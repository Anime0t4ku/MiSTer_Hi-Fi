package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var sourceSettingsMu sync.Mutex

func sourceDocument(path string, dst any) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", errors.New("could not read source settings")
	}
	if err = json.Unmarshal(b, dst); err != nil {
		return "", errors.New("source JSON is invalid; correct the file before editing")
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

type smbSettingsEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Server        string `json:"server"`
	Share         string `json:"share"`
	Path          string `json:"path"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Guest         bool   `json:"guest"`
	HasPassword   bool   `json:"has_password"`
	ClearPassword bool   `json:"clear_password"`
}

func smbEditable(s SMBShare, index int) smbSettingsEntry {
	return smbSettingsEntry{ID: strconv.Itoa(index), Name: s.Name, Server: s.Server, Share: s.Share, Path: s.Path, Username: s.Username, Guest: s.Guest, HasPassword: s.Password != ""}
}
func resolveSMBEntry(e smbSettingsEntry, old SMBConfig) (SMBShare, error) {
	s := SMBShare{Name: strings.TrimSpace(e.Name), Server: strings.TrimSpace(e.Server), Share: strings.TrimSpace(e.Share), Path: strings.TrimSpace(e.Path), Username: strings.TrimSpace(e.Username), Password: e.Password, Guest: e.Guest}
	if s.Server == "" || strings.ContainsAny(s.Server, "/\\ ,\t\r\n") || s.Share == "" || strings.ContainsAny(s.Share, "/\\\r\n") {
		return s, errors.New("SMB server and share name are required (enter the server without smb://)")
	}
	s.Path = strings.ReplaceAll(s.Path, `\`, "/")
	if strings.HasPrefix(s.Path, "/") || s.Path == ".." || strings.Contains(s.Path, "../") {
		return s, errors.New("SMB folder must stay inside the share")
	}
	if strings.ContainsAny(s.Username, "\r\n") || strings.ContainsAny(s.Password, "\r\n") {
		return s, errors.New("SMB credentials cannot contain line breaks")
	}
	if e.ID != "" {
		index, err := strconv.Atoi(e.ID)
		if err != nil || index < 0 || index >= len(old.Shares) {
			return s, errors.New("saved SMB share is no longer available; reload settings")
		}
		prev := old.Shares[index]
		if !s.Guest && !e.ClearPassword && s.Password == "" {
			if s.Server != prev.Server || s.Username != prev.Username {
				if prev.Password != "" {
					return s, errors.New("enter a password again when changing the SMB server or username")
				}
			} else {
				s.Password = prev.Password
			}
		}
	}
	if s.Guest || e.ClearPassword {
		s.Password = ""
	}
	return s, nil
}
func (w *webRemote) handleSMBSettings(rw http.ResponseWriter, r *http.Request) {
	sourceSettingsMu.Lock()
	defer sourceSettingsMu.Unlock()
	old := SMBConfig{}
	revision, err := sourceDocument(smbConfigPath, &old)
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	if r.Method == http.MethodGet {
		entries := make([]smbSettingsEntry, 0, len(old.Shares))
		for i, s := range old.Shares {
			entries = append(entries, smbEditable(s, i))
		}
		writeJSON(rw, map[string]any{"revision": revision, "shares": entries})
		return
	}
	if !settingsRequest(rw, r) {
		return
	}
	var request struct {
		Revision string              `json:"revision"`
		Shares   *[]smbSettingsEntry `json:"shares"`
	}
	if err = decodeSettings(r, &request); err != nil || request.Shares == nil {
		http.Error(rw, "invalid SMB settings", 400)
		return
	}
	if request.Revision != revision {
		http.Error(rw, "SMB settings changed; reload before saving", 409)
		return
	}
	if len(*request.Shares) > 100 {
		http.Error(rw, "maximum 100 SMB shares", 400)
		return
	}
	next := SMBConfig{Shares: []SMBShare{}}
	seen := map[string]bool{}
	for _, entry := range *request.Shares {
		if entry.ID != "" {
			if seen[entry.ID] {
				http.Error(rw, "duplicate SMB share reference", 400)
				return
			}
			seen[entry.ID] = true
		}
		s, err := resolveSMBEntry(entry, old)
		if err != nil {
			http.Error(rw, err.Error(), 400)
			return
		}
		next.Shares = append(next.Shares, s)
	}
	if err = settingsJSONWrite(smbConfigPath, next); err != nil {
		http.Error(rw, "could not save SMB settings", 500)
		return
	}
	writeJSON(rw, map[string]bool{"saved": true})
}
func validateRadioStation(s RadioStation) (RadioStation, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	s.Genre = strings.TrimSpace(s.Genre)
	if s.Name == "" || !isHTTPURL(s.URL) {
		return s, errors.New("each radio station needs a name and an HTTP(S) stream URL")
	}
	return s, nil
}
func (w *webRemote) handleRadioSettings(rw http.ResponseWriter, r *http.Request) {
	sourceSettingsMu.Lock()
	defer sourceSettingsMu.Unlock()
	old := RadioConfig{Stations: []RadioStation{}}
	revision, err := sourceDocument(radioConfigPath, &old)
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(rw, map[string]any{"revision": revision, "stations": old.Stations})
		return
	}
	if !settingsRequest(rw, r) {
		return
	}
	var request struct {
		Revision string          `json:"revision"`
		Stations *[]RadioStation `json:"stations"`
	}
	if err = decodeSettings(r, &request); err != nil || request.Stations == nil {
		http.Error(rw, "invalid radio settings", 400)
		return
	}
	if request.Revision != revision {
		http.Error(rw, "radio settings changed; reload before saving", 409)
		return
	}
	if len(*request.Stations) > 1000 {
		http.Error(rw, "maximum 1000 radio stations", 400)
		return
	}
	next := RadioConfig{Stations: []RadioStation{}}
	for _, station := range *request.Stations {
		s, err := validateRadioStation(station)
		if err != nil {
			http.Error(rw, err.Error(), 400)
			return
		}
		next.Stations = append(next.Stations, s)
	}
	if err = settingsJSONWrite(radioConfigPath, next); err != nil {
		http.Error(rw, "could not save radio settings", 500)
		return
	}
	writeJSON(rw, map[string]bool{"saved": true})
}

var testSMBConnection = func(ctx context.Context, s SMBShare) error {
	s.Name = fmt.Sprintf(".web-test-%d", time.Now().UnixNano())
	root, err := mountShareContext(ctx, s)
	// The mount identity is deterministic even if validation of the subfolder fails.
	identity := sha256.Sum256([]byte(s.Server + "\x00" + s.Share + "\x00" + s.Username + "\x00" + s.Password + fmt.Sprint(s.Guest)))
	mountRoot := filepath.Join(smbMountRoot, fmt.Sprintf("%s-%x", sanitize(s.Name), identity[:6]))
	defer func() {
		end, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = exec.CommandContext(end, "umount", mountRoot).Run()
		if !isMounted(mountRoot) {
			_ = os.Remove(mountRoot)
			smbMountMu.Lock()
			delete(smbManagedMounts, mountRoot)
			smbMountMu.Unlock()
		}
	}()
	if err != nil {
		return errors.New("SMB connection failed; check server, share, folder, credentials and MiSTer CIFS support")
	}
	f, err := os.Open(root)
	if err != nil {
		return errors.New("SMB share mounted but the folder cannot be read")
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if err != nil && err != io.EOF {
		return errors.New("SMB folder cannot be listed")
	}
	return nil
}

func (w *webRemote) handleSourceTest(rw http.ResponseWriter, r *http.Request) {
	if !settingsRequest(rw, r) {
		return
	}
	var request struct {
		Kind     string            `json:"kind"`
		Plex     *PlexConfig       `json:"plex"`
		Jellyfin *JellyfinConfig   `json:"jellyfin"`
		Emby     *JellyfinConfig   `json:"emby"`
		SMB      *smbSettingsEntry `json:"smb"`
		Radio    *RadioStation     `json:"radio"`
		Revision string            `json:"revision"`
	}
	if err := decodeSettings(r, &request); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var err error
	message := "Connection successful."
	switch request.Kind {
	case "plex":
		if request.Plex == nil {
			http.Error(rw, "Plex settings required", 400)
			return
		}
		cfg := *request.Plex
		cfg.Server = strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
		old, _ := loadPlex()
		if cfg.Token == "" && cfg.Server == old.Server {
			cfg.Token = old.Token
		}
		if cfg.Token == "" || !isHTTPURL(cfg.Server) {
			http.Error(rw, "Plex server and token required", 400)
			return
		}
		if cfg.Library != "" && !plexID(cfg.Library) {
			http.Error(rw, "Plex library must be a numeric ID", 400)
			return
		}
		var sections []plexMeta
		sections, err = cfg.listContext(ctx, "/library/sections", nil, 0)
		if err == nil {
			found := false
			for _, section := range sections {
				if section.Type == "artist" && (cfg.Library == "" || section.Key == cfg.Library) {
					found = true
					break
				}
			}
			if !found {
				err = errors.New("server connected, but no matching music library was found")
			}
		}
	case "jellyfin", "emby":
		emby := request.Kind == "emby"
		jf := request.Jellyfin
		if emby {
			jf = request.Emby
		}
		if jf == nil {
			http.Error(rw, request.Kind+" settings required", 400)
			return
		}
		cfg := *jf
		cfg.emby = emby
		cfg.Server = strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
		if !isHTTPURL(cfg.Server) {
			http.Error(rw, cfg.name()+" server required", 400)
			return
		}
		old, _ := loadJFRaw(emby)
		if cfg.Server == strings.TrimRight(old.Server, "/") {
			cfg.Token, cfg.UserID = old.Token, old.UserID
		} else {
			cfg.Token, cfg.UserID = "", ""
		}
		if cfg.Library != "" && !jellyfinID(cfg.Library) {
			http.Error(rw, cfg.name()+" library must be a library id", 400)
			return
		}
		err = testJellyfinConnection(ctx, cfg)
	case "smb":
		if request.SMB == nil {
			http.Error(rw, "SMB settings required", 400)
			return
		}
		sourceSettingsMu.Lock()
		old := SMBConfig{}
		revision, readErr := sourceDocument(smbConfigPath, &old)
		sourceSettingsMu.Unlock()
		if readErr != nil {
			err = readErr
			break
		}
		if request.SMB.ID != "" && request.Revision != revision {
			http.Error(rw, "SMB settings changed; reload before testing", 409)
			return
		}
		var share SMBShare
		share, err = resolveSMBEntry(*request.SMB, old)
		if err == nil {
			err = testSMBConnection(ctx, share)
		}
	case "radio":
		if request.Radio == nil {
			http.Error(rw, "radio station required", 400)
			return
		}
		var station RadioStation
		station, err = validateRadioStation(*request.Radio)
		if err == nil {
			var req *http.Request
			req, err = http.NewRequestWithContext(ctx, "GET", station.URL, nil)
			if err == nil {
				req.Header.Set("Icy-MetaData", "0")
				req.Header.Set("Range", "bytes=0-1023")
				var response *http.Response
				response, err = (&http.Client{Timeout: 10 * time.Second}).Do(req)
				if err == nil {
					defer response.Body.Close()
					if response.StatusCode < 200 || response.StatusCode >= 300 {
						err = fmt.Errorf("station returned HTTP %d", response.StatusCode)
					} else if strings.Contains(response.Header.Get("Content-Type"), "text/html") {
						err = errors.New("this URL returns a web page; enter the direct audio stream URL")
					} else {
						var n int64
						n, err = io.CopyN(io.Discard, response.Body, 64)
						if n > 0 {
							err = nil
						}
						if err != nil {
							err = errors.New("station returned no audio data")
						}
					}
				}
			}
		}
		message = "Station reachable; received stream data."
	default:
		http.Error(rw, "unknown source type", 400)
		return
	}
	if err != nil {
		http.Error(rw, err.Error(), 502)
		return
	}
	writeJSON(rw, map[string]any{"ok": true, "message": message})
}
