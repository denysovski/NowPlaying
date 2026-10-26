// NowPlaying: a tiny local server that receives the current song from the
// browser extension and pushes it instantly to the OBS overlay (Server-Sent
// Events). It also exposes the song as JSON and a text file so a Twitch bot
// (!song) or an OBS text source can read it later.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFiles embed.FS

type Track struct {
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Source  string `json:"source"` // "youtube" | "soundcloud" | ""
	URL     string `json:"url"`
	Playing bool   `json:"playing"`
	Active  bool   `json:"active"` // false when nothing is open/playing
	Updated int64  `json:"updated"`
}

type historyItem struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Source string `json:"source"`
	At     int64  `json:"at"`
}

type hub struct {
	mu       sync.Mutex
	current  Track
	history  []historyItem
	lastSeen time.Time
	clients  map[chan []byte]struct{}
	overlays int // connected OBS overlays (not counting the app window)
	textPath string
	settings overlaySettings
}

func (h *hub) broadcastLocked(frame []byte) {
	for c := range h.clients {
		select {
		case c <- frame:
		default: // slow client, drop this update; it will get the next one
		}
	}
}

func sseFrame(event string, v any) []byte {
	data, _ := json.Marshal(v)
	if event == "" {
		return []byte("data: " + string(data) + "\n\n")
	}
	return []byte("event: " + event + "\ndata: " + string(data) + "\n\n")
}

func (h *hub) setSettings(st overlaySettings) {
	h.mu.Lock()
	h.settings = st
	h.broadcastLocked(sseFrame("settings", st))
	h.mu.Unlock()
}

func (h *hub) getSettings() overlaySettings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings
}

func newHub(textPath string) *hub {
	return &hub{clients: map[chan []byte]struct{}{}, textPath: textPath}
}

func (h *hub) snapshot() Track {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

// set stores a new state and broadcasts it if anything visible changed.
func (h *hub) set(t Track) {
	h.mu.Lock()
	h.lastSeen = time.Now()
	prev := h.current
	t.Updated = prev.Updated
	same := prev.Title == t.Title && prev.Artist == t.Artist && prev.Source == t.Source &&
		prev.Playing == t.Playing && prev.Active == t.Active && prev.URL == t.URL
	if same {
		h.mu.Unlock()
		return
	}
	t.Updated = time.Now().UnixMilli()
	h.current = t
	if t.Active && (prev.Title != t.Title || prev.Artist != t.Artist) &&
		(len(h.history) == 0 || h.history[0].Title != t.Title || h.history[0].Artist != t.Artist) {
		h.history = append([]historyItem{{t.Title, t.Artist, t.Source, t.Updated}}, h.history...)
		if len(h.history) > 25 {
			h.history = h.history[:25]
		}
	}
	h.broadcastLocked(sseFrame("", t))
	h.mu.Unlock()

	songChanged := prev.Title != t.Title || prev.Artist != t.Artist || prev.Active != t.Active
	if songChanged {
		h.writeText(t)
		if t.Active {
			log.Printf("Now playing [%s]: %s", t.Source, songLine(t))
		} else {
			log.Printf("Nothing playing")
		}
	}
}

func songLine(t Track) string {
	if !t.Active || t.Title == "" {
		return ""
	}
	if t.Artist == "" {
		return t.Title
	}
	return t.Artist + " - " + t.Title
}

func (h *hub) writeText(t Track) {
	if h.textPath == "" {
		return
	}
	if err := os.WriteFile(h.textPath, []byte(songLine(t)), 0o644); err != nil {
		log.Printf("could not write %s: %v", h.textPath, err)
	}
}

// watchdog clears the song if the browser stops reporting (closed, crashed).
func (h *hub) watchdog(timeout time.Duration) {
	for range time.Tick(time.Second) {
		h.mu.Lock()
		stale := h.current.Active && time.Since(h.lastSeen) > timeout
		h.mu.Unlock()
		if stale {
			h.set(Track{})
		}
	}
}

func (h *hub) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		cors(w)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var t Track
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&t); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	t.Title = strings.TrimSpace(t.Title)
	t.Artist = strings.TrimSpace(t.Artist)
	t.Active = t.Active && t.Title != ""
	if !t.Active {
		t = Track{}
	}
	h.set(t)
	cors(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *hub) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	cors(w)
	t := h.snapshot()
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		line := songLine(t)
		if line == "" {
			line = "No song is playing right now."
		}
		fmt.Fprint(w, line)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// handleStatus feeds the app window.
func (h *hub) handleStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	resp := struct {
		Extension bool          `json:"extension"`
		Track     Track         `json:"track"`
		History   []historyItem `json:"history"`
		Started   int64         `json:"started"`
		Overlays  int           `json:"overlays"`
	}{
		Overlays:  h.overlays,
		Extension: !h.lastSeen.IsZero() && time.Since(h.lastSeen) < 8*time.Second,
		Track:     h.current,
		History:   h.history,
		Started:   startedAt.UnixMilli(),
	}
	if resp.History == nil {
		resp.History = []historyItem{}
	}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(resp)
}

func (h *hub) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	cors(w)

	isOverlay := r.URL.Query().Get("c") != "app"
	c := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	if isOverlay {
		h.overlays++
	}
	first := append(sseFrame("settings", h.settings), sseFrame("", h.current)...)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		if isOverlay {
			h.overlays--
		}
		h.mu.Unlock()
	}()

	fmt.Fprint(w, "retry: 1000\n")
	w.Write(first)
	fl.Flush()
	// Named ping so the overlay can tell the app is alive (it hides itself when pings stop).
	ping := time.NewTicker(4 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-c:
			w.Write(frame)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, "event: ping\ndata: 1\n\n")
			fl.Flush()
		}
	}
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

var startedAt = time.Now()

func main() {
	port := flag.Int("port", 8787, "port to listen on")
	startHidden := flag.Bool("tray", false, "start hidden in the tray")
	withOBS := flag.Bool("with-obs", false, "quit automatically when OBS closes")
	flag.Parse()
	debug.SetGCPercent(50) // small heap; this app does very little allocation

	// Only one copy: if it's already running, bring its window to the front.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		if *startHidden { // started by OBS/autostart while already running: nothing to do
			return
		}
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/api/show", *port), nil)
		req.Header.Set("X-NowPlaying", "1")
		cl := &http.Client{Timeout: 2 * time.Second}
		if resp, err2 := cl.Do(req); err2 == nil && resp.StatusCode == http.StatusNoContent {
			return
		}
		fatalBox(fmt.Sprintf("NowPlaying could not start on port %d.\n\nSomething else is using that port.", *port))
		return
	}

	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	h := newHub(filepath.Join(exeDir, "nowplaying.txt"))
	h.writeText(Track{})
	go h.watchdog(8 * time.Second)
	store := loadConfig()

	web, _ := fs.Sub(webFiles, "web")
	cover := newCoverFile(exeDir, web)
	// Same overlay as a file, for OBS "Local file" mode: it loads even when the
	// app isn't running yet and stays invisible until the app starts.
	if data, err := fs.ReadFile(web, "overlay.html"); err == nil {
		os.WriteFile(filepath.Join(exeDir, "OBS-overlay.html"), data, 0o644)
	}
	st := store.get().Overlay
	st.CoverV = cover.version()
	h.settings = st
	go cover.watch(func(v int64) {
		s := h.getSettings()
		s.CoverV = v
		h.setSettings(s)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/cover.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if p := cover.path(); p != "" {
			http.ServeFile(w, r, p)
			return
		}
		http.ServeFileFS(w, r, web, "cover.jpg")
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.Header.Get("X-NowPlaying") != "1" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			var in overlaySettings
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in) != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if in.Speed < 10 || in.Speed > 200 {
				in.Speed = 45
			}
			s := h.getSettings()
			s.HidePaused, s.Speed = in.HidePaused, in.Speed
			h.setSettings(s)
			store.update(func(c *appConfig) { c.Overlay = s })
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.getSettings())
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-NowPlaying") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		showWindow()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/update", h.handleUpdate)
	mux.HandleFunc("/api/now-playing", h.handleNowPlaying)
	mux.HandleFunc("/events", h.handleEvents)
	mux.HandleFunc("/api/status", h.handleStatus)
	bot := newTwitchBot(h, store, *port)
	go bot.run()
	mux.HandleFunc("/api/twitch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			bot.handleSave(w, r)
			return
		}
		bot.handleGet(w, r)
	})
	mux.HandleFunc("/api/twitch/login", bot.handleLogin)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/overlay", http.StatusFound)
			return
		}
		switch r.URL.Path {
		case "/overlay":
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, web, "overlay.html")
		case "/app":
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, web, "app.html")
		case "/logo.png", "/icon.ico":
			http.ServeFileFS(w, r, web, r.URL.Path[1:])
		default:
			http.NotFound(w, r)
		}
	})

	go func() { log.Fatal(http.Serve(ln, mux)) }()
	if *withOBS {
		go quitWithOBS()
	}

	// Blocks until the user picks Quit in the tray (or forever without a window).
	runUI(*port, cover.dir, *startHidden)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// quitWithOBS ends the app shortly after OBS closes (started by the OBS script).
func quitWithOBS() {
	misses := 0
	for range time.Tick(3 * time.Second) {
		if obsRunning() {
			misses = 0
			continue
		}
		if misses++; misses >= 2 {
			quitApp()
			return
		}
	}
}
