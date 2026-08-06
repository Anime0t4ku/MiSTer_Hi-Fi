# MiSTer Hi-Fi

MiSTer Hi-Fi is a controller-first music player for MiSTer FPGA.

It supports local music, USB storage, SMB shares, online radio, playlists, physical Audio CDs, album artwork, a spectrum visualizer, equalizer, OLED mode, custom fallback fonts, and direct launching through NFC using Zaparoo.

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
Physical Audio CD
```

Music can be browsed directly from the source list.

Physical Audio CDs open directly in the player and automatically start from the first audio track. Press **B** from the player to open the disc track list.

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
```

M4A files can contain either **AAC-LC** or **Apple Lossless (ALAC)** audio. Embedded MP4 metadata and cover artwork are supported.

When selecting a normal audio file from the built-in browser, MiSTer Hi-Fi treats all supported audio files in the same folder as an album queue.

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

Physical Audio CDs are shown as:

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

Swaps the physical behavior of the A and B buttons without changing their on-screen labels.

### Swap X/Y

Swaps the physical behavior of the X and Y buttons without changing their on-screen labels.

These options make it possible to use Nintendo-, Xbox-, or PlayStation-style controller layouts while keeping the MiSTer Hi-Fi interface consistent.

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

M4A/MP4 playback uses a separate **Symphonia** decoder path for AAC-LC and ALAC.

Build:

```bash
chmod +x fetch_miniaudio.sh build-mister.sh
./build-mister.sh
```

The resulting MiSTer binary is placed at:

```text
Scripts/.config/MiSTerHiFi/mister_hifi
```

The repository does not store `miniaudio.h`. The build process downloads the required upstream miniaudio source automatically.

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
