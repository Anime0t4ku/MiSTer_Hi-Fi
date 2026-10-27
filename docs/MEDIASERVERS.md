# Plex, Jellyfin and Emby

Connect MiSTer Hi-Fi to your music server to browse artists, albums, recently added music and playlists. Setup is easiest from a phone or computer on the same network as your MiSTer.

## Open Source Settings

1. Start MiSTer Hi-Fi.
2. Open `http://YOUR-MISTER-IP:8182` in your browser.
3. Open **Settings > Source Settings**.
4. Choose **Plex**, **Jellyfin** or **Emby**.

Use your server's local address, including its port. For example, a Jellyfin or Emby server might use `http://192.168.1.100:8096`; Plex commonly uses `http://192.168.1.100:32400`. Replace the example address with your own server's address.

## Plex

1. Enter your **Server URL**.
2. Enter your **X-Plex-Token**. This is an access token for your Plex account, not your password.
3. Leave **Music library ID** blank to use the first music library, or enter the ID of the library you want.
4. Save the settings and use the connection test.

MiSTer Hi-Fi does not provide a Plex account sign-in screen. If you need help obtaining your token, follow Plex's [token instructions](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/). Treat the token like a password.

When editing the same server later, leaving the token field blank keeps its saved value.

## Jellyfin

1. Enter your **Server URL** and save it.
2. Start **Quick Connect** in the Jellyfin settings page.
3. In Jellyfin, open your account's **Quick Connect** page and approve the displayed code.
4. Wait for Hi-Fi to show that you are signed in, then test the connection.

Quick Connect must be available on your Jellyfin server. Hi-Fi saves the issued access token, so you do not need to approve the code every time you listen.

You can also start the Quick Connect sign-in from the Jellyfin source on the MiSTer screen.

## Emby

1. Enter your **Server URL** and save it.
2. Enter your Emby **username and password** in the sign-in form.
3. Sign in, then test the connection.

Hi-Fi saves the access token and user ID returned by Emby, rather than storing the password you entered. Emby does not use the Jellyfin Quick Connect flow.

## Browse and play

Once configured, the server appears in the source list on the MiSTer and in the Web Remote.

1. Open the server source.
2. Choose artists, albums, recently added albums or playlists.
3. Open an album or playlist and select a song.

Playback starts from the selected song. The player shows the title, artist, album and artwork when available. Plex's recently added view shows up to 100 albums.

A server can appear in the source list even while it is offline; opening it may then show a connection error.

## Loading and playback

Hi-Fi downloads the selected song's original file to temporary storage before playback. There is no server-side transcoding. Longer or larger files can take more time to prepare, especially over a slow connection.

The next song is downloaded in advance where possible. **Stop** or **Next** cancels a pending download. M4A music can contain AAC-LC or Apple Lossless (ALAC) audio.

**Gapless playback** is optional and experimental. Enable it under **Settings > Experimental**. It works with cached FLAC and WAV songs from these servers. If the next song has not finished downloading, playback waits for it instead. MP3 and M4A do not use this gapless path.

## Playback history and scrobbling

Choose the scrobbling option in the server's source settings:

| Option | What it does |
| --- | --- |
| Server / Plex | Uses the music server's own playback-history handling. |
| Last.fm | Lets Hi-Fi submit listens to Last.fm. Configure Last.fm first in App Settings. |
| ListenBrainz | Lets Hi-Fi submit listens to ListenBrainz. Add your user token in App Settings first. |
| Both | Lets Hi-Fi submit to Last.fm and ListenBrainz. |
| None | Disables the selected scrobbling submission. Playback status may still be reported to the server. |

Choosing Last.fm or ListenBrainz does not set up those accounts automatically. See the [scrobbling instructions](USER-GUIDE.md#lastfm-scrobbling).

## Other source options

- **Music library ID:** choose a specific library when your server has more than one. Leave it blank to use the default music-library selection.
- **Maximum track size:** limits each downloaded song, not your whole library. Raise it if a large lossless file exceeds the limit, provided your MiSTer has enough temporary storage.
- **Allow insecure TLS:** skips certificate verification for servers using a self-signed certificate. Leave this off unless your server needs it.

Changing the server address clears the saved login associated with the previous address. Sign in again or supply the token for the new server.

## If it does not connect

- Check that the server is running and its address includes the correct port.
- Confirm that your MiSTer can reach it on the network.
- Use the source's connection test to see the reported error.
- For Plex, check the token and selected library.
- For Jellyfin, check Quick Connect availability and approve a fresh code if necessary.
- For Emby, sign in again and check that the account can access the music library.
- If only one song fails, check its format, file size and the server's access permissions.
