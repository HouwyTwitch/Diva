package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed web/*
var web embed.FS

type message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type peer struct {
	c    *websocket.Conn
	send chan []byte
}
type room struct {
	host   *peer
	viewer *peer
}

var rooms = struct {
	sync.Mutex
	m map[string]*room
}{m: map[string]*room{}}
var up = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" { // Native Windows agent.
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}, ReadBufferSize: 4096, WriteBufferSize: 4096}

func main() {
	token := os.Getenv("DIVA_TOKEN")
	if len(token) < 16 {
		log.Fatal("DIVA_TOKEN must contain at least 16 characters")
	}
	mux := http.NewServeMux()
	webRoot, err := fs.Sub(web, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/config.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"stunUrl": getenv("DIVA_STUN_URL", "stun:stun.cloudflare.com:3478")})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { serveWS(w, r, token) })
	s := &http.Server{Addr: getenv("DIVA_LISTEN", ":8080"), Handler: security(mux), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Diva listening on %s", s.Addr)
	log.Fatal(s.ListenAndServe())
}

func serveWS(w http.ResponseWriter, r *http.Request, secret string) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(secret)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	role, id := r.URL.Query().Get("role"), r.URL.Query().Get("room")
	if (role != "host" && role != "viewer") || id == "" || len(id) > 64 {
		http.Error(w, "bad request", 400)
		return
	}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p := &peer{c: c, send: make(chan []byte, 64)}
	rooms.Lock()
	rm := rooms.m[id]
	if rm == nil {
		rm = &room{}
		rooms.m[id] = rm
	}
	if role == "host" {
		if rm.host != nil {
			rooms.Unlock()
			c.Close()
			return
		}
		rm.host = p
		if rm.viewer != nil {
			relay(rm.host, mustJSON(message{Type: "viewer-ready"}))
		}
	} else {
		if rm.viewer != nil {
			rooms.Unlock()
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "computer is already in use"), time.Now().Add(time.Second))
			_ = c.Close()
			return
		}
		rm.viewer = p
		relay(rm.host, mustJSON(message{Type: "viewer-ready"}))
	}
	rooms.Unlock()
	go writer(p)
	for {
		_, b, err := c.ReadMessage()
		if err != nil {
			break
		}
		if !validMessage(b) {
			continue
		}
		rooms.Lock()
		if role == "host" {
			relay(rm.viewer, b)
		} else {
			relay(rm.host, b)
		}
		rooms.Unlock()
	}
	rooms.Lock()
	if role == "host" {
		rm.host = nil
		relay(rm.viewer, mustJSON(message{Type: "host-left"}))
	} else {
		rm.viewer = nil
		relay(rm.host, mustJSON(message{Type: "viewer-left"}))
	}
	if rm.host == nil && rm.viewer == nil {
		delete(rooms.m, id)
	}
	rooms.Unlock()
	close(p.send)
	c.Close()
}
func writer(p *peer) {
	for b := range p.send {
		p.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if p.c.WriteMessage(websocket.TextMessage, b) != nil {
			return
		}
	}
}
func relay(p *peer, b []byte) {
	if p == nil {
		return
	}
	select {
	case p.send <- b:
	default:
	}
}
func validMessage(b []byte) bool {
	var m message
	return len(b) < 256*1024 && json.Unmarshal(b, &m) == nil && (m.Type == "offer" || m.Type == "answer" || m.Type == "candidate")
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}
