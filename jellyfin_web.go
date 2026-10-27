package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Jellyfin in the web remote's settings: the config form (no token ever goes
// to the browser) and the Quick Connect sign-in, which runs on the MiSTer and
// is polled by the page.

// jellyfinPollInterval is how often a pending Quick Connect code is checked.
var jellyfinPollInterval = 2 * time.Second

// jfLogin is the one sign-in that can be pending at a time.
var jfLogin struct {
	sync.Mutex
	state   string // "", "pending", "signed_in", "error"
	code    string
	message string
	cancel  context.CancelFunc
}

func (w *webRemote) handleJellyfinConfig(rw http.ResponseWriter, r *http.Request) {
	handleJFConfig(rw, r, false)
}
func (w *webRemote) handleEmbyConfig(rw http.ResponseWriter, r *http.Request) {
	handleJFConfig(rw, r, true)
}

func handleJFConfig(rw http.ResponseWriter, r *http.Request, emby bool) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := loadJF(emby)
		if err != nil {
			cfg = JellyfinConfig{Scrobbling: "server", MaxTrackMB: 128, emby: emby}
			// a file that exists but is broken: show what is in it
			if c, e2 := loadJFRaw(emby); e2 == nil {
				cfg = c
			}
		}
		signedIn := cfg.Token != "" && cfg.UserID != ""
		cfg.Token, cfg.UserID = "", ""
		writeJSON(rw, map[string]any{"config": cfg, "configured": cfg.Server != "", "signed_in": signedIn})
	case http.MethodPost:
		if !settingsRequest(rw, r) {
			return
		}
		var in JellyfinConfig
		if err := decodeSettings(r, &in); err != nil {
			http.Error(rw, "invalid configuration", 400)
			return
		}
		in.emby = emby
		old, _ := loadJF(emby)
		in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
		if !isHTTPURL(in.Server) {
			http.Error(rw, "server must be an http(s) URL", 400)
			return
		}
		// the token and user belong to the server they were issued by
		if in.Server == old.Server {
			in.Token, in.UserID = old.Token, old.UserID
		} else {
			in.Token, in.UserID = "", ""
		}
		if in.Library != "" && !jellyfinID(in.Library) {
			http.Error(rw, "library must be a library id", 400)
			return
		}
		if in.Scrobbling == "" {
			in.Scrobbling = "server"
		}
		if in.Scrobbling != "server" && !isOwnScrobbleMode(in.Scrobbling) && in.Scrobbling != "none" {
			http.Error(rw, "invalid scrobbling mode", 400)
			return
		}
		if in.MaxTrackMB == 0 {
			in.MaxTrackMB = 128
		}
		if in.MaxTrackMB < 1 || in.MaxTrackMB > 1024 {
			http.Error(rw, "track limit must be 1–1024 MB", 400)
			return
		}
		if err := saveJellyfin(in); err != nil {
			http.Error(rw, "cannot save "+in.name()+" configuration", 500)
			return
		}
		writeJSON(rw, map[string]any{"saved": true, "signed_in": in.Token != ""})
	default:
		http.Error(rw, "method not allowed", 405)
	}
}

// handleJellyfinQuickConnect: POST {"action":"start"|"cancel"} and GET (status).
func (w *webRemote) handleJellyfinQuickConnect(rw http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		jfLogin.Lock()
		out := map[string]any{"state": jfLogin.state, "code": jfLogin.code, "message": jfLogin.message}
		jfLogin.Unlock()
		writeJSON(rw, out)
		return
	}
	if !settingsRequest(rw, r) {
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if err := decodeSettings(r, &in); err != nil {
		http.Error(rw, "invalid request", 400)
		return
	}
	switch in.Action {
	case "cancel":
		jellyfinLoginCancel("cancelled")
		writeJSON(rw, map[string]bool{"ok": true})
	case "start":
		cfg, err := loadJellyfin()
		if err != nil {
			http.Error(rw, "save the server address first: "+err.Error(), 400)
			return
		}
		jellyfinLoginCancel("")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		cfg.Token, cfg.UserID = "", ""
		qc, err := cfg.quickConnectStart(ctx)
		if err != nil {
			cancel()
			http.Error(rw, err.Error(), 502)
			return
		}
		jfLogin.Lock()
		jfLogin.state, jfLogin.code, jfLogin.message, jfLogin.cancel = "pending", qc.Code, "", cancel
		jfLogin.Unlock()
		go jellyfinLoginWait(ctx, cfg, qc)
		writeJSON(rw, map[string]string{"code": qc.Code})
	default:
		http.Error(rw, "unknown action", 400)
	}
}

func jellyfinLoginCancel(reason string) {
	jfLogin.Lock()
	cancel := jfLogin.cancel
	jfLogin.cancel = nil
	if reason != "" && jfLogin.state == "pending" {
		jfLogin.state, jfLogin.message = "error", reason
	}
	jfLogin.Unlock()
	if cancel != nil {
		cancel()
	}
}

// jellyfinLoginWait polls until the code was entered in Jellyfin, then saves
// the token. It ends with the context (cancel or three minutes).
func jellyfinLoginWait(ctx context.Context, cfg JellyfinConfig, qc jfQuickConnect) {
	finish := func(state, msg string) {
		jfLogin.Lock()
		if jfLogin.state == "pending" || state == "signed_in" {
			jfLogin.state, jfLogin.message = state, msg
		}
		jfLogin.cancel = nil
		jfLogin.Unlock()
	}
	tick := time.NewTicker(jellyfinPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				finish("error", "sign-in timed out")
			}
			return
		case <-tick.C:
			ok, err := cfg.quickConnectApproved(ctx, qc.Secret)
			if err != nil {
				if ctx.Err() == nil {
					finish("error", err.Error())
				}
				return
			}
			if ok {
				// keep what the form saved meanwhile: only token and user are new
				latest, lerr := loadJellyfin()
				if lerr == nil && latest.Server == cfg.Server {
					cfg = latest
				}
				cfg.Token, cfg.UserID = "", ""
				if _, err := cfg.quickConnectFinish(ctx, qc.Secret); err != nil {
					finish("error", err.Error())
				} else {
					finish("signed_in", "")
				}
				return
			}
		}
	}
}
