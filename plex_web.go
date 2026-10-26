package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func plexID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Virtual paths contain only category names and Plex numeric IDs, never URLs.
func plexBrowse(ctx context.Context, cfg PlexConfig, folder string) ([]webEntry, []Track, error) {
	folder = path.Clean("/" + folder)
	if folder == "/" {
		return []webEntry{{Name: "Artists", Path: "/artists", IsDir: true}, {Name: "Albums", Path: "/albums", IsDir: true}, {Name: "Recently added", Path: "/recent", IsDir: true}, {Name: "Playlists", Path: "/playlists", IsDir: true}}, nil, nil
	}
	parts := strings.Split(strings.Trim(folder, "/"), "/")
	for _, id := range parts[1:] {
		if !plexID(id) {
			return nil, nil, errors.New("invalid Plex path")
		}
	}
	var endpoint string
	q := url.Values{}
	limit := 0
	trackList := false
	artistLabel := false
	section := func() (string, error) {
		if cfg.Library != "" {
			return cfg.Library, nil
		}
		list, err := cfg.listContext(ctx, "/library/sections", nil, 0)
		if err != nil {
			return "", err
		}
		for _, s := range list {
			if s.Type == "artist" {
				return s.Key, nil
			}
		}
		return "", errors.New("no Plex music library")
	}
	switch parts[0] {
	case "artists":
		if len(parts) == 1 {
			sec, err := section()
			if err != nil {
				return nil, nil, err
			}
			endpoint = "/library/sections/" + sec + "/all"
			q.Set("type", "8")
			q.Set("sort", "titleSort")
		} else if len(parts) == 2 {
			endpoint = "/library/metadata/" + parts[1] + "/children"
		} else if len(parts) == 3 {
			endpoint = "/library/metadata/" + parts[2] + "/children"
			trackList = true
		}
	case "albums", "recent":
		if len(parts) == 1 {
			sec, err := section()
			if err != nil {
				return nil, nil, err
			}
			endpoint = "/library/sections/" + sec + "/all"
			q.Set("type", "9")
			q.Set("sort", "artist.titleSort,titleSort")
			artistLabel = true
			if parts[0] == "recent" {
				q.Set("sort", "addedAt:desc")
				limit = 100
			}
		} else if len(parts) == 2 {
			endpoint = "/library/metadata/" + parts[1] + "/children"
			trackList = true
		}
	case "playlists":
		if len(parts) == 1 {
			endpoint = "/playlists"
			q.Set("playlistType", "audio")
		} else if len(parts) == 2 {
			endpoint = "/playlists/" + parts[1] + "/items"
			trackList = true
		}
	}
	if endpoint == "" {
		return nil, nil, errors.New("invalid Plex path")
	}
	metas, err := cfg.listContext(ctx, endpoint, q, limit)
	if err != nil {
		return nil, nil, err
	}
	entries := make([]webEntry, 0, len(metas))
	tracks := make([]Track, 0, len(metas))
	for _, m := range metas {
		if trackList {
			t := plexTrack(m)
			t.PlexSource = &cfg
			if t.PlexPart == "" {
				continue
			}
			name := t.Title
			if parts[0] == "playlists" && t.Artist != "" {
				name = t.Artist + " - " + name
			} else if m.Index > 0 {
				name = fmt.Sprintf("%02d  %s", m.Index, name)
			}
			entries = append(entries, webEntry{Name: name, Path: folder + "/track/" + strconv.Itoa(len(tracks))})
			tracks = append(tracks, t)
		} else {
			if !plexID(m.RatingKey) {
				continue
			}
			name := m.Title
			if artistLabel && m.ParentTitle != "" {
				name = m.ParentTitle + " - " + name
			}
			entries = append(entries, webEntry{Name: name, Path: folder + "/" + m.RatingKey, IsDir: true})
		}
	}
	return entries, tracks, nil
}
func plexWebQueue(ctx context.Context, selected string) (Queue, error) {
	cfg, err := loadPlex()
	if err != nil {
		return Queue{}, err
	}
	i := strings.LastIndex(selected, "/track/")
	if i < 0 {
		return Queue{}, errors.New("invalid Plex track")
	}
	index, err := strconv.Atoi(selected[i+7:])
	if err != nil {
		return Queue{}, errors.New("invalid Plex track")
	}
	_, tracks, err := plexBrowse(ctx, cfg, selected[:i])
	if err != nil {
		return Queue{}, err
	}
	if index < 0 || index >= len(tracks) {
		return Queue{}, errors.New("Plex track unavailable")
	}
	return Queue{Tracks: tracks, Index: index}, nil
}
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
		if cfg.Scrobbling != "plex" && cfg.Scrobbling != "lastfm" && cfg.Scrobbling != "none" {
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
