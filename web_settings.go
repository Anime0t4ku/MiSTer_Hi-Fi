package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type settingsTask struct {
	redraw bool
	ctx    context.Context
	fn     func() (any, error)
	reply  chan settingsResult
}
type settingsResult struct {
	value any
	err   error
}

func (task settingsTask) run() {
	if err := task.ctx.Err(); err != nil {
		task.reply <- settingsResult{err: err}
		return
	}
	value, err := task.fn()
	task.reply <- settingsResult{value, err}
}

// Config reads and writes run on the same thread as the framebuffer UI.
func (a *App) settingsCall(ctx context.Context, fn func() (any, error), redraw ...bool) (any, error) {
	if a == nil || a.settingsRequests == nil {
		return nil, errors.New("settings unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	task := settingsTask{ctx: ctx, fn: fn, reply: make(chan settingsResult, 1)}
	if len(redraw) > 0 {
		task.redraw = redraw[0]
	}
	select {
	case a.settingsRequests <- task:
	case <-ctx.Done():
		return nil, errors.New("MiSTer is busy; please try again")
	}
	// Once the UI accepts a task, wait for its result: callers must not close
	// queue descriptors or report a failed save while that task is still applying.
	result := <-task.reply
	return result.value, result.err
}
func atomicSettingsWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func settingsJSONWrite(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicSettingsWrite(path, b)
}
func settingsRequest(rw http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", 405)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(rw, "invalid origin", 403)
			return false
		}
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(rw, "JSON required", 415)
		return false
	}
	return true
}
func decodeSettings(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid settings")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid settings")
	}
	return nil
}

type settingsOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}
type settingsField struct {
	Path    string           `json:"path"`
	Label   string           `json:"label"`
	Type    string           `json:"type"`
	Group   string           `json:"group"`
	Options []settingsOption `json:"options,omitempty"`
	Min     float64          `json:"min,omitempty"`
	Max     float64          `json:"max,omitempty"`
	Step    float64          `json:"step,omitempty"`
	Help    string           `json:"help,omitempty"`
	Invert  bool             `json:"invert,omitempty"`
}

func appSettingsFields() []settingsField {
	fields := []settingsField{}
	check := func(path, label, group string) {
		fields = append(fields, settingsField{Path: path, Label: label, Type: "checkbox", Group: group})
	}
	number := func(path, label, group string, min, max, step float64) {
		fields = append(fields, settingsField{Path: path, Label: label, Type: "number", Group: group, Min: min, Max: max, Step: step})
	}
	check("oled_mode", "OLED mode", "Display")
	check("hide_album_art", "Show album art", "Display")
	fields[len(fields)-1].Invert = true
	check("auto_hide_missing_art", "Auto hide missing art", "Display")
	check("prioritize_external_art", "Prioritize external cover art", "Display")
	check("show_clock", "Show clock", "Display")
	opts := []settingsOption{}
	for _, v := range screenSaverOptions {
		opts = append(opts, settingsOption{Value: v, Label: screenSaverLabel(v)})
	}
	fields = append(fields, settingsField{Path: "screensaver_seconds", Label: "Screensaver", Type: "select", Group: "Display", Options: opts})
	fonts := []settingsOption{{Value: "", Label: "Off"}}
	for _, font := range scanCustomFonts() {
		fonts = append(fonts, settingsOption{Value: font.File, Label: font.Name})
	}
	fields = append(fields, settingsField{Path: "custom_font", Label: "Custom fallback font", Type: "select", Group: "Display", Options: fonts, Help: "Used only for characters missing from the built-in font. Add .ttf or .otf files to the Hi-Fi fonts folder."})
	fields = append(fields, settingsField{Path: "visualizer", Label: "Visualizer", Type: "select", Group: "Display", Options: []settingsOption{{Value: "bars", Label: "Spectrum bars"}}})
	check("remember_shuffle_loop", "Remember shuffle / loop", "Playback")
	check("confirm_on_exit", "Confirm on exit", "Playback")
	check("gapless_playback", "Gapless playback (experimental)", "Playback")
	fields[len(fields)-1].Help = "FLAC, WAV and CDDA only; Plex remains non-gapless."
	number("volume", "Volume", "Playback", 0, 100, 1)
	check("muted", "Muted", "Playback")
	check("saved_shuffle", "Remembered shuffle", "Playback")
	check("saved_loop", "Remembered loop", "Playback")
	check("saved_loop_one", "Remembered loop one", "Playback")
	check("swap_ab", "Swap A/B", "Controls and navigation")
	check("swap_xy", "Swap X/Y", "Controls and navigation")
	number("navigation.jump_entries", "List jump size", "Controls and navigation", 1, 10000, 1)
	number("navigation.page_jump_entries", "Page jump size", "Controls and navigation", 1, 10000, 1)
	check("navigation.wrap", "Wrap list navigation", "Controls and navigation")
	check("navigation.hold_repeat", "Hold to repeat", "Controls and navigation")
	number("navigation.hold_delay_ms", "Hold delay (ms)", "Controls and navigation", 100, 5000, 1)
	number("navigation.repeat_interval_ms", "Repeat interval (ms)", "Controls and navigation", 50, 2000, 1)
	check("eq.enabled", "Enabled", "Equalizer")
	for _, band := range []struct{ key, label string }{{"Bass", "Bass 60 Hz"}, {"LowMid", "Low-mid 250 Hz"}, {"Mid", "Mid 1 kHz"}, {"HighMid", "High-mid 4 kHz"}, {"Treble", "Treble 12 kHz"}} {
		number("eq."+band.key, band.label+" (dB)", "Equalizer", -6, 6, 0.5)
	}
	check("web_remote_enabled", "Enable Web Remote", "Web Remote")
	number("web_remote_port", "Port", "Web Remote", 1, 65535, 1)
	fields[len(fields)-1].Help = "Changes to Web Remote enabled/port take effect after restarting Hi-Fi."
	check("lastfm.enabled", "Enable Last.fm", "Last.fm")
	for _, field := range []struct{ key, label, kind string }{{"username", "Username", "text"}, {"api_key", "API key", "password"}, {"api_secret", "API secret", "password"}, {"session_key", "Session key", "password"}} {
		fields = append(fields, settingsField{Path: "lastfm." + field.key, Label: field.label, Type: field.kind, Group: "Last.fm", Help: ""})
	}
	fields[len(fields)-1].Help = "Blank credential fields keep saved values. Obtain a session key with mister_hifi --lastfm-auth."
	return fields
}
func redactAppConfig(c Config) Config {
	c.LastFM.APIKey = ""
	c.LastFM.APISecret = ""
	c.LastFM.SessionKey = ""
	return c
}
func settingsSnapshot(a *App) any {
	return map[string]any{"config": redactAppConfig(*a.cfg), "fields": appSettingsFields(), "presets": eqPresets, "lastfm_configured": a.cfg.LastFM.ready(), "version": version}
}
func mergeSettingsMap(dst, patch map[string]any) {
	for key, value := range patch {
		if sub, ok := value.(map[string]any); ok {
			if target, ok := dst[key].(map[string]any); ok {
				mergeSettingsMap(target, sub)
				continue
			}
		}
		dst[key] = value
	}
}
func patchedAppConfig(current Config, patch map[string]any) (Config, error) {
	fields := appSettingsFields()
	allowed := map[string]bool{}
	for _, f := range fields {
		allowed[f.Path] = true
	}
	var validateKeys func(map[string]any, string) error
	validateKeys = func(m map[string]any, prefix string) error {
		for k, v := range m {
			key := prefix + k
			if sub, ok := v.(map[string]any); ok {
				if err := validateKeys(sub, key+"."); err != nil {
					return err
				}
			} else if !allowed[key] {
				return fmt.Errorf("unknown setting: %s", key)
			}
		}
		return nil
	}
	if err := validateKeys(patch, ""); err != nil {
		return current, err
	}
	b, _ := json.Marshal(current)
	merged := map[string]any{}
	_ = json.Unmarshal(b, &merged)
	mergeSettingsMap(merged, patch)
	b, err := json.Marshal(merged)
	if err != nil {
		return current, err
	}
	next := current
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&next); err != nil {
		return current, errors.New("settings have invalid values")
	}
	// Empty password inputs preserve the current credentials.
	if next.LastFM.APIKey == "" {
		next.LastFM.APIKey = current.LastFM.APIKey
	}
	if next.LastFM.APISecret == "" {
		next.LastFM.APISecret = current.LastFM.APISecret
	}
	if next.LastFM.SessionKey == "" {
		next.LastFM.SessionKey = current.LastFM.SessionKey
	}
	for _, field := range fields {
		var raw map[string]any
		_ = json.Unmarshal(b, &raw)
		var value any = raw
		for _, part := range strings.Split(field.Path, ".") {
			if obj, ok := value.(map[string]any); ok {
				value = obj[part]
			}
		}
		if value == nil {
			return current, fmt.Errorf("%s cannot be null", field.Label)
		}
		if field.Type == "number" {
			n, ok := value.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < field.Min || n > field.Max {
				return current, fmt.Errorf("%s must be between %g and %g", field.Label, field.Min, field.Max)
			}
		}
		if field.Type == "select" {
			found := false
			for _, option := range field.Options {
				if fmt.Sprint(value) == fmt.Sprint(option.Value) {
					found = true
				}
			}
			if !found {
				return current, fmt.Errorf("invalid %s", field.Label)
			}
		}
	}
	if next.SavedLoopOne && !next.SavedLoop {
		return current, errors.New("remembered loop one requires remembered loop")
	}
	return next, nil
}
func (a *App) applyWebConfig(next Config) error {
	old := *a.cfg
	if next.RememberShuffleLoop && !old.RememberShuffleLoop && a.player != nil {
		a.player.mu.Lock()
		next.SavedShuffle = a.player.q.Shuffle
		next.SavedLoop = a.player.q.Repeat
		next.SavedLoopOne = a.player.q.RepeatOne
		a.player.mu.Unlock()
	}
	if err := settingsJSONWrite(appConfigPath, next); err != nil {
		return errors.New("could not save app settings")
	}
	*a.cfg = next
	applyCustomFont(a.cfg, scanCustomFonts())
	swapABInput.Store(next.SwapAB)
	swapXYInput.Store(next.SwapXY)
	screenSaverSeconds.Store(int64(next.ScreenSaverSeconds))
	configureNavigation(next.Navigation)
	if p := a.player; p != nil {
		p.opMu.Lock()
		p.mu.Lock()
		queued := p.gaplessQueuedIndex
		p.cfg = *a.cfg
		active := !p.stopped && !p.loading
		paused := p.paused
		p.mu.Unlock()
		nativeAudioSetEQ(next.EQ)
		if old.GaplessPlayback != next.GaplessPlayback && active {
			if queued >= 0 && !next.GaplessPlayback {
				_ = p.restartCurrentFileAtPositionUnlocked(nativeAudioPosition(), paused)
			} else {
				nativeAudioClearQueuedNext()
				p.prepareGaplessNextFile()
			}
		}
		p.opMu.Unlock()
	}
	nativeAudioSetVolume(next.Volume, next.Muted)
	a.requestPlayerRefresh()
	return nil
}
func (w *webRemote) handleAppSettings(rw http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := w.app.settingsCall(r.Context(), func() (any, error) { return settingsSnapshot(w.app), nil })
		if err != nil {
			http.Error(rw, err.Error(), 503)
			return
		}
		writeJSON(rw, value)
		return
	}
	if !settingsRequest(rw, r) {
		return
	}
	patch := map[string]any{}
	if err := decodeSettings(r, &patch); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	value, err := w.app.settingsCall(r.Context(), func() (any, error) {
		next, err := patchedAppConfig(*w.app.cfg, patch)
		if err != nil {
			return nil, err
		}
		restart := next.WebRemoteEnabled != w.app.cfg.WebRemoteEnabled || next.WebRemotePort != w.app.cfg.WebRemotePort
		if err = w.app.applyWebConfig(next); err != nil {
			return nil, err
		}
		return map[string]any{"saved": true, "restart_required": restart, "config": redactAppConfig(*w.app.cfg)}, nil
	}, true)
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	writeJSON(rw, value)
}
func registerWebSettings(mux *http.ServeMux, w *webRemote) {
	mux.HandleFunc("/api/settings/app", w.handleAppSettings)
	mux.HandleFunc("/api/settings/smb", w.handleSMBSettings)
	mux.HandleFunc("/api/settings/radio", w.handleRadioSettings)
	mux.HandleFunc("/api/settings/test", w.handleSourceTest)
	mux.HandleFunc("/settings.js", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		rw.Header().Set("Cache-Control", "no-store")
		io.WriteString(rw, webSettingsJS)
	})
	mux.HandleFunc("/settings.css", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/css; charset=utf-8")
		io.WriteString(rw, webSettingsCSS)
	})
}
