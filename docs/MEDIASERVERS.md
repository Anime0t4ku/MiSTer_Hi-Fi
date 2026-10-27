# Media servers

Hi-Fi can browse and stream from music servers. Plex, Jellyfin and Emby are built in;
the code that does everything else (menus, the web remote, the track cache,
playback reports, prefetching, `<id>://` targets for NFC tags) does not depend
on which server it is. A server only implements two small interfaces.

A server **shows up in the sources menu and the web remote only when its config
file exists** (`<id>.json` in `/media/fat/Scripts/.config/MiSTerHiFi/`).

## Using one

Set a server up in the web remote: Settings > Source Settings (a tab per
server, with a connection test), or write the config file by hand.

`plex.json` (see `plex.example.json`): `server`, `token`, optional `library`
(section id; the first music library by default), `insecure` (skip TLS checks,
for a self-signed certificate), `scrobbling` (`plex`: the server counts, for a
scrobbler that follows Plex; `lastfm`, `listenbrainz` or `both`: Hi-Fi's own,
with the Last.fm / ListenBrainz settings of `config.json`; `none`) and `max_track_mb`.

`jellyfin.json` (see `jellyfin.example.json`): only `server` is needed. Sign in
with Quick Connect: in the web settings (the page shows a code), from the
sources menu on the MiSTer (the screen shows a code), or from a shell,
`mister_hifi --jellyfin-login http://host:8096`; then in Jellyfin open your
profile, Quick Connect, and enter the code. Hi-Fi stores the token and user id
it gets (no password). Same optional keys as Plex, with `scrobbling` `server`
(Jellyfin keeps the history), `lastfm`, `listenbrainz`, `both` or `none`, and `library` (the library id).

`emby.json` (see `emby.example.json`): the same keys as Jellyfin's. Emby is the
same code as Jellyfin (`jellyfin.go`: `JellyfinConfig.emby` and
`jellyfinServer.emby` switch the differences: the `/emby` path prefix, the
`X-Emby-*` headers, the item queries under `/Users/<id>/`, numeric item ids, a
`PlaySessionId` on playback reports). Emby has no Quick Connect: sign in with a
user name and password, in the web settings (Emby tab) or from a shell,
`mister_hifi --emby-login http://host:8096 <user>` (asks for the password).
Only the token and user id are stored, never the password. Tested against Emby 4.10.

Command line / NFC targets: `plex://album/<id>`, `plex://playlist/<id>`,
`plex://search/<text>` and the same for `jellyfin://` and `emby://`.

## Gapless

With the *Gapless playback* setting on, a track from a server is queued in the audio engine as soon as the
prefetch has the next one in the cache (`srvPrefetch`, then `prepareGaplessNextFile`), and the engine moves on
without a gap (`handleGaplessTransitionUnlocked` then starts the reporting, cover and prefetch of the new
track). Only FLAC and WAV are queued, as with local files (`gaplessTrackSupported`); shuffle works too, the queued
track is the one of the prefetch's plan; Repeat One does not queue. A track that is not cached yet when the current one
ends is simply fetched and started as before.

## Adding one

1. `yourserver.go`: implement `mediaServer` and `mediaSession` (`mediaserver.go`,
   documented there). `Connect(ctx)` validates the config and returns an immutable
   session (a snapshot: queued tracks keep playing from it if the config changes).
   A session lists artists, albums and playlists, returns `Track`s (set `SrvKey`
   and `SrvPart`; optionally `SrvExt`, `SrvThumb`, `Number`, ...), opens a track's
   file, fetches its cover, reports playback, and says who scrobbles
   (`Scrobbling`), how big a track may be (`MaxTrackBytes`), what an item id looks
   like (`ValidID`: ids go into URL paths) and what keeps cached files apart
   (`CacheKey`). Every network call takes the context, which ends when the track
   stops: use it. Return `errNotSignedIn` from `Connect` and implement
   `mediaServerLogin` if it can sign in from the screen.

   What the generic code gives you for free: the cache with its size limit and
   cancellation, the counted background workers, reports with listen time,
   prefetching the next track (also with shuffle), the "downloading" state, the
   menus, the web remote tree and the `<id>://` targets.
2. Add it to `mediaServers` in `mediaserver.go`.
3. Add `yourserver.example.json`.
4. Test it against a fake server like `mediaserver_test.go` does (`newFakeJellyfin`); the
   behaviour of the generic part is covered by `plex_integration_test.go`.
5. For the web settings, add a form like `jellyfinForm` in `web_settings_ui.go` and a handler like
   `jellyfin_web.go` (config, and a sign-in if the server has one).

`plex.go` is the smaller example, `jellyfin.go` the one with a sign-in flow.
Servers that speak the Subsonic API (Navidrome, Airsonic, ...) fit the same
shape and would be a good next one.

## Testing

`go test .` runs everything against fake servers (no network, no hardware):

    CC=gcc CGO_ENABLED=0 go test .

`emby_live_test.go` runs against a real Emby server (read-only apart from one playback report) when
`HIFI_LIVE_EMBY_SERVER`, `HIFI_LIVE_EMBY_USER` and `HIFI_LIVE_EMBY_PASS` are set; `./build.sh test` passes
`HIFI_LIVE_*` variables on to its container.
