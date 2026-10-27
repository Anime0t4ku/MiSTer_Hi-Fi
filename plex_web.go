package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (w *webRemote) handlePlexConfig(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := loadPlex()
		if err != nil {
			cfg = PlexConfig{Scrobbling: "plex", MaxTrackMB: 128}
		}
		configured := cfg.Token != ""
		cfg.Token = ""
		writeJSON(rw, map[string]any{"config": cfg, "configured": configured})
	case http.MethodPost:
		// Configuration writes must originate in this web remote, not another site.
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(rw, "invalid origin", 403)
				return
			}
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(rw, "JSON required", 415)
			return
		}
		var cfg PlexConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&cfg); err != nil {
			http.Error(rw, "invalid configuration", 400)
			return
		}
		old, _ := loadPlex()
		cfg.Server = strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
		cfg.Token = strings.TrimSpace(cfg.Token)
		if cfg.Token == "" && cfg.Server == old.Server {
			cfg.Token = old.Token
		}
		if !isHTTPURL(cfg.Server) || cfg.Token == "" {
			http.Error(rw, "server and token required (new token required when changing server)", 400)
			return
		}
		if cfg.Library != "" && !plexID(cfg.Library) {
			http.Error(rw, "library must be a numeric section ID", 400)
			return
		}
		if cfg.Scrobbling == "" {
			cfg.Scrobbling = "plex"
		}
		if cfg.Scrobbling != "plex" && !isOwnScrobbleMode(cfg.Scrobbling) && cfg.Scrobbling != "none" {
			http.Error(rw, "invalid scrobbling mode", 400)
			return
		}
		if cfg.MaxTrackMB == 0 {
			cfg.MaxTrackMB = 128
		}
		if cfg.MaxTrackMB < 1 || cfg.MaxTrackMB > 1024 {
			http.Error(rw, "track limit must be 1–1024 MB", 400)
			return
		}
		if err := os.MkdirAll(filepath.Dir(plexConfigPath), 0755); err != nil {
			http.Error(rw, "cannot save Plex configuration", 500)
			return
		}
		b, _ := json.MarshalIndent(cfg, "", "  ")
		f, err := os.CreateTemp(filepath.Dir(plexConfigPath), "plex-*.tmp")
		if err != nil {
			http.Error(rw, "cannot save Plex configuration", 500)
			return
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(b); err != nil {
			f.Close()
			http.Error(rw, "cannot save Plex configuration", 500)
			return
		}
		if err = f.Close(); err == nil {
			err = os.Rename(f.Name(), plexConfigPath)
		}
		if err != nil {
			http.Error(rw, "cannot save Plex configuration", 500)
			return
		}
		writeJSON(rw, map[string]bool{"saved": true})
	default:
		http.Error(rw, "method not allowed", 405)
	}
}
