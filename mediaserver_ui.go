package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Media servers: the menus on the MiSTer's screen and the <id>://... targets.

func srvError(app *App, srv mediaServer, err error) {
	message(app, srv.Label()+" ERROR", strings.ToUpper(err.Error()))
}

// mediaServerUI is the menu of one server: artists, albums, recent albums and
// playlists.
func mediaServerUI(app *App, srv mediaServer) {
	ctx := context.Background()
	sess, err := srv.Connect(ctx)
	if errors.Is(err, errNotSignedIn) {
		if l, ok := srv.(mediaServerLogin); ok {
			if lerr := l.Login(app); lerr != nil {
				message(app, srv.Label(), strings.ToUpper(lerr.Error()))
				return
			}
			sess, err = srv.Connect(ctx)
		}
	}
	if err != nil {
		switch {
		case os.IsNotExist(err):
			message(app, srv.Label(), strings.ToUpper(srv.ID())+" IS NOT CONFIGURED")
		case errors.Is(err, errNotSignedIn):
			message(app, srv.Label(), "NOT SIGNED IN")
		default:
			srvError(app, srv, err)
		}
		return
	}
	initial := 0
	for {
		i, ok := menu(app, srv.Label(), []string{"ARTISTS", "ALBUMS", "RECENT ALBUMS", "PLAYLISTS"}, initial)
		if !ok {
			return
		}
		initial = i
		switch i {
		case 0:
			srvArtistsUI(app, srv, sess)
		case 1:
			albums, err := sess.Albums(ctx, false)
			if err != nil {
				srvError(app, srv, err)
				break
			}
			srvAlbumsUI(app, srv, sess, "ALBUMS", albums, true)
		case 2:
			albums, err := sess.Albums(ctx, true)
			if err != nil {
				srvError(app, srv, err)
				break
			}
			srvAlbumsUI(app, srv, sess, "RECENT ALBUMS", albums, true)
		case 3:
			srvPlaylistsUI(app, srv, sess)
		}
		if app.jumpSources {
			return
		}
	}
}

func srvArtistsUI(app *App, srv mediaServer, sess mediaSession) {
	ctx := context.Background()
	artists, err := sess.Artists(ctx)
	if err != nil {
		srvError(app, srv, err)
		return
	}
	if len(artists) == 0 {
		message(app, srv.Label(), "NO ARTISTS")
		return
	}
	names := make([]string, len(artists))
	for i, a := range artists {
		names[i] = a.Title
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, srv.Label()+" - ARTISTS", names, initial, true)
		if !ok {
			return
		}
		initial = i
		albums, err := sess.ArtistAlbums(ctx, artists[i].ID)
		if err != nil {
			srvError(app, srv, err)
			continue
		}
		srvAlbumsUI(app, srv, sess, artists[i].Title, albums, false)
		if app.jumpSources {
			return
		}
	}
}

func srvAlbumsUI(app *App, srv mediaServer, sess mediaSession, title string, albums []mediaItem, withArtist bool) {
	if len(albums) == 0 {
		message(app, srv.Label(), "NO ALBUMS")
		return
	}
	names := make([]string, len(albums))
	for i, a := range albums {
		names[i] = a.Title
		if withArtist && a.Artist != "" {
			names[i] = a.Artist + " - " + a.Title
		}
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, srv.Label()+" - "+title, names, initial, true)
		if !ok {
			return
		}
		initial = i
		tracks, err := sess.AlbumTracks(context.Background(), albums[i].ID)
		if err != nil {
			srvError(app, srv, err)
			continue
		}
		srvTracksUI(app, srv, sess, albums[i].Title, attachSession(sess, srv.ID(), tracks), false)
		if app.jumpSources {
			return
		}
	}
}

func srvPlaylistsUI(app *App, srv mediaServer, sess mediaSession) {
	ctx := context.Background()
	lists, err := sess.Playlists(ctx)
	if err != nil {
		srvError(app, srv, err)
		return
	}
	if len(lists) == 0 {
		message(app, srv.Label(), "NO PLAYLISTS")
		return
	}
	names := make([]string, len(lists))
	for i, l := range lists {
		names[i] = l.Title
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, srv.Label()+" - PLAYLISTS", names, initial, true)
		if !ok {
			return
		}
		initial = i
		tracks, err := sess.PlaylistTracks(ctx, lists[i].ID)
		if err != nil {
			srvError(app, srv, err)
			continue
		}
		srvTracksUI(app, srv, sess, lists[i].Title, attachSession(sess, srv.ID(), tracks), true)
		if app.jumpSources {
			return
		}
	}
}

func srvTracksUI(app *App, srv mediaServer, sess mediaSession, title string, all []Track, withArtist bool) {
	var tracks []Track
	var names []string
	for _, t := range all {
		if t.SrvPart == "" {
			continue
		}
		tracks = append(tracks, t)
		name := t.Title
		if withArtist && t.Artist != "" {
			name = t.Artist + " - " + t.Title
		} else if t.Number > 0 {
			name = fmt.Sprintf("%02d  %s", t.Number, t.Title)
		}
		names = append(names, name)
	}
	if len(tracks) == 0 {
		message(app, srv.Label(), "NO TRACKS")
		return
	}
	initial := 0
	for {
		i, ok := menuWithEntryCounter(app, srv.Label()+" - "+title, names, initial, true)
		if !ok {
			return
		}
		initial = i
		q := Queue{Tracks: append([]Track{}, tracks...), Index: i}
		origin := &browseOrigin{Kind: srv.ID(), Selected: names[i]}
		if err := app.startQueue(q, origin); err != nil {
			srvError(app, srv, err)
			continue
		}
		playerUI(app)
		if app.jumpSources {
			return
		}
		if t := app.player; t != nil {
			t.mu.Lock()
			if t.q.Index >= 0 && t.q.Index < len(names) && len(t.q.Tracks) == len(tracks) {
				// keep the cursor on the track that was playing when we left
				initial = t.q.Index
			}
			t.mu.Unlock()
		}
	}
}

// ---- command line / NFC targets ----

// srvExternalQueue resolves a target of the form
//
//	<id>://album/<key>       all tracks of an album
//	<id>://playlist/<key>    all items of a playlist
//	<id>://search/<text>     the album best matching text ("artist album")
//
// for any known server. handled is false when the target is not one of
// theirs.
func srvExternalQueue(raw string) (q Queue, handled bool, err error) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return Queue{}, false, nil
	}
	srv := mediaServerByID(strings.ToLower(scheme))
	if srv == nil {
		return Queue{}, false, nil
	}
	q, err = srvQueueFor(srv, rest)
	return q, true, err
}

func srvQueueFor(srv mediaServer, rest string) (Queue, error) {
	ctx := context.Background()
	sess, err := srv.Connect(ctx)
	if err != nil {
		return Queue{}, err
	}
	kind, arg, _ := strings.Cut(rest, "/")
	arg = strings.TrimSpace(arg)
	if u, e := url.PathUnescape(arg); e == nil {
		arg = u
	}
	if arg == "" {
		return Queue{}, errors.New("empty " + srv.ID() + " target")
	}
	var tracks []Track
	switch strings.ToLower(kind) {
	case "album":
		if !sess.ValidID(arg) {
			return Queue{}, errors.New("invalid " + srv.ID() + " id")
		}
		tracks, err = sess.AlbumTracks(ctx, arg)
	case "playlist":
		if !sess.ValidID(arg) {
			return Queue{}, errors.New("invalid " + srv.ID() + " id")
		}
		tracks, err = sess.PlaylistTracks(ctx, arg)
	case "search":
		var album mediaItem
		if album, err = sess.FindAlbum(ctx, arg); err == nil {
			tracks, err = sess.AlbumTracks(ctx, album.ID)
		}
	default:
		return Queue{}, errors.New("unknown " + srv.ID() + " target")
	}
	if err != nil {
		return Queue{}, err
	}
	var q Queue
	for _, t := range attachSession(sess, srv.ID(), tracks) {
		if t.SrvPart != "" {
			q.Tracks = append(q.Tracks, t)
		}
	}
	if len(q.Tracks) == 0 {
		return Queue{}, errors.New("no tracks")
	}
	return q, nil
}
