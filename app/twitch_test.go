package main

import (
	"bufio"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSongCommand(t *testing.T) {
	val := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "OAuth goodtoken" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"login":"deys","scopes":["chat:read","chat:edit"],"expires_in":5000000}`))
	}))
	defer val.Close()
	twitchValidateURL = val.URL

	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	cert := srv.TLS.Certificates[0]
	srv.Close()
	ln, _ := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	defer ln.Close()
	twitchIRCAddr = ln.Addr().String()
	twitchTLS = &tls.Config{InsecureSkipVerify: true}

	got := make(chan []string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(c)
		var lines []string
		for i := 0; i < 4; i++ { // CAP, PASS, NICK, JOIN
			l, _ := r.ReadString('\n')
			lines = append(lines, strings.TrimSpace(l))
		}
		c.Write([]byte(":tmi.twitch.tv ROOMSTATE #deys\r\n"))
		c.Write([]byte("PING :tmi.twitch.tv\r\n"))
		c.Write([]byte("@display-name=Viewer1;id=abc-123 :viewer1!viewer1@viewer1.tmi.twitch.tv PRIVMSG #deys :hello\r\n"))
		c.Write([]byte("@display-name=Viewer1;id=abc-123 :viewer1!viewer1@viewer1.tmi.twitch.tv PRIVMSG #deys :!Song please\r\n"))
		c.Write([]byte("@display-name=Viewer2;id=def :viewer2!viewer2@x PRIVMSG #deys :!song\r\n")) // cooldown: ignored
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				break
			}
			lines = append(lines, strings.TrimSpace(l))
		}
		c.Close()
		got <- lines
	}()

	h := newHub("")
	h.set(Track{Title: "Something About Us", Artist: "Daft Punk", Source: "youtube", URL: "https://www.youtube.com/watch?v=abc123&list=x", Playing: true, Active: true})
	b := &twitchBot{hub: h, restart: make(chan struct{}, 1)}
	cfg := twitchConfig{Enabled: true, Token: "oauth:goodtoken"}
	cfg.defaults()
	go b.session(cfg)

	lines := <-got
	t.Log(strings.Join(lines, "\n"))
	want := []string{
		"PASS oauth:goodtoken", "NICK deys", "JOIN #deys", "PONG :tmi.twitch.tv",
		"@reply-parent-msg-id=abc-123 PRIVMSG #deys :🎵 Daft Punk - Something About Us → https://youtu.be/abc123",
	}
	all := strings.Join(lines, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q", w)
		}
	}
	if strings.Count(all, "PRIVMSG") != 1 {
		t.Errorf("expected exactly one reply (cooldown)")
	}
	if b.state != "connected" {
		t.Errorf("state = %s", b.state)
	}

	// nothing playing
	h.set(Track{})
	if got := b.replyText(cfg, "x"); got != "Right now nothing is playing." {
		t.Errorf("nothing reply = %q", got)
	}
}

func TestBadToken(t *testing.T) {
	val := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer val.Close()
	twitchValidateURL = val.URL
	if _, err := validateToken("x"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal(err)
	}
}

func TestSoundCloudReplyHasNoLink(t *testing.T) {
	h := newHub("")
	h.set(Track{Title: "Delilah", Artist: "Fred again..", Source: "soundcloud", URL: "https://soundcloud.com/fred/delilah", Playing: true, Active: true})
	b := &twitchBot{hub: h}
	cfg := twitchConfig{}
	cfg.defaults()
	if got := b.replyText(cfg, "x"); got != "🎵 Fred again.. - Delilah" {
		t.Fatalf("got %q", got)
	}
}
