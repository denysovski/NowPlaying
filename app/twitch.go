package main

// Twitch chat bot: answers !song with the current song. Nightbot and
// StreamElements run in the cloud and can't see this PC, so the app joins
// chat itself (with the streamer's or a bot account's token).

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type twitchConfig struct {
	Enabled  bool   `json:"enabled"`
	ClientID string `json:"clientId"`
	Token    string `json:"token"`
	Refresh  string `json:"refresh"`
	Channel  string `json:"channel"`  // empty = the logged-in account's channel
	Command  string `json:"command"`  // default !song
	Reply    string `json:"reply"`    // template
	Nothing  string `json:"nothing"`  // reply when nothing plays
	Cooldown int    `json:"cooldown"` // seconds
}

func (c *twitchConfig) defaults() {
	if c.Command == "" {
		c.Command = "!song"
	}
	if c.Reply == "" {
		c.Reply = "🎵 {song} → {url}"
	}
	if c.Nothing == "" {
		c.Nothing = "Right now nothing is playing."
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 5
	}
}

// Twitch IRC endpoint; a variable so tests can point it at a fake server.
var twitchIRCAddr = "irc.chat.twitch.tv:6697"
var twitchValidateURL = "https://id.twitch.tv/oauth2/validate"
var twitchOAuthBase = "https://id.twitch.tv/oauth2"
var twitchTLS *tls.Config // nil = normal verification

type twitchBot struct {
	hub     *hub
	store   *configStore
	port    int
	mu      sync.Mutex
	cfg     twitchConfig
	state   string // off | connecting | connected | error
	detail  string
	login   string
	oauth   string // pending login state
	restart chan struct{}
}

func newTwitchBot(h *hub, store *configStore, port int) *twitchBot {
	return &twitchBot{hub: h, store: store, port: port, cfg: store.get().Twitch, restart: make(chan struct{}, 1)}
}

// save must be called with b.mu held.
func (b *twitchBot) save() error {
	cfg := b.cfg
	return b.store.update(func(c *appConfig) { c.Twitch = cfg })
}

func (b *twitchBot) setState(state, detail string) {
	b.mu.Lock()
	b.state, b.detail = state, detail
	b.mu.Unlock()
}

func (b *twitchBot) kick() {
	select {
	case b.restart <- struct{}{}:
	default:
	}
}

// redirectURI is only needed to register the Twitch app (the device login
// never redirects), and Twitch requires https there.
func (b *twitchBot) redirectURI() string {
	return "https://localhost"
}

// ---- running the bot

func (b *twitchBot) run() {
	for {
		b.mu.Lock()
		cfg := b.cfg
		b.mu.Unlock()

		if !cfg.Enabled || cfg.Token == "" {
			if cfg.Token == "" {
				b.setState("off", "Not connected to Twitch")
			} else {
				b.setState("off", "Turned off")
			}
			<-b.restart
			continue
		}

		b.setState("connecting", "Connecting…")
		err := b.session(cfg)
		if err == errRestart {
			continue
		}
		msg := "Disconnected, retrying…"
		if err != nil {
			msg = err.Error()
		}
		b.setState("error", msg)
		select {
		case <-b.restart:
		case <-time.After(10 * time.Second):
		}
	}
}

var errRestart = errors.New("restart")
var errExpired = errors.New("Twitch login expired, click Connect Twitch again")

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Message      string `json:"message"`
}

func twitchPost(path string, form url.Values) (tokenResp, int, error) {
	var t tokenResp
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.PostForm(twitchOAuthBase+path, form)
	if err != nil {
		return t, 0, fmt.Errorf("Can't reach Twitch (check internet)")
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&t)
	return t, resp.StatusCode, nil
}

// freshToken validates the saved token and swaps it for a new one with the
// refresh token when it has expired (Twitch device-login tokens last ~4 h).
func (b *twitchBot) freshToken(cfg twitchConfig) (string, validateResp, error) {
	token := strings.TrimPrefix(strings.TrimSpace(cfg.Token), "oauth:")
	v, err := validateToken(token)
	if err != errExpired || cfg.Refresh == "" || cfg.ClientID == "" {
		return token, v, err
	}
	t, code, err := twitchPost("/token", url.Values{
		"client_id": {cfg.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {cfg.Refresh},
	})
	if err != nil {
		return token, v, err
	}
	if code != 200 || t.AccessToken == "" {
		return token, v, errExpired
	}
	b.mu.Lock()
	b.cfg.Token, b.cfg.Refresh = t.AccessToken, t.RefreshToken
	b.save()
	b.mu.Unlock()
	v, err = validateToken(t.AccessToken)
	return t.AccessToken, v, err
}

type validateResp struct {
	Login     string   `json:"login"`
	Scopes    []string `json:"scopes"`
	ExpiresIn int      `json:"expires_in"`
}

func validateToken(token string) (validateResp, error) {
	var v validateResp
	req, _ := http.NewRequest("GET", twitchValidateURL, nil)
	req.Header.Set("Authorization", "OAuth "+token)
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return v, fmt.Errorf("Can't reach Twitch (check internet)")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return v, errExpired
	}
	if resp.StatusCode != 200 {
		return v, fmt.Errorf("Twitch said %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return v, err
	}
	need := map[string]bool{"chat:read": false, "chat:edit": false}
	for _, s := range v.Scopes {
		if _, ok := need[s]; ok {
			need[s] = true
		}
	}
	if !need["chat:read"] || !need["chat:edit"] {
		return v, fmt.Errorf("The token can't read/write chat, click Connect Twitch again")
	}
	return v, nil
}

func (b *twitchBot) session(cfg twitchConfig) error {
	token, v, err := b.freshToken(cfg)
	if err != nil {
		return err
	}
	channel := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cfg.Channel), "#"))
	if channel == "" {
		channel = v.Login
	}
	b.mu.Lock()
	b.login = v.Login
	b.mu.Unlock()

	tcfg := twitchTLS
	if tcfg == nil {
		tcfg = &tls.Config{ServerName: strings.Split(twitchIRCAddr, ":")[0]}
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", twitchIRCAddr, tcfg)
	if err != nil {
		return fmt.Errorf("Can't connect to Twitch chat")
	}
	defer conn.Close()

	var wmu sync.Mutex
	send := func(line string) error {
		wmu.Lock()
		defer wmu.Unlock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := conn.Write([]byte(line + "\r\n"))
		return err
	}
	send("CAP REQ :twitch.tv/tags twitch.tv/commands")
	send("PASS oauth:" + token)
	send("NICK " + v.Login)
	send("JOIN #" + channel)

	// Close the connection on restart, or when the token stops validating (checked hourly, as Twitch asks).
	done := make(chan struct{})
	defer close(done)
	var restarted atomic.Bool
	go func() {
		tick := time.NewTicker(time.Hour)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-b.restart:
				restarted.Store(true)
				conn.Close()
				return
			case <-tick.C:
				if _, err := validateToken(token); err != nil {
					conn.Close() // reconnect; freshToken renews the login
					return
				}
			}
		}
	}()

	var lastReply time.Time
	r := bufio.NewReader(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(6 * time.Minute)) // Twitch pings every ~5 min
		line, err := r.ReadString('\n')
		if err != nil {
			if restarted.Load() {
				return errRestart
			}
			return nil
		}
		m := parseIRC(strings.TrimRight(line, "\r\n"))
		switch m.command {
		case "PING":
			send("PONG :" + m.trailing)
		case "NOTICE":
			if strings.Contains(strings.ToLower(m.trailing), "authentication failed") ||
				strings.Contains(strings.ToLower(m.trailing), "improperly formatted auth") {
				return fmt.Errorf("Twitch rejected the login, click Connect Twitch again")
			}
		case "RECONNECT":
			return nil
		case "ROOMSTATE", "366":
			b.setState("connected", "Listening in #"+channel+" as "+v.Login)
		case "PRIVMSG":
			word := strings.ToLower(strings.Fields(m.trailing + " x")[0])
			if word != strings.ToLower(cfg.Command) {
				continue
			}
			if time.Since(lastReply) < time.Duration(cfg.Cooldown)*time.Second {
				continue
			}
			lastReply = time.Now()
			user := m.tags["display-name"]
			if user == "" {
				user = strings.SplitN(m.prefix, "!", 2)[0]
			}
			out := "PRIVMSG #" + channel + " :" + b.replyText(cfg, user)
			if id := m.tags["id"]; id != "" {
				out = "@reply-parent-msg-id=" + id + " " + out
			}
			send(out)
		}
	}
}

func (b *twitchBot) replyText(cfg twitchConfig, user string) string {
	t := b.hub.snapshot()
	tpl := cfg.Reply
	if !t.Active || t.Title == "" {
		tpl = cfg.Nothing
	}
	src := map[string]string{"youtube": "YouTube", "soundcloud": "SoundCloud"}[t.Source]
	// Only YouTube gets a link; SoundCloud just shows the song name.
	link := ""
	if t.Source == "youtube" {
		link = cleanYouTubeURL(t.URL)
	}
	rep := strings.NewReplacer(
		"{user}", "@"+user,
		"{song}", songLine(t),
		"{title}", t.Title,
		"{artist}", t.Artist,
		"{source}", src,
		"{url}", link,
	)
	out := strings.TrimSpace(rep.Replace(tpl))
	out = strings.TrimSpace(strings.TrimSuffix(out, "→")) // template with {url} but no url
	out = strings.NewReplacer("\r", " ", "\n", " ").Replace(out)
	if len(out) > 480 {
		out = out[:480]
	}
	return out
}

// Keep only ?v= so links in chat are short.
func cleanYouTubeURL(u string) string {
	i := strings.Index(u, "v=")
	if i < 0 || !strings.Contains(u, "/watch") {
		return u
	}
	id := u[i+2:]
	if j := strings.IndexAny(id, "&#"); j >= 0 {
		id = id[:j]
	}
	return "https://youtu.be/" + id
}

type ircMsg struct {
	tags     map[string]string
	prefix   string
	command  string
	params   []string
	trailing string
}

func parseIRC(line string) ircMsg {
	m := ircMsg{tags: map[string]string{}}
	if strings.HasPrefix(line, "@") {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return m
		}
		for _, kv := range strings.Split(line[1:sp], ";") {
			k, v, _ := strings.Cut(kv, "=")
			v = strings.NewReplacer(`\s`, " ", `\:`, ";", `\\`, `\`).Replace(v)
			m.tags[k] = v
		}
		line = line[sp+1:]
	}
	if strings.HasPrefix(line, ":") {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return m
		}
		m.prefix = line[1:sp]
		line = line[sp+1:]
	}
	if i := strings.Index(line, " :"); i >= 0 {
		m.trailing = line[i+2:]
		line = line[:i]
	}
	f := strings.Fields(line)
	if len(f) > 0 {
		m.command = f[0]
		m.params = f[1:]
	}
	return m
}

// ---- HTTP API for the app window

// Only the app's own pages may change settings: a custom header forces a CORS
// preflight (which we never allow), and the Origin must be ours.
func (b *twitchBot) allowed(r *http.Request) bool {
	if r.Header.Get("X-NowPlaying") != "1" {
		return false
	}
	o := r.Header.Get("Origin")
	return o == "" || o == fmt.Sprintf("http://127.0.0.1:%d", b.port) || o == fmt.Sprintf("http://localhost:%d", b.port)
}

func (b *twitchBot) handleGet(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	c := b.cfg
	resp := map[string]any{
		"enabled": c.Enabled, "clientId": c.ClientID, "channel": c.Channel, "command": c.Command,
		"reply": c.Reply, "nothing": c.Nothing, "cooldown": c.Cooldown,
		"hasToken": c.Token != "", "state": b.state, "detail": b.detail, "login": b.login,
		"redirect": b.redirectURI(),
	}
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(resp)
}

func (b *twitchBot) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !b.allowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in struct {
		twitchConfig
		Logout bool `json:"logout"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	token := b.cfg.Token
	if in.Token != "" {
		token = strings.TrimPrefix(strings.TrimSpace(in.Token), "oauth:")
	}
	refresh := b.cfg.Refresh
	if in.Logout {
		token, refresh = "", ""
		b.login = ""
	}
	in.twitchConfig.Token = token
	in.twitchConfig.Refresh = refresh
	in.twitchConfig.ClientID = strings.TrimSpace(in.ClientID)
	in.twitchConfig.defaults()
	b.cfg = in.twitchConfig
	err := b.save()
	b.mu.Unlock()
	b.kick()
	if err != nil {
		http.Error(w, "could not save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b.handleGet(w, r)
}

// handleLogin starts Twitch's device login: no redirect page needed. The app
// shows a code, opens twitch.tv/activate, and waits until the user approves.
func (b *twitchBot) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !b.allowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	b.mu.Lock()
	cid := b.cfg.ClientID
	b.mu.Unlock()
	if cid == "" {
		http.Error(w, "Paste the Client ID first", http.StatusBadRequest)
		return
	}
	var dev struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
		Message         string `json:"message"`
	}
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.PostForm(twitchOAuthBase+"/device", url.Values{"client_id": {cid}, "scopes": {"chat:read chat:edit"}})
	if err != nil {
		http.Error(w, "Can't reach Twitch (check internet)", http.StatusBadGateway)
		return
	}
	json.NewDecoder(resp.Body).Decode(&dev)
	resp.Body.Close()
	if resp.StatusCode != 200 || dev.DeviceCode == "" {
		msg := "Twitch refused the Client ID. Check it, and that Client Type is Public."
		if dev.Message != "" {
			msg += " (" + dev.Message + ")"
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if dev.Interval <= 0 {
		dev.Interval = 5
	}
	b.mu.Lock()
	b.oauth = dev.DeviceCode
	b.mu.Unlock()
	b.setState("connecting", "Approve NowPlaying in your browser (code "+dev.UserCode+")")

	go func(deviceCode string, interval, expires int) {
		deadline := time.Now().Add(time.Duration(expires) * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(time.Duration(interval) * time.Second)
			b.mu.Lock()
			current := b.oauth == deviceCode
			b.mu.Unlock()
			if !current {
				return // a newer login was started
			}
			t, code, err := twitchPost("/token", url.Values{
				"client_id": {cid}, "device_code": {deviceCode}, "scopes": {"chat:read chat:edit"},
				"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
			})
			if err != nil {
				continue
			}
			if code == 200 && t.AccessToken != "" {
				b.mu.Lock()
				b.oauth = ""
				b.cfg.Token, b.cfg.Refresh, b.cfg.Enabled = t.AccessToken, t.RefreshToken, true
				b.save()
				b.mu.Unlock()
				b.kick()
				return
			}
			if !strings.Contains(t.Message, "pending") && !strings.Contains(t.Message, "slow_down") {
				b.setState("error", "Twitch login was cancelled or expired, click Connect Twitch again")
				return
			}
		}
		b.setState("error", "Twitch login timed out, click Connect Twitch again")
	}(dev.DeviceCode, dev.Interval, dev.ExpiresIn)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": dev.VerificationURI, "code": dev.UserCode})
}
