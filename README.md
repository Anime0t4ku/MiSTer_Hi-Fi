# MiSTer Hi-Fi

A controller-first music player for MiSTer FPGA. Play music from your SD card, USB drives, SMB shares, Plex, Jellyfin, Emby, internet radio and audio CDs. Virtual CDs and experimental GBA music playback are also available.

Features include album artwork, visualizers, a five-band equalizer, shuffle and repeat, volume and mute, OLED mode, custom fallback fonts, scrobbling and a browser-based Web Remote.

## Get started

1. Download a release and extract it to the **root of your MiSTer SD card**.
2. Run **Scripts > misterhifi.sh** on your MiSTer.
3. Choose a source, browse to your music and select a track.

The player creates its configuration on first launch. Its files live in:

```text
/media/fat/Scripts/misterhifi.sh
/media/fat/Scripts/.config/MiSTerHiFi/
```

## Music sources

| Source | How to use it |
| --- | --- |
| SD Card / USB | Browse your folders and select music. USB appears when a drive is detected. |
| SMB | Add your network shares in the Web Remote's Source Settings. |
| Plex / Jellyfin / Emby | Configure the server in Source Settings. Each server appears after it is configured. |
| Online Radio | Add direct stream URLs in Source Settings. |
| Physical Disc | Connect a USB optical drive and insert an audio CD. This source is hidden without a detected drive. |
| Virtual CD | Select a CHD or BIN/CUE image containing CD audio tracks. |
| GBA Music (Experimental) | Enable it in Settings, then browse your GBA games and choose a song. |

Local files support **MP3, FLAC, WAV, Ogg Vorbis and M4A containing AAC-LC or ALAC (MP4 container)**, plus M3U/M3U8 playlists. Selecting a track in the normal file browser queues the other supported audio files in that folder; subfolders are not added. Playlists keep their own track order.

[Read the user guide](docs/USER-GUIDE.md) for source setup, artwork, formats and limitations.

## Controls

| Controller | Action |
| --- | --- |
| D-pad | Navigate; left/right jump through lists |
| A / B | Select / Back |
| X / Y | Play or pause / Stop and unload |
| L / R | Previous / Next track; switch tabs inside Settings |
| Start | Open Now Playing |
| Home | Open Sources |

MiSTer Hi-Fi uses your system-wide MiSTer controller mapping when available. Optional A/B and X/Y swaps are available in Settings.

On a keyboard, use **arrows**, **Enter** and **Esc/Backspace**. **Page Up/Down** moves through long lists and switches settings tabs; **Tab** opens Sources and **O** opens Now Playing. See the [complete controls](docs/USER-GUIDE.md#controls) for playback shortcuts.

Music keeps playing while you browse. Select the Now Playing bar to return to the player. Select the progress bar and press left/right to seek by 10 seconds.

## Web Remote

Open **`http://YOUR-MISTER-IP:8182`** on a device on the same network. You can browse sources, control playback and edit settings.

- **App Settings:** Playback, Appearance, Controls, System and Experimental tabs.
- **Source Settings:** Plex, Jellyfin, Emby, SMB and Online Radio setup.
- Save web settings with **Save app settings**; on-device settings save automatically.
- Changes to Web Remote enabled/port settings require restarting Hi-Fi.

You can show the Web Remote address on the MiSTer screen through **Settings > System > Show Web Address**. The port can be changed in web settings.

## Make it your own

| Settings tab | Options |
| --- | --- |
| Playback | Remember shuffle/repeat, volume and mute |
| Appearance | Visualizer, accent colour, OLED mode, artwork, clock, screensaver and fallback font |
| Controls | Button swaps, list/page jumps, wrapping and hold-repeat timing |
| System | Exit confirmation, Web Remote and on-screen web address |
| Experimental | Gapless playback, 30/60 FPS target and GBA Music |

The Web Remote also provides equalizer and scrobbling settings under Playback. The on-device player has its own Equalizer screen.

Choose spectrum bars, Retro LED, either KITT style, Retro Sunset, Retro Grid or Colour Wave. Accent presets and a custom RGB editor control highlights and other themed elements. OLED mode uses black UI backgrounds and outlined information badges; artwork and active visualizer elements retain their colours.

The FPS target defaults to **30 FPS**. **60 FPS** is experimental; actual performance depends on the display resolution and visualizer. Gapless playback is also experimental and off by default. It supports local FLAC/WAV, physical CDs and cached media-server FLAC/WAV tracks; MP3 is excluded.

## GBA Music (Experimental)

GBA Music is **off by default**. Enable it under **Settings > Experimental**. When disabled, its source is hidden and GBA playback requests are blocked.

It looks for `games/GBA` on:

- **CIFS:** `/media/fat/cifs/games/GBA`
- **SD Card:** `/media/fat/games/GBA`
- **USB:** `/media/usb0/games/GBA` through `/media/usb7/games/GBA`

Only available storage options appear. A single option opens directly; multiple USB drives get a drive selector. Folders and filenames are preserved. Open a `.gba` or `.zip` game, then select a song. Other files remain visible but cannot be played.

Only games supported by the MP2K music engine can play. An unsupported game shows a message, then returns to the file list with the game still selected. See the [confirmed-game record](docs/GBA-SUPPORTED.md); it currently contains no named confirmations.

Each song is prepared before playback. The screen shows prepared audio time and elapsed waiting time. Once ready, its measured duration, seeking and visualizer work through the normal audio player. Preparation can be cancelled with Stop or by choosing another song.

For artwork, the easiest setup is a matching image beside the ROM:

```text
Pokemon Emerald (USA).gba
Pokemon Emerald (USA).png
```

Artwork is local, not downloaded. Metadata and additional cover layouts are explained in the [GBA guide](docs/USER-GUIDE.md#gba-artwork-and-song-names).

## Launch directly or use NFC

You can launch a file, folder or playlist from a terminal:

```sh
/media/fat/Scripts/misterhifi.sh "/media/fat/Music/Artist/Album"
```

For a Zaparoo NFC tag, use:

```text
**mister.script:misterhifi.sh "/media/fat/Music/Artist/Album"
```

See the [launching and NFC examples](docs/USER-GUIDE.md#launching-and-nfc) for files, playlists, SMB and media servers, including current NFC limitations.

## Build from source

Source archives need to be compiled before installation. The target is **Linux ARMv7**, using Go, an ARM C/C++ cross-compiler and Rust/Cargo for M4A support. The repository includes a `Containerfile` describing the build environment.

```sh
chmod +x build-mister.sh fetch_miniaudio.sh fetch_libchdr.sh build_agbplay.sh fetch_agbplay.sh
./build-mister.sh
```

The result is `Scripts/.config/MiSTerHiFi/mister_hifi`. Build prerequisites and dependency details are in the [user guide](docs/USER-GUIDE.md#building).

## License

MiSTer Hi-Fi's own code is licensed under **GPLv3**. Third-party components retain their own licenses; see [dependencies and licenses](docs/USER-GUIDE.md#dependencies-and-licenses).

Music, ROMs, album artwork, radio content and custom fonts are not included.
