# MiSTer Hi-Fi

**Developer:** Anime0t4ku  
**Contributor:** Phoenix


MiSTer Hi-Fi is a controller-first music player for MiSTer FPGA.

It supports local music, USB storage, SMB shares, playlists, physical Audio CDs, album artwork, a visualizer, equalizer, OLED mode, and direct launching through NFC using Zaparoo.

## Installation

Download the latest release and extract it to the **root of your MiSTer SD card**.

The files will be installed under:

```text
/media/fat/Scripts/
├── misterhifi.sh
└── .config/
    └── MiSTerHiFi/
        ├── mister_hifi
        └── smb.example.json
```

MiSTer Hi-Fi creates its configuration automatically on first launch.

The configuration file is stored at:

```text
/media/fat/Scripts/.config/MiSTerHiFi/config.json
```

## Sources

MiSTer Hi-Fi supports:

```text
SD Card
USB
SMB
Physical Disc
```

Music can be browsed directly from the source list.

Physical Audio CDs open directly in the player and automatically start from the first audio track. Press **B** from the player to open the disc track list.

## Supported Audio

Current audio support:

```text
MP3
FLAC
WAV
M3U
M3U8
Audio CD
```

When selecting a normal audio file from the built-in browser, MiSTer Hi-Fi treats the supported audio files in the same folder as an album queue.

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

Artwork filenames are matched case-insensitively, so names such as `COVER.JPG`, `Folder.PNG` and `Front.Jpeg` also work.

If no artwork is found, MiSTer Hi-Fi uses the normal no-art player layout.

## SMB

SMB shares are configured in:

```text
/media/fat/Scripts/.config/MiSTerHiFi/smb.json
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

## Launching

Open MiSTer Hi-Fi normally:

```bash
/media/fat/Scripts/misterhifi.sh
```

Show the installed version:

```bash
/media/fat/Scripts/misterhifi.sh --version
```

Play a single file directly:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/song.flac"
```

Play a folder as an album:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/Artist/Album"
```

The first supported audio file in the folder starts automatically and the remaining tracks become the album queue.

Play a playlist directly:

```bash
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/Playlists/Favorites.m3u"
```

External single-file launches only play the requested file.

## NFC / Zaparoo

Write a MiSTer script command to the NFC tag.

Single file:

```text
**mister.script:misterhifi.sh "/media/fat/Music/song.flac"
```

Album folder:

```text
**mister.script:misterhifi.sh "/media/fat/Music/Artist/Album"
```

Playlist:

```text
**mister.script:misterhifi.sh "/media/fat/Music/Playlists/Favorites.m3u"
```

For SMB, use the configured share name instead of the temporary mount path:

```text
**mister.script:misterhifi.sh "smb://Music NAS/Artist/Album"
```

When MiSTer Hi-Fi is already running, later Zaparoo scans are sent directly to the active player. The application does not restart. The current queue is replaced and the new track, album or playlist starts immediately.

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
Previous
Play / Pause
Stop
Next
Shuffle
Repeat
Equalizer
Progress Seeking
Spectrum Visualizer
```

Playback continues while browsing other sources and folders.

A Now Playing / Paused / Loaded bar remains available while browsing and can be selected to return to the player.

## Audio Information

Now Playing displays the source audio format next to the track number.

Example:

```text
Track: 1 of 12    FLAC    16 bit    44.1 kHz    770 kbps
```

The audio information is shown in compact white blocks and includes the media format, bit depth when applicable, sample rate and bitrate.

Physical Audio CDs are shown as `CDDA`, `16 bit`, `44.1 kHz` and `1411 kbps`.

## Display Settings

- **OLED Mode** uses a true-black background.
- **Show Album Art** enables the album-art player layout.
- **Auto Hide Missing Art** automatically uses the full-width no-art player layout when the current track has no artwork. This option is disabled in Settings while Show Album Art is Off.
- **Show Clock** displays the MiSTer's 24-hour system clock in the top-right corner on every MiSTer Hi-Fi screen.
- **Confirm on Exit** asks for confirmation before closing MiSTer Hi-Fi and is enabled by default.
- **Screensaver** can turn the display completely black after 30 seconds, 1 minute, 2 minutes, 5 minutes, or 10 minutes of inactivity. Any controller or keyboard input wakes the display and the wake input is consumed. The screensaver is Off by default.
- **Gapless Playback (Experimental)** enables seamless natural track-to-track transitions for FLAC, WAV and physical Audio CD (CDDA) playback. MP3 is intentionally excluded. The setting is Off by default.
- **Swap A/B** and **Swap X/Y** change physical controller behavior without changing the on-screen button labels.

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

MiSTer Hi-Fi includes the following application settings:

```text
OLED Mode
Show Album Art
Swap A/B
Swap X/Y
```

**OLED Mode** changes the main application background from dark grey to true black.

**Show Album Art** controls whether artwork is shown in Now Playing. When disabled, the player reflows to use the full width for track information, progress seeking, the visualizer and centered playback controls.

**Swap A/B** and **Swap X/Y** change the physical controller button behavior without changing the button labels shown in the interface. This makes it possible to use Nintendo-, Xbox- or PlayStation-style controller layouts while keeping MiSTer Hi-Fi's on-screen controls consistent.

Settings are saved automatically in:

```text
/media/fat/Scripts/.config/MiSTerHiFi/config.json
```

## Building From Source

MiSTer Hi-Fi is written in Go with a small C audio bridge and targets Linux ARMv7.

It uses miniaudio for decoding and audio output.

The project does not store `miniaudio.h` in the repository. The build script downloads the pinned upstream version automatically.

Build:

```bash
chmod +x fetch_miniaudio.sh build-mister.sh
./build-mister.sh
```

The resulting binary is placed at:

```text
Scripts/.config/MiSTerHiFi/mister_hifi
```

The current build uses:

```text
miniaudio 0.11.25
```

## Third-Party Software

MiSTer Hi-Fi uses [miniaudio](https://github.com/mackron/miniaudio) by David Reid for audio decoding and playback.

miniaudio is available under the author's choice of **Public Domain** or the **MIT No Attribution (MIT-0) License**.

MiSTer Hi-Fi does not include `miniaudio.h` in its source repository. `fetch_miniaudio.sh` downloads the pinned upstream version used by the project during the build process.

## License

MiSTer Hi-Fi is released under the **GNU General Public License v3.0**.

The GPLv3 license applies to MiSTer Hi-Fi's own source code and does not replace the separate license terms of third-party software such as miniaudio.

## Known Issues

- Special characters in file or track names are not fully supported yet and may prevent affected tracks from playing. This will be addressed in a future update.

- Zaparoo's **HOLD** mode is currently not compatible with MiSTer Hi-Fi.
- While MiSTer Hi-Fi is active, Zaparoo cannot process another NFC scan.

## Notes

MiSTer Hi-Fi does not include music files or album artwork.