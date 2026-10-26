package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceLoginAndRefresh(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/device":
			if r.Form.Get("client_id") != "cid" || r.Form.Get("scopes") != "chat:read chat:edit" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"device_code":"DEV","user_code":"ABCD1234","verification_uri":"https://www.twitch.tv/activate?device-code=ABCD1234","interval":1,"expires_in":30}`))
		case "/token":
			switch r.Form.Get("grant_type") {
			case "urn:ietf:params:oauth:grant-type:device_code":
				if polls.Add(1) < 2 {
					w.WriteHeader(400)
					w.Write([]byte(`{"status":400,"message":"authorization_pending"}`))
					return
				}
				w.Write([]byte(`{"access_token":"tok1","refresh_token":"ref1"}`))
			case "refresh_token":
				if r.Form.Get("refresh_token") != "ref1" {
					w.WriteHeader(400)
					return
				}
				w.Write([]byte(`{"access_token":"tok2","refresh_token":"ref2"}`))
			}
		case "/validate":
			if r.Header.Get("Authorization") != "OAuth tok2" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"login":"deys","scopes":["chat:read","chat:edit"]}`))
		}
	}))
	defer srv.Close()
	twitchOAuthBase = srv.URL
	twitchValidateURL = srv.URL + "/validate"

	store := &configStore{path: filepath.Join(t.TempDir(), "config.json")}
	store.cfg.Twitch.ClientID = "cid"
	b := newTwitchBot(newHub(""), store, 8787)

	req := httptest.NewRequest("POST", "/api/twitch/login", nil)
	req.Header.Set("X-NowPlaying", "1")
	rec := httptest.NewRecorder()
	b.handleLogin(rec, req)
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["code"] != "ABCD1234" || !strings.Contains(out["url"], "activate") {
		t.Fatalf("login response: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(b.detail, "ABCD1234") {
		t.Errorf("detail = %q", b.detail)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		tok := b.cfg.Token
		b.mu.Unlock()
		if tok == "tok1" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b.cfg.Token != "tok1" || b.cfg.Refresh != "ref1" || !b.cfg.Enabled {
		t.Fatalf("after device login cfg = %+v", b.cfg)
	}
	if store.get().Twitch.Refresh != "ref1" {
		t.Fatalf("not saved")
	}

	// tok1 is "expired" by the fake validate endpoint -> refresh to tok2
	tok, v, err := b.freshToken(b.cfg)
	if err != nil || tok != "tok2" || v.Login != "deys" || store.get().Twitch.Refresh != "ref2" {
		t.Fatalf("refresh: tok=%s v=%+v err=%v saved=%+v", tok, v, err, store.get().Twitch)
	}
}
