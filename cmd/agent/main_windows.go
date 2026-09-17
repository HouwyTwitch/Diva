//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
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

func main() {
	server := flag.String("server", "", "wss://server/ws")
	room := flag.String("room", "pc", "computer name")
	token := flag.String("token", "", "access token")
	fps := flag.Int("fps", 60, "capture FPS")
	bitrate := flag.Int("bitrate", 12000, "video kbps")
	monitor := flag.Int("monitor", 0, "monitor index (0 is virtual desktop)")
	ffmpeg := flag.String("ffmpeg", "ffmpeg.exe", "FFmpeg path")
	flag.Parse()
	if *server == "" || *token == "" {
		flag.Usage()
		os.Exit(2)
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
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
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
			write(ws, "candidate", c.ToJSON())
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) { log.Printf("peer: %s", s) })
	go capture(track, *ffmpeg, *fps, *bitrate, *monitor)
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
		case "candidate":
			var c webrtc.ICECandidateInit
			must(json.Unmarshal(m.Payload, &c))
			must(pc.AddICECandidate(c))
		}
	}
}

func capture(track *webrtc.TrackLocalStaticSample, ff string, fps, bitrate, monitor int) {
	// gdigrab captures the complete Windows virtual desktop; monitor cropping can be
	// supplied by putting normal FFmpeg arguments in a wrapper script.
	args := []string{"-hide_banner", "-loglevel", "warning", "-f", "gdigrab", "-framerate", fmt.Sprint(fps), "-i", "desktop", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-b:v", fmt.Sprintf("%dk", bitrate), "-g", fmt.Sprint(fps), "-pix_fmt", "yuv420p", "-bsf:v", "h264_metadata=aud=insert", "-f", "h264", "pipe:1"}
	_ = monitor
	cmd := exec.Command(ff, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	must(err)
	must(cmd.Start())
	r := &h264Reader{r: bufio.NewReaderSize(out, 1<<20)}
	for {
		frame, err := r.readAccessUnit()
		if len(frame) > 0 {
			_ = track.WriteSample(media.Sample{Data: frame, Duration: time.Second / time.Duration(fps)})
		}
		if err != nil {
			log.Fatal(err)
		}
	}
}

type h264Reader struct {
	r                 *bufio.Reader
	delimiterConsumed bool
	pendingNAL        []byte
}

func (h *h264Reader) readAccessUnit() ([]byte, error) {
	var out []byte
	for {
		nal, err := h.readNAL()
		if len(nal) > 4 && (nal[4]&31) == 9 && len(out) > 0 {
			h.pendingNAL = nal
			return out, nil
		}
		out = append(out, nal...)
		if err != nil {
			return out, err
		}
	}
}
func (h *h264Reader) readNAL() ([]byte, error) {
	if len(h.pendingNAL) > 0 {
		n := h.pendingNAL
		h.pendingNAL = nil
		return n, nil
	}
	prefix := []byte{0, 0, 0, 1}
	var out []byte
	if h.delimiterConsumed {
		out = append(out, prefix...)
		h.delimiterConsumed = false
	}
	zeros := 0
	for {
		b, err := h.r.ReadByte()
		if err != nil {
			return out, err
		}
		if b == 0 {
			zeros++
			continue
		}
		if b == 1 && zeros >= 3 {
			if len(out) > 0 {
				h.delimiterConsumed = true
				return out, nil
			}
			out = append(out, prefix...)
			zeros = 0
			continue
		}
		out = append(out, make([]byte, zeros)...)
		zeros = 0
		out = append(out, b)
	}
}
func write(ws *websocket.Conn, t string, v any) {
	b, _ := json.Marshal(v)
	_ = ws.WriteJSON(envelope{Type: t, Payload: b})
}
func inject(e inputEvent) {
	if e.Type == "mousemove" {
		w, h := screenSize()
		setCursor.Call(uintptr(int(e.X*float64(w))), uintptr(int(e.Y*float64(h))))
		return
	}
	if e.Type == "wheel" {
		mouse(0x0800, uint32(int32(-e.DY*120)))
		return
	}
	if strings.HasPrefix(e.Type, "mouse") {
		down := e.Type == "mousedown"
		f := uint32(0x0002)
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
func screenSize() (int, int) {
	gx := user32.NewProc("GetSystemMetrics")
	w, _, _ := gx.Call(78)
	h, _, _ := gx.Call(79)
	return int(w), int(h)
}

var keys = map[string]uint16{"Enter": 13, "Escape": 27, "Backspace": 8, "Tab": 9, "Space": 32, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Delete": 46, "ControlLeft": 17, "ControlRight": 17, "ShiftLeft": 16, "ShiftRight": 16, "AltLeft": 18, "AltRight": 18, "MetaLeft": 91, "MetaRight": 92}

func init() {
	for c := byte('A'); c <= 'Z'; c++ {
		keys["Key"+string(c)] = uint16(c)
	}
	for c := byte('0'); c <= '9'; c++ {
		keys["Digit"+string(c)] = uint16(c)
	}
}
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
