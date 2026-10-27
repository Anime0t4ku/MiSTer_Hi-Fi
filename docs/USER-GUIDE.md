# MiSTer Hi-Fi user guide

Detailed setup and controls for the current script-based MiSTer player. For installation and a feature overview, see the [README](../README.md).

## Configuration files

Configuration is stored in `/media/fat/Scripts/.config/MiSTerHiFi/`. `config.json` holds app settings; `smb.json`, `radio.json`, `plex.json`, `jellyfin.json` and `emby.json` hold source settings. The player creates its main configuration on first launch. On-device settings save automatically. In the Web Remote, save the form after editing. Blank saved-password/token fields preserve the existing credentials where indicated.

App Settings has Playback, Appearance, Controls, System and Experimental tabs in both interfaces. Source Settings in the Web Remote has separate forms for network sources. The on-device player also provides an Equalizer screen.

## Supported Audio Formats

MiSTer Hi-Fi supports the following local audio formats:

```text
MP3
FLAC
WAV / PCM
Ogg Vorbis
M4A (AAC-LC; MP4 container)
M4A (ALAC; MP4 container)
```

It also supports:

```text
M3U playlists
M3U8 playlists
Physical Audio CD / CDDA
Virtual CD / CDDA (CHD and BIN/CUE)
```

M4A files can contain either **AAC-LC** or **Apple Lossless (ALAC)** audio. Embedded MP4 metadata and cover artwork are supported.

When selecting a normal audio file from the built-in browser, MiSTer Hi-Fi treats all supported audio files in the same folder as an album queue.

The browser shows the current position and total number of entries in the active folder, for example `127 / 842`. The synthetic parent-directory entry is not included in the total.

Subfolders are not scanned recursively.

M3U and M3U8 playlists use the tracks and order defined by the playlist.

## Album Artwork

MiSTer Hi-Fi first looks for embedded artwork in supported audio files.

If no embedded artwork is available, it looks for one of these files in the same folder:

```text
cover.jpg
cover.jpeg
cover.png
folder.jpg
folder.jpeg
folder.png
front.jpg
front.jpeg
front.png
```

Artwork filenames are matched case-insensitively, so names such as `COVER.JPG`, `Folder.PNG`, and `Front.Jpeg` also work.

If no artwork is found, the player shows a placeholder. Enable **Auto Hide Missing Art** to use the full-width layout automatically instead.

When **Prioritize External Cover Art** is enabled in Settings, MiSTer Hi-Fi checks for external cover images before scanning supported audio files for embedded artwork. If an external cover is found, the embedded artwork scan is skipped.

External artwork is reused for subsequent tracks from the same folder, avoiding unnecessary reloads and image decoding when an album uses a shared cover file.

Use the `.m4a` filename extension for MP4-container music; the normal file browser does not list `.mp4` files.

## SMB

SMB shares are configured in:

```text
/media/fat/Scripts/.config/MiSTerHiFi/smb.json
```

An example configuration is included as:

```text
/media/fat/Scripts/.config/MiSTerHiFi/smb.example.json
```

Example:

```json
{
  "shares": [
    {
      "name": "Music NAS",
      "server": "192.168.1.100",
      "share": "Music",
      "username": "user",
      "password": "password"
    }
  ]
}
```

Multiple shares can be configured.

SMB playback uses background read-ahead buffering so short network or storage delays do not interrupt playback while still keeping startup fast.

SMB shares are mounted temporarily under:

```text
/tmp/misterhifi-mnt/
```

No network shares are mounted inside the MiSTer Hi-Fi configuration folder.

## Online Radio

Online Radio stations are configured in:

```text
/media/fat/Scripts/.config/MiSTerHiFi/radio.json
```

A `radio.example.json` file is included as a configuration example.

Copy or rename it to:

```text
radio.json
```

Then replace the placeholder station information with direct HTTP or HTTPS audio stream URLs.

Example:

```json
{
  "stations": [
    {
      "name": "Example Radio Station",
      "url": "https://example.com/live.flac",
      "genre": "Example Genre"
    }
  ]
}
```

`name` and `url` are required.

`genre` is optional and is displayed in the station list.

Online Radio is intended for **direct audio stream URLs**, not station web pages or playlist landing pages.

### Supported Radio Formats

Online Radio supports:

```text
MP3
FLAC
Ogg FLAC
Ogg Vorbis
WAV / PCM
```

Raw AAC, AAC+ / HE-AAC, Opus, and Ogg Opus streams are not currently supported.

AAC and ALAC remain supported when contained in normal M4A/MP4 music files. Support for M4A files does not imply support for raw AAC internet radio streams.

If an Online Radio stream ends unexpectedly, MiSTer Hi-Fi automatically reconnects to the same station. This helps stations that close or restart their stream connection between songs and also improves recovery from short interruptions. Explicitly stopping playback or selecting another station cancels the reconnect.

## Media servers

Plex, Jellyfin and Emby share the same browsing, caching and playback path. Configure them through **Web Remote > Settings > Source Settings**. A server appears in the source list after its configuration file exists.

- **Plex:** enter the local server URL and X-Plex-Token. An empty library ID uses the first music library.
- **Jellyfin:** enter the server URL and sign in with Quick Connect. Approve the code in your Jellyfin account. Quick Connect is also available from the MiSTer screen or `mister_hifi --jellyfin-login http://host:8096`.
- **Emby:** enter the server URL and sign in with a username and password. Only the issued token and user ID are stored. The terminal alternative is `mister_hifi --emby-login http://host:8096 <user>`.

Browse artists, albums, recently added albums and playlists. Plex's recently added view is limited to 100 albums. Original files are downloaded to a temporary cache, without server transcoding. Playback starts after the selected file is ready, and the next track is prefetched. Stop or Next cancels pending preparation. Cache limits apply per track.

With gapless enabled, cached FLAC/WAV server tracks can transition without a gap. The shuffle choice is shared with prefetch so the downloaded track matches the next selection. If the next file is not ready, it starts after downloading normally. M4A playback supports AAC-LC and ALAC through the normal decoder.

Scrobbling modes are server-managed (`plex` for Plex, `server` for Jellyfin/Emby), `lastfm`, `listenbrainz`, `both` or `none`. **Both** means Hi-Fi submits to Last.fm and ListenBrainz; it does not mean server plus Hi-Fi.

See [media-server configuration](MEDIASERVERS.md) for JSON fields and external target formats.

## Last.fm Scrobbling

MiSTer Hi-Fi can scrobble what you're playing to [Last.fm](https://www.last.fm) - both local files and Online Radio stations. For radio, it reads the ICY metadata (`StreamTitle`) that Shoutcast/Icecast servers send between songs, so scrobbles reflect the actual song currently on air rather than just the station name. Stations that don't send metadata simply won't scrobble a specific song.

### Setup

1. Create a Last.fm API application at <https://www.last.fm/api/account/create> (any name/description works) to get an **API key** and **shared secret**.
2. On the MiSTer, run the one-time authorization flow from a terminal:

   ```sh
   /media/fat/Scripts/.config/MiSTerHiFi/mister_hifi --lastfm-auth
   ```

3. Enter your API key and secret when prompted (only needed the first time).
4. Open the URL it prints in any browser - your phone works fine, it doesn't need to be on the MiSTer itself - and click **Allow**.
5. Back in the terminal, press Enter. On success, a permanent session key is saved to `config.json` and scrobbling is enabled.

Your Last.fm password is never sent to or stored by MiSTer Hi-Fi - only the session key Last.fm issues after you approve access, which you can revoke at any time from your [Last.fm application settings](https://www.last.fm/settings/applications).

### What gets scrobbled

A track is scrobbled once it has played for at least half its length (or 4 minutes, whichever is shorter), matching [Last.fm's scrobbling rules](https://www.last.fm/api/scrobbling). Since Online Radio has no known track length ahead of time, radio songs are scrobbled once they've played for at least 30 seconds and either the station's `StreamTitle` changes to a new song or playback stops.

### Configuration

The `lastfm` section of `config.json` holds these settings:

```json
"lastfm": {
  "enabled": true,
  "api_key": "your-api-key",
  "api_secret": "your-shared-secret",
  "session_key": "issued-after-auth",
  "username": "your-lastfm-username"
}
```

Set `"enabled": false` to pause scrobbling without losing your saved session key.

## ListenBrainz Scrobbling

[ListenBrainz](https://listenbrainz.org) works next to Last.fm (or instead of it) and needs no application or authorization flow, only your **user token**: copy it from <https://listenbrainz.org/profile/>. Then either fill in the *ListenBrainz* group of the web settings, or put it in `config.json`:

```json
"listenbrainz": {
  "enabled": true,
  "token": "your-user-token",
  "url": ""
}
```

`url` is empty for listenbrainz.org; a server with the same API (a self-hosted ListenBrainz, Maloja, Koito) goes there, without `/1/submit-listens`. The same rules as for Last.fm apply: "playing now" when a track starts, a listen once it has played half its length (or 4 minutes, 30 seconds for radio). The scrobbling mode of a media server (Plex, Jellyfin, Emby) can be `lastfm`, `listenbrainz` or `both` to let Hi-Fi do it instead of the server.

## Physical and virtual CDs

**Virtual CD** lets you use CD-based game disc images already stored on the MiSTer as playable audio discs. It supports **CHD** and **BIN/CUE** images and exposes the disc's CDDA tracks while ignoring data tracks.

When no image is mounted, open **Virtual CD > Select Disc** and choose a `.chd` or `.cue` file from the MiSTer SD card. Once mounted, Virtual CD provides:

```text
Play Disc
Browse Disc
Unmount Disc
```

**Play Disc** starts from the first audio track. **Browse Disc** opens the audio track list for individual track selection. **Unmount Disc** closes the current image and returns Virtual CD to the Select Disc state.

CHD images are read directly through **libchdr** and are not extracted to temporary BIN/CUE files. BIN/CUE images are also read directly from their referenced BIN data.

Physical Audio CDs open directly in the player and automatically start from the first audio track. Press **B** from the player to open the disc track list.

The Physical Disc source is hidden when no optical drive is detected. For recognised discs, MusicBrainz can supply album, artist and track names, with cover artwork from Cover Art Archive or a fallback lookup. Results are cached. Offline or unrecognised discs retain generic track names.

## Launching and NFC

Open MiSTer Hi-Fi normally:

```bash
/media/fat/Scripts/misterhifi.sh
```

Play a single file directly:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/song.flac"
```

Play a folder as an album:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/Artist/Album"
```

The first supported audio file in the folder starts automatically and the remaining supported tracks become the album queue.

Play a playlist directly:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/Playlists/Favorites.m3u"
```

External single-file launches only play the requested file.

### NFC / Zaparoo

MiSTer Hi-Fi can be launched directly through Zaparoo using NFC tags.

Write a MiSTer script command to the NFC tag.

#### Single file

```text
**mister.script:misterhifi.sh "/media/fat/Music/song.flac"
```

#### Album folder

```text
**mister.script:misterhifi.sh "/media/fat/Music/Artist/Album"
```

#### Playlist

```text
**mister.script:misterhifi.sh "/media/fat/Music/Playlists/Favorites.m3u"
```

#### SMB

For SMB content, use the configured share name instead of the temporary mount path:

```text
**mister.script:misterhifi.sh "smb://Music NAS/Artist/Album"
```

When MiSTer Hi-Fi is already running, later Zaparoo scans are sent directly to the active player.

The application does not restart. The current queue is replaced and the new track, album, or playlist starts immediately.

Media-server targets use `plex://album/<id>`, `plex://playlist/<id>` or `plex://search/<text>`; replace `plex` with `jellyfin` or `emby` for those servers. GBA targets use `gba://<ROM path>#<song number>` and require GBA Music to be enabled.

**NFC limitations:** Zaparoo HOLD mode is not compatible. Further scans may not be processed while Hi-Fi is active; command forwarding only takes effect when a new launch command reaches the running player.

## Controls

### Controller

MiSTer Hi-Fi uses the system-wide MiSTer controller mapping when one is available, so controllers follow the layout configured through MiSTer rather than relying on their raw Linux button numbers. If no MiSTer map exists for a controller, the built-in direct input mapping remains available as a fallback.

```text
D-Pad    Navigate
A        Select
B        Back
Home     Sources

L1       Previous Track
X        Play / Pause
Y        Stop / Unload / Return to Track List
R1       Next Track
Start    Now Playing
```

### List navigation

Settings includes list jump size, page jump size, wrap list navigation, hold to repeat, hold delay, and repeat interval. Left/right on the D-pad or keyboard jump 5 entries by default; keyboard Page Up/Down jump 10. Jump sizes can be selected from 1, 5, 10, 25, 50, 100, 250, 500, or 1000 entries.

Wrapping is enabled by default: a jump past the top goes to the last entry, and a jump past the bottom goes to the first. Blank separators are skipped. The Now Playing bar remains accessible with Down from the last list entry.

Holding a direction in a browser list repeats after 400 ms, then every 150 ms by default. Releasing the direction stops repetition. Hold repetition does not apply to playback seeking or settings changes. Settings scroll automatically to keep the selected option visible.

These values are saved in the existing `config.json` under `navigation`:

```json
"navigation": {
  "jump_entries": 5,
  "page_jump_entries": 10,
  "wrap": true,
  "hold_repeat": true,
  "hold_delay_ms": 400,
  "repeat_interval_ms": 150
}
```

### Keyboard

```text
Arrow Keys         Navigate / Adjust
Enter              Select / Confirm
Esc / Backspace    Back
Page Up / Down     Move through long lists
Home / End         First / Last entry
Tab                Sources
O                  Now Playing

Space / P          Play / Pause
S                  Stop / Unload
N                  Next Track
B                  Previous Track
H                  Toggle Shuffle
R                  Toggle Repeat
```

### Media Keys

Dedicated keyboard media controls are also supported where available:

```text
Play / Pause       Play / Pause
Play               Play / Resume
Pause              Pause
Stop               Stop / Unload
Next Track         Next Track
Previous Track     Previous Track
```

MiSTer Hi-Fi now provides its own internal 0-100% playback volume control. The player volume buttons move to the next or previous 5% step, while mute preserves the selected volume level.

When the progress bar is selected:

```text
Left     Seek Back 10 Seconds
Right    Seek Forward 10 Seconds
```

## Player

The player includes:

```text
Previous Track
Play / Pause
Stop
Next Track
Shuffle
Repeat All / Repeat One
Volume Down / Volume Up / Mute
Equalizer
Progress Seeking
Spectrum Visualizer
```

Repeat cycles through Off, Repeat All, and Repeat One. Repeat All wraps the queue back to the first track, while Repeat One replays the current track.

Playback continues while browsing other sources and folders.

A **Now Playing**, **Paused**, or **Loaded** bar remains available while browsing and can be selected to return to the player.

## Audio Information

Now Playing displays information about the active audio source next to the track number.

Example:

```text
Track: 1 of 12    FLAC    16 bit    44.1 kHz    770 kbps
```

Depending on the source, the information can include:

```text
Audio Format
Codec
Bit Depth
Sample Rate
Bitrate
```

M4A files identify their contained codec as `AAC` or `ALAC`. ALAC also reports bit depth when available.

Physical and Virtual CD audio tracks are shown as:

```text
CDDA    16 bit    44.1 kHz    1411 kbps
```

## Equalizer

MiSTer Hi-Fi includes a 5-band equalizer.

The bands are centered around:

```text
60 Hz
250 Hz
1 kHz
4 kHz
12 kHz
```

The equalizer is applied directly to the active playback path.

**Inside Settings**, L/R switches categories instead of skipping tracks; keyboard Page Up/Down also switches tabs. Up/Down selects an option, Left/Right adjusts it, and Confirm opens editors such as the RGB picker. Each tab remembers its selected row while Settings is open.

## Settings

MiSTer Hi-Fi includes settings for playback, display, controls, and fonts.

### OLED Mode

Uses true-black UI backgrounds, including selected rows, Now Playing, missing-art placeholders and inactive visualizer segments. Audio-information badges use outlines. Sunset/Grid keep their foreground artwork on black; normal mode retains its existing colours.

### Show Album Art

Enables the album-art player layout.

When disabled, the player uses the full available width for track information, progress seeking, the visualizer, and playback controls.

### Auto Hide Missing Art

Automatically switches to the full-width no-art layout when the current track has no artwork.

This option is unavailable while **Show Album Art** is disabled.

### Prioritize External Cover Art

Checks for external cover images before embedded artwork.

When an external cover is found, embedded artwork is not scanned. This can improve loading speed for formats such as FLAC and avoids unnecessary artwork work when an album already provides a shared external cover image.

External artwork is cached for tracks played from the same folder so the same image does not need to be reloaded for every track.

### Show Clock

Displays the MiSTer's 24-hour system clock in the top-right corner.

### Confirm on Exit

Asks for confirmation before closing MiSTer Hi-Fi.

This is enabled by default.

### Screensaver

Turns the display completely black after a selected period of inactivity.

Available intervals:

```text
30 Seconds
1 Minute
2 Minutes
5 Minutes
10 Minutes
Off
```

Any controller or keyboard input wakes the display. The wake input itself is consumed and is not passed to the application.

### Remember Shuffle / Loop

Remembers the current Shuffle and Loop states between playback queues and application restarts.

When enabled, changes to Shuffle or Loop are saved automatically and restored when a new queue is created. When disabled, MiSTer Hi-Fi uses the normal default states.

### Gapless Playback

Enables seamless natural track-to-track transitions where supported.

Gapless playback is available for:

```text
FLAC
WAV
Physical Audio CD / CDDA
Cached Plex / Jellyfin / Emby FLAC and WAV
```

MP3 is intentionally excluded.

Gapless playback is experimental and disabled by default.

### Swap A/B

Swaps the logical MiSTer A and B behavior after the system-wide controller mapping is applied, without changing the on-screen labels.

### Swap X/Y

Swaps the logical MiSTer X and Y behavior after the system-wide controller mapping is applied, without changing the on-screen labels.

These remain optional preference overrides. Controller compatibility itself comes from MiSTer's mapping; the swaps only change the final Hi-Fi button behavior.

### Custom Fallback Font

Allows a user-provided TrueType or OpenType font to supply characters that are unavailable in MiSTer Hi-Fi's built-in bitmap font.

The built-in font always remains the primary UI font.

Settings are saved automatically in:

```text
/media/fat/Scripts/.config/MiSTerHiFi/config.json
```

## Custom Fallback Fonts

MiSTer Hi-Fi automatically creates:

```text
/media/fat/Scripts/.config/MiSTerHiFi/fonts/
```

Place user-provided `.ttf` or compatible `.otf` files in this folder.

Valid fonts automatically appear under:

```text
Settings > Appearance > Custom Fallback Font
```

The font filename is used as its display name.

Custom Fallback Font defaults to **Off**.

If the folder contains no valid fonts, the setting is disabled.

If a previously selected font is removed, MiSTer Hi-Fi safely falls back to **Off**.

The custom font does not replace MiSTer Hi-Fi's built-in bitmap font. It is only used for characters that the built-in font cannot display, such as Japanese or accented characters.

Fallback glyphs follow MiSTer Hi-Fi's existing text scaling, layout, clipping, and alignment.

MiSTer Hi-Fi does not include or distribute custom font files. Users are responsible for ensuring they have permission to use any fonts they add.

### Visualizers, accent colour and web address

Choose Spectrum Bars, Retro LED, KITT Voicebox, KITT Voicebox (Original), Retro Sunset, Retro Grid or Colour Wave. The accent colour affects selection outlines, progress and plain spectrum bars. Other visualizers also use their own colours. Choose a preset or open the RGB editor for a custom colour.

**Show Web Address** controls the address displayed on the MiSTer screen; it does not enable or disable the Web Remote. The default port is 8182. Changing the Web Remote enabled state or port requires restarting Hi-Fi.

### Experimental options

- **Gapless playback:** off by default; see the supported formats above. Cached server FLAC/WAV tracks are included.
- **FPS Target (Experimental):** 30 FPS by default, or an optional 60 FPS target. This changes the visualizer refresh target, not the display's HDMI mode. Actual frame rate depends on resolution and visualizer.
- **Enable GBA Music:** off by default. When disabled, the source is hidden, requests to browse/play GBA music are blocked and active GBA playback stops.

The Web Remote has the same five settings categories. Edits stay in the form when switching tabs; Save app settings applies edits across tabs. Focused category tabs support Left/Right, Home and End keys.

## GBA music

Only MP2K-compatible GBA ROMs are supported; Game Boy and Game Boy Color playback are not implemented. `.gba` files and ZIPs containing a `.gba` file can be opened. ZIP playback uses the first GBA ROM in the archive.

The source checks `games/GBA` on SD Card (`/media/fat`), CIFS (`/media/fat/cifs`) and USB drives (`/media/usb0` through `/media/usb7`). Available choices appear as CIFS, SD CARD and USB. One source opens directly. Multiple USB drives get a drive selector. Browsing preserves folders and filenames; other file types are visible but not playable. Dismissing an unsupported-game message returns to its folder with the same game highlighted.

The engine tries to list music rather than sound effects: its preferred selection is looping songs with at least three tracks, with broader fallbacks for games that do not fit that pattern. This can omit short jingles or other valid music. No compatibility percentage is claimed for the entire GBA library. The [confirmed-game record](GBA-SUPPORTED.md) contains only named, verified playback results.

### GBA preparation and duration

Each song is synthesised before playback, retaining the two-loop/fade behaviour. This adds a preparation wait before each song but avoids synthesis competing with realtime playback and the visualizer. The loading screen shows how much audio has been prepared and how long it has waited; animated visualizers pause during preparation.

Once preparation finishes, the measured duration is shown. Playback, seeking and the visualizer use the standard file-audio path. Stop or choosing another song cancels preparation. Temporary audio is removed once the engine owns the open file, or when preparation fails. Preparation is limited to 20 minutes of generated audio and three minutes of processing time. Errors are displayed and diagnostic messages are written to standard error.

### GBA artwork and song names

Artwork is local and is not downloaded automatically or extracted from ROMs. Search order:

1. `gamelist.xml` or `hifi-gba.json` beside the game or in parent directories up to the GBA root. XML supports `<image>`, `<boxart2d>` and parent game entries. Paths can be relative to the metadata file or absolute.
2. A PNG/JPG/JPEG matching the complete ROM filename without its extension. Optional suffixes are `-image`, `-boxart` and `-cover`. The ROM directory and its `images`, `media`, `boxart`, `covers` and `.boxart` subfolders are checked.
3. Standard folder covers such as `cover.png`, `folder.jpg` or `front.jpeg`.

Keep region tags when matching an image to a ROM, for example `Game (USA).gba` and `Game (USA).png`. A shared folder cover applies to games in that folder when no game-specific cover is found. Artwork appears in the player for the game's songs.

Song names can be supplied through `hifi-gba-songs.json` beside the ROM. Entries are keyed by the ROM header's game code and engine song ID, not the displayed list position:

```json
{
  "BPEE": {
    "413": "Title Screen"
  }
}
```

Without a matching entry, songs use generic labels such as `SONG 1`. No scan, RomM-import or song-name-generation helper scripts are included in this baseline.

## Building

MiSTer Hi-Fi targets Linux ARMv7. The supplied `Containerfile` describes a compatible build environment. A native build environment needs:

- Go 1.23 or later, as required by `go.mod`.
- ARMv7 hard-float C and C++ cross-compilers; GBA synthesis uses C++20.
- Rust/Cargo with the `armv7-unknown-linux-gnueabihf` target for AAC/ALAC decoding.
- The fetch/build utilities used by the scripts, including curl, tar, CMake, Perl, make and standard compiler tools.

```sh
chmod +x build-mister.sh fetch_miniaudio.sh fetch_libchdr.sh build_agbplay.sh fetch_agbplay.sh
rustup target add armv7-unknown-linux-gnueabihf
./build-mister.sh
```

The script recognises the toolchain at `/opt/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin`. Otherwise the configured compiler commands must be on PATH. Its defaults are `arm-none-linux-gnueabihf-gcc` and `arm-none-linux-gnueabihf-g++`.

The resulting executable is `Scripts/.config/MiSTerHiFi/mister_hifi`. The build fetches missing miniaudio/stb headers, libchdr and the pinned agbplay/fmt/JSON sources, builds native dependencies, builds the Rust M4A decoder, then links the Go application. It uses Linux framebuffer, audio and input devices; a separate FPGA hybrid core is not required.

The GBA build treats downloaded dependency headers as system headers and silences historical ARM ABI notices. Warnings in application and engine source remain enabled.

## Dependencies and licenses

MiSTer Hi-Fi uses several third-party components.

### miniaudio

[miniaudio](https://github.com/mackron/miniaudio) by David Reid is used for audio decoding and playback.

miniaudio is available under the author's choice of **Public Domain** or the **MIT No Attribution (MIT-0) License**.

### libchdr

[libchdr](https://github.com/rtissera/libchdr) is used by **Virtual CD** to read MAME CHD CD images, their track metadata, and compressed CD sectors directly.

libchdr is distributed under the **BSD 3-Clause License**. libchdr also includes third-party compression/decoding components which remain under their respective upstream licenses. The build uses the upstream libchdr source and bundled dependencies without relicensing them under MiSTer Hi-Fi's GPLv3 license.

The libchdr source and its accompanying license files are downloaded from the upstream project by `fetch_libchdr.sh` during the build.

### Symphonia

Symphonia is used for M4A/MP4 AAC and ALAC decoding.

It is built with the required ISO/MP4, AAC, and ALAC components.

Symphonia is distributed under the **Mozilla Public License 2.0 (MPL-2.0)**.

Its license text is included at:

```text
../licenses/Symphonia-MPL-2.0.txt
```

### MP4 Metadata

MP4 metadata parsing uses:

```text
github.com/dhowden/tag
```

This component is distributed under the **BSD 2-Clause License**.

### stb_truetype

[`stb_truetype`](https://github.com/nothings/stb) is used to parse and rasterize user-provided TrueType/OpenType fallback fonts.

Its license is included in the repository at:

```text
../licenses/stb-LICENSE.txt
```

### GBA playback dependencies

[agbplay](https://github.com/ipatix/agbplay) supplies the MP2K music engine under LGPLv3. The build uses its playback core and a wrapper rather than its desktop interface. Its fetched license is `third_party/agbplay-src/LICENSE-agbplay.txt`.

The engine also uses [{fmt}](https://github.com/fmtlib/fmt) and [nlohmann/json](https://github.com/nlohmann/json), which retain their upstream MIT licenses and notices. The build fetches the libraries with the engine.

MiSTer Hi-Fi's GPLv3 license applies to its own code and does not replace dependency licenses. Music, ROMs, artwork, radio content and custom fonts are not distributed with the project.
