package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Media servers in the web remote. A server is a source with its own id
// ("plex", "jellyfin") and its library a tree of virtual folders:
//
//	/                                the menu
//	/artists, /albums, /recent, /playlists
//	/artists/<id>                    an artist's albums
//	/artists/<id>/<id>               the tracks of one of them
//	/albums/<id>, /recent/<id>, /playlists/<id>   the tracks
//	.../track/<n>                    what /api/play receives
//
// Paths contain only category names and server ids, never URLs, and ids are
// checked by the session before they are used.
func srvWebBrowse(ctx context.Context, sess mediaSession, label, folder string) ([]webEntry, []Track, error) {
	folder = path.Clean("/" + folder)
	if folder == "/" {
		return []webEntry{{Name: "Artists", Path: "/artists", IsDir: true}, {Name: "Albums", Path: "/albums", IsDir: true}, {Name: "Recently added", Path: "/recent", IsDir: true}, {Name: "Playlists", Path: "/playlists", IsDir: true}}, nil, nil
	}
	invalid := errors.New("invalid " + label + " path")
	parts := strings.Split(strings.Trim(folder, "/"), "/")
	for _, id := range parts[1:] {
		if !sess.ValidID(id) {
			return nil, nil, invalid
		}
	}
	var items []mediaItem
	var tracks []Track
	var err error
	listTracks := false
	artistLabel := false
	switch parts[0] {
	case "artists":
		switch len(parts) {
		case 1:
			items, err = sess.Artists(ctx)
		case 2:
			items, err = sess.ArtistAlbums(ctx, parts[1])
		case 3:
			tracks, err = sess.AlbumTracks(ctx, parts[2])
			listTracks = true
		default:
			return nil, nil, invalid
		}
	case "albums", "recent":
		switch len(parts) {
		case 1:
			items, err = sess.Albums(ctx, parts[0] == "recent")
			artistLabel = true
		case 2:
			tracks, err = sess.AlbumTracks(ctx, parts[1])
			listTracks = true
		default:
			return nil, nil, invalid
		}
	case "playlists":
		switch len(parts) {
		case 1:
			items, err = sess.Playlists(ctx)
		case 2:
			tracks, err = sess.PlaylistTracks(ctx, parts[1])
			listTracks = true
		default:
			return nil, nil, invalid
		}
	default:
		return nil, nil, invalid
	}
	if err != nil {
		return nil, nil, err
	}
	if listTracks {
		var out []Track
		entries := make([]webEntry, 0, len(tracks))
		for _, t := range tracks {
			if t.SrvPart == "" {
				continue
			}
			name := t.Title
			if parts[0] == "playlists" && t.Artist != "" {
				name = t.Artist + " - " + name
			} else if t.Number > 0 {
				name = fmt.Sprintf("%02d  %s", t.Number, name)
			}
			entries = append(entries, webEntry{Name: name, Path: folder + "/track/" + strconv.Itoa(len(out))})
			out = append(out, t)
		}
		return entries, out, nil
	}
	entries := make([]webEntry, 0, len(items))
	for _, it := range items {
		if !sess.ValidID(it.ID) {
			continue
		}
		name := it.Title
		if artistLabel && it.Artist != "" {
			name = it.Artist + " - " + name
		}
		entries = append(entries, webEntry{Name: name, Path: folder + "/" + it.ID, IsDir: true})
	}
	return entries, nil, nil
}

// srvWebQueue builds the queue for /api/play: the tracks of the album or
// playlist that the chosen ".../track/<n>" belongs to, starting at n.
func srvWebQueue(ctx context.Context, srv mediaServer, selected string) (Queue, error) {
	sess, err := srv.Connect(ctx)
	if err != nil {
		return Queue{}, err
	}
	i := strings.LastIndex(selected, "/track/")
	if i < 0 {
		return Queue{}, errors.New("invalid " + srv.ID() + " track")
	}
	index, err := strconv.Atoi(selected[i+7:])
	if err != nil {
		return Queue{}, errors.New("invalid " + srv.ID() + " track")
	}
	_, tracks, err := srvWebBrowse(ctx, sess, srv.Label(), selected[:i])
	if err != nil {
		return Queue{}, err
	}
	if index < 0 || index >= len(tracks) {
		return Queue{}, errors.New(srv.ID() + " track unavailable")
	}
	return Queue{Tracks: attachSession(sess, srv.ID(), tracks), Index: index}, nil
}

// configuredServerByID is the server with this id, if it is configured.
func configuredServerByID(id string) mediaServer {
	if s := mediaServerByID(id); s != nil && s.Configured() {
		return s
	}
	return nil
}
