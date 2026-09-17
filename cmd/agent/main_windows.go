//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"diva/internal/annexb"
)

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type inputEvent struct {
	Type, Key    string
	Button       int
	X, Y, DX, DY float64
}

var user32 = syscall.NewLazyDLL("user32.dll")
var sendInput = user32.NewProc("SendInput")
var setCursor = user32.NewProc("SetCursorPos")
var wsWriteMu sync.Mutex
var captureX, captureY, captureWidth, captureHeight int

func main() {
	server := flag.String("server", "", "wss://server/ws")
	room := flag.String("room", "pc", "computer name")
	token := flag.String("token", "", "access token")
	fps := flag.Int("fps", 60, "capture FPS")
	bitrate := flag.Int("bitrate", 12000, "video kbps")
	publicIP := flag.String("public-ip", "", "public IPv4 address advertised to the browser")
	stunURL := flag.String("stun", "stun:stun.cloudflare.com:3478", "STUN URL used for NAT discovery (empty disables STUN)")
	udpPort := flag.Uint("udp-port", 50000, "single UDP port used by WebRTC")
	x := flag.Int("x", 0, "capture left coordinate")
	y := flag.Int("y", 0, "capture top coordinate")
	width := flag.Int("width", 0, "capture width (0 means full virtual desktop)")
	height := flag.Int("height", 0, "capture height (0 means full virtual desktop)")
	encoder := flag.String("encoder", "auto", "FFmpeg encoder: auto, libx264, h264_nvenc, h264_qsv, or h264_amf")
	encoderFallback := flag.Bool("encoder-fallback", true, "fall back to libx264 when a hardware encoder cannot start")
	ffmpeg := flag.String("ffmpeg", "ffmpeg.exe", "FFmpeg path")
	flag.Parse()
	if *server == "" || *token == "" {
		flag.Usage()
		os.Exit(2)
	}
	captureX, captureY, captureWidth, captureHeight = *x, *y, *width, *height
	if captureWidth <= 0 || captureHeight <= 0 {
		captureX, captureY, captureWidth, captureHeight = virtualScreen()
	}
	u, err := url.Parse(*server)
	must(err)
	q := u.Query()
	q.Set("role", "host")
	q.Set("room", *room)
	q.Set("token", *token)
	u.RawQuery = q.Encode()
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	must(err)
	defer ws.Close()
	var settings webrtc.SettingEngine
	if *publicIP != "" {
		settings.SetNAT1To1IPs([]string{*publicIP}, webrtc.ICECandidateTypeHost)
	}
	must(settings.SetEphemeralUDPPortRange(uint16(*udpPort), uint16(*udpPort)))
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	configuration := webrtc.Configuration{}
	if *stunURL != "" {
		configuration.ICEServers = []webrtc.ICEServer{{URLs: []string{*stunURL}}}
	}
	pc, err := api.NewPeerConnection(configuration)
	must(err)
	defer pc.Close()
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "video", "diva")
	must(err)
	_, err = pc.AddTrack(track)
	must(err)
	dc, err := pc.CreateDataChannel("input", nil)
	must(err)
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		var e inputEvent
		if json.Unmarshal(m.Data, &e) == nil {
			inject(e)
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			log.Printf("local ICE candidate: type=%s protocol=%s address=%s port=%d", c.Typ, c.Protocol, c.Address, c.Port)
			write(ws, "candidate", c.ToJSON())
		}
	})
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		log.Printf("ICE: %s", s)
		if s == webrtc.ICEConnectionStateFailed {
			log.Printf("ICE failed: verify UDP %d forwarding/firewall and that public-ip=%s is the Windows/router public IPv4", *udpPort, *publicIP)
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) { log.Printf("peer: %s", s) })
	go capture(track, *ffmpeg, *fps, *bitrate, *encoder, *encoderFallback, *x, *y, *width, *height)
	var pendingCandidates []webrtc.ICECandidateInit
	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			log.Fatal(err)
		}
		var m envelope
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch m.Type {
		case "viewer-ready":
			offer, err := pc.CreateOffer(nil)
			must(err)
			must(pc.SetLocalDescription(offer))
			write(ws, "offer", offer)
		case "answer":
			var d webrtc.SessionDescription
			must(json.Unmarshal(m.Payload, &d))
			must(pc.SetRemoteDescription(d))
			for _, candidate := range pendingCandidates {
				must(pc.AddICECandidate(candidate))
			}
			pendingCandidates = nil
		case "candidate":
			var c webrtc.ICECandidateInit
			must(json.Unmarshal(m.Payload, &c))
			log.Printf("remote ICE candidate received: %s", c.Candidate)
			if pc.RemoteDescription() == nil {
				pendingCandidates = append(pendingCandidates, c)
			} else {
				must(pc.AddICECandidate(c))
			}
		case "viewer-left":
			log.Fatal("viewer disconnected; restarting the agent for a clean WebRTC session")
		}
	}
}

func capture(track *webrtc.TrackLocalStaticSample, ff string, fps, bitrate int, requested string, fallback bool, x, y, width, height int) {
	encoders := []string{requested}
	if requested == "auto" {
		encoders = []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"}
	} else if fallback && requested != "libx264" {
		encoders = append(encoders, "libx264")
	}
	for index, encoder := range encoders {
		log.Printf("starting FFmpeg encoder %s (attempt %d/%d)", encoder, index+1, len(encoders))
		err := runCapture(track, ff, fps, bitrate, encoder, x, y, width, height)
		if err == nil {
			return
		}
		log.Printf("encoder %s stopped: %v", encoder, err)
		if index+1 < len(encoders) {
			log.Printf("trying fallback encoder %s", encoders[index+1])
		}
	}
	log.Fatal("all configured H.264 encoders failed; test FFmpeg manually and update the GPU driver")
}

func runCapture(track *webrtc.TrackLocalStaticSample, ff string, fps, bitrate int, encoder string, x, y, width, height int) error {
	args := []string{"-hide_banner", "-loglevel", "warning", "-f", "gdigrab", "-framerate", fmt.Sprint(fps), "-offset_x", fmt.Sprint(x), "-offset_y", fmt.Sprint(y)}
	if width > 0 && height > 0 {
		args = append(args, "-video_size", fmt.Sprintf("%dx%d", width, height))
	}
	args = append(args, "-i", "desktop", "-an", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:v", encoder)
	if encoder == "libx264" {
		args = append(args, "-preset", "ultrafast", "-tune", "zerolatency")
	} else if encoder == "h264_nvenc" {
		args = append(args, "-preset", "p1", "-tune", "ull", "-rc", "cbr", "-delay", "0")
	}
	args = append(args, "-b:v", fmt.Sprintf("%dk", bitrate), "-maxrate", fmt.Sprintf("%dk", bitrate), "-bufsize", fmt.Sprintf("%dk", bitrate/2), "-g", fmt.Sprint(fps), "-bf", "0", "-pix_fmt", "yuv420p", "-bsf:v", "h264_metadata=aud=insert", "-f", "h264", "pipe:1")
	cmd := exec.Command(ff, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open FFmpeg output: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start FFmpeg: %w", err)
	}
	r := annexb.New(out)
	for {
		frame, err := r.ReadAccessUnit()
		if len(frame) > 0 {
			_ = track.WriteSample(media.Sample{Data: frame, Duration: time.Second / time.Duration(fps)})
		}
		if err != nil {
			waitErr := cmd.Wait()
			if waitErr != nil {
				return fmt.Errorf("FFmpeg exited before producing a usable stream: %w", waitErr)
			}
			if err == io.EOF {
				return fmt.Errorf("FFmpeg closed its video stream")
			}
			return fmt.Errorf("read H.264 stream: %w", err)
		}
	}
}

func write(ws *websocket.Conn, t string, v any) {
	b, _ := json.Marshal(v)
	wsWriteMu.Lock()
	defer wsWriteMu.Unlock()
	_ = ws.WriteJSON(envelope{Type: t, Payload: b})
}
func inject(e inputEvent) {
	if e.Type == "mousemove" {
		px := captureX + int(e.X*float64(captureWidth-1))
		py := captureY + int(e.Y*float64(captureHeight-1))
		setCursor.Call(uintptr(px), uintptr(py))
		return
	}
	if e.Type == "wheel" {
		delta := int32(120)
		if e.DY > 0 {
			delta = -120
		}
		mouse(0x0800, uint32(delta))
		return
	}
	if strings.HasPrefix(e.Type, "mouse") {
		down := e.Type == "mousedown"
		f := uint32(0x0002)
		if e.Button == 1 {
			f = 0x0020
		}
		if e.Button == 2 {
			f = 0x0008
		}
		if !down {
			f <<= 1
		}
		mouse(f, 0)
		return
	}
	if code, ok := keys[e.Key]; ok {
		flags := uint32(0)
		if e.Type == "keyup" {
			flags = 2
		}
		keyboard(code, flags)
	}
}

type mouseInput struct {
	dx, dy            int32
	data, flags, time uint32
	extra             uintptr
}
type keyInput struct {
	vk, scan    uint16
	flags, time uint32
	extra       uintptr
}
type input struct {
	typ  uint32
	data [40]byte
}

func mouse(flags, data uint32) {
	var i input
	i.typ = 0
	m := (*mouseInput)(unsafe.Pointer(&i.data[0]))
	m.flags = flags
	m.data = data
	sendInput.Call(1, uintptr(unsafe.Pointer(&i)), unsafe.Sizeof(i))
}
func keyboard(vk uint16, flags uint32) {
	var i input
	i.typ = 1
	k := (*keyInput)(unsafe.Pointer(&i.data[0]))
	k.vk = vk
	k.flags = flags
	sendInput.Call(1, uintptr(unsafe.Pointer(&i)), unsafe.Sizeof(i))
}
func virtualScreen() (int, int, int, int) {
	gx := user32.NewProc("GetSystemMetrics")
	x, _, _ := gx.Call(76)
	y, _, _ := gx.Call(77)
	w, _, _ := gx.Call(78)
	h, _, _ := gx.Call(79)
	return int(int32(x)), int(int32(y)), int(w), int(h)
}

var keys = map[string]uint16{
	"Enter": 13, "Escape": 27, "Backspace": 8, "Tab": 9, "Space": 32,
	"PageUp": 33, "PageDown": 34, "End": 35, "Home": 36, "ArrowLeft": 37,
	"ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Insert": 45, "Delete": 46,
	"ControlLeft": 17, "ControlRight": 17, "ShiftLeft": 16, "ShiftRight": 16,
	"AltLeft": 18, "AltRight": 18, "MetaLeft": 91, "MetaRight": 92,
	"CapsLock": 20, "NumLock": 144, "ScrollLock": 145, "Pause": 19,
	"Semicolon": 186, "Equal": 187, "Comma": 188, "Minus": 189, "Period": 190,
	"Slash": 191, "Backquote": 192, "BracketLeft": 219, "Backslash": 220,
	"BracketRight": 221, "Quote": 222,
}

func init() {
	for c := byte('A'); c <= 'Z'; c++ {
		keys["Key"+string(c)] = uint16(c)
	}
	for c := byte('0'); c <= '9'; c++ {
		keys["Digit"+string(c)] = uint16(c)
	}
	for n := 1; n <= 24; n++ {
		keys[fmt.Sprintf("F%d", n)] = uint16(111 + n)
	}
	for n := 0; n <= 9; n++ {
		keys[fmt.Sprintf("Numpad%d", n)] = uint16(96 + n)
	}
}
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
