# MiSTer Hi-Fi

MiSTer Hi-Fi is a controller-first music player for MiSTer FPGA.

It supports local music, USB storage, SMB shares, online radio, playlists, Physical CDs, Virtual CDs from CHD and BIN/CUE disc images, album artwork, a spectrum visualizer, equalizer, OLED mode, custom fallback fonts, and direct launching through NFC using Zaparoo.

## Installation

Download the latest release and extract it to the **root of your MiSTer SD card**.

The files are installed under:

```text
/media/fat/Scripts/
├── misterhifi.sh
└── .config/
    └── MiSTerHiFi/
        ├── mister_hifi
        ├── smb.example.json
        └── radio.example.json
```

MiSTer Hi-Fi creates its configuration automatically on first launch.

The main configuration file is stored at:

```text
/media/fat/Scripts/.config/MiSTerHiFi/config.json
```

## Sources

MiSTer Hi-Fi can browse and play music from:

```text
SD Card
USB
SMB
Online Radio
Virtual CD
Physical Audio CD
```

Music can be browsed directly from the source list.

### Virtual CD

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

Optical-drive read speed can be limited from **Settings > CD Drive Speed**. This can reduce noise from high-speed CD/DVD drives during Audio CD playback.

## Supported Audio Formats

MiSTer Hi-Fi supports the following local audio formats:

```text
MP3
FLAC
WAV / PCM
Ogg Vorbis
M4A / MP4 (AAC-LC)
M4A / MP4 (ALAC)
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

If no artwork is found, MiSTer Hi-Fi automatically uses the normal no-art player layout.

When **Prioritize External Cover Art** is enabled in Settings, MiSTer Hi-Fi checks for external cover images before scanning supported audio files for embedded artwork. If an external cover is found, the embedded artwork scan is skipped.

External artwork is reused for subsequent tracks from the same folder, avoiding unnecessary reloads and image decoding when an album uses a shared cover file.

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

## Launching

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

## NFC / Zaparoo

MiSTer Hi-Fi can be launched directly through Zaparoo using NFC tags.

Write a MiSTer script command to the NFC tag.

### Single File

```text
**mister.script:misterhifi.sh "/media/fat/Music/song.flac"
```

### Album Folder

```text
**mister.script:misterhifi.sh "/media/fat/Music/Artist/Album"
```

### Playlist

```text
**mister.script:misterhifi.sh "/media/fat/Music/Playlists/Favorites.m3u"
```

### SMB

For SMB content, use the configured share name instead of the temporary mount path:

```text
**mister.script:misterhifi.sh "smb://Music NAS/Artist/Album"
```

When MiSTer Hi-Fi is already running, later Zaparoo scans are sent directly to the active player.

The application does not restart. The current queue is replaced and the new track, album, or playlist starts immediately.

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

Volume and mute media keys are not handled by MiSTer Hi-Fi because the application does not provide internal volume control.

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
Repeat
Equalizer
Progress Seeking
Spectrum Visualizer
```

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

## Settings

MiSTer Hi-Fi includes settings for playback, display, controls, and fonts.

### OLED Mode

Uses a true-black background.

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

### CD Drive Speed

Controls the optical-drive read speed used for Physical Audio CD playback. This can reduce drive noise when using high-speed CD/DVD drives.

Available options:

```text
Default
Auto
1x
2x
4x
8x
16x
```

**Default** leaves the drive untouched. **Auto** asks the Linux CD-ROM driver to select the speed automatically. Fixed values request the corresponding CD read speed directly from the drive.

Drive speed control is handled natively by MiSTer Hi-Fi and does not require `setcd` or any additional package. Support for individual requested speeds depends on the optical drive.

### Gapless Playback

Enables seamless natural track-to-track transitions where supported.

Gapless playback is available for:

```text
FLAC
WAV
Physical Audio CD / CDDA
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
Settings > Custom Fallback Font
```

The font filename is used as its display name.

Custom Fallback Font defaults to **Off**.

If the folder contains no valid fonts, the setting is disabled.

If a previously selected font is removed, MiSTer Hi-Fi safely falls back to **Off**.

The custom font does not replace MiSTer Hi-Fi's built-in bitmap font. It is only used for characters that the built-in font cannot display, such as Japanese or accented characters.

Fallback glyphs follow MiSTer Hi-Fi's existing text scaling, layout, clipping, and alignment.

MiSTer Hi-Fi does not include or distribute custom font files. Users are responsible for ensuring they have permission to use any fonts they add.

## Building From Source

MiSTer Hi-Fi is written primarily in Go, with additional native components for audio decoding and playback, and targets Linux ARMv7.

The main audio path uses **miniaudio**.

Virtual CD CHD playback uses **libchdr** to read CHD CD metadata and sectors directly without extracting the image.

M4A/MP4 playback uses a separate **Symphonia** decoder path for AAC-LC and ALAC.

Build:

```bash
chmod +x fetch_miniaudio.sh fetch_libchdr.sh build-mister.sh
./build-mister.sh
```

The resulting MiSTer binary is placed at:

```text
Scripts/.config/MiSTerHiFi/mister_hifi
```

The repository does not store `miniaudio.h` or the libchdr source tree. The build process downloads the required upstream sources automatically. libchdr is pinned to the version used by `fetch_libchdr.sh` and is cross-compiled into the MiSTer ARMv7 build.

Building M4A support also requires Rust/Cargo and the following Rust target:

```text
armv7-unknown-linux-gnueabihf
```

The build script adds this target automatically when `rustup` is available.

## Third-Party Software

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
licenses/Symphonia-MPL-2.0.txt
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
licenses/stb-LICENSE.txt
```

## Known Issues

- Zaparoo's **HOLD** mode is currently not compatible with MiSTer Hi-Fi.
- While MiSTer Hi-Fi is active, Zaparoo cannot process another NFC scan.

## Notes

MiSTer Hi-Fi does not include music files, album artwork, radio streams, or custom fonts.

## Credits

**Developer:** Anime0t4ku  
**Contributor:** Phoenix

## License

MiSTer Hi-Fi is released under the **GNU General Public License v3.0**.

The GPLv3 license applies to MiSTer Hi-Fi's own source code and does not replace the separate licenses of third-party software used by the project.
