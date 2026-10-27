package main

import (
	"context"
	"net/http"
)

func (w *webRemote) gbaWebEnabled(ctx context.Context) (bool, error) {
	value, err := w.app.settingsCall(ctx, func() (any, error) { return w.app.cfg.GBAMusicEnabled, nil })
	if err != nil {
		return false, err
	}
	return value.(bool), nil
}
func (w *webRemote) handleGBABrowse(rw http.ResponseWriter, r *http.Request) {
	enabled, err := w.gbaWebEnabled(r.Context())
	if err != nil {
		http.Error(rw, err.Error(), 503)
		return
	}
	if !enabled {
		http.Error(rw, "GBA Music is disabled", 403)
		return
	}
	page, err := gbaBrowse(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	writeJSON(rw, page)
}
