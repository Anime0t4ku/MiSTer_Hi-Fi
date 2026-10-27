#ifndef MISTER_HIFI_GBA_PLAYER_H
#define MISTER_HIFI_GBA_PLAYER_H

/* Music from GBA ROMs: a thin C interface to agbplay's core (third_party,
 * fetch_agbplay.sh), which plays the songs of ROMs that use Nintendo's MP2K
 * sound engine, the engine most GBA games have. Games with another engine are
 * reported as "not supported" by gba_open.
 *
 * Usage: gba_open a ROM, list its songs with gba_song_*, gba_start one, call
 * gba_render for stereo float frames until it returns less than asked for.
 * Every function is thread-compatible but not thread-safe on one object. */

#ifdef __cplusplus
extern "C" {
#endif

typedef struct gba_rom gba_rom;
typedef struct gba_player gba_player;

/* Opens a ROM and looks for the MP2K song table. NULL on failure (not a GBA
 * ROM, not MP2K, unreadable); err then says why. */
gba_rom *gba_open(const char *path, char *err, int errlen);
void gba_close(gba_rom *rom);

/* The game's title and code from the ROM header ("POKEMON EMER", "BPEE"). */
const char *gba_title(const gba_rom *rom);
const char *gba_code(const gba_rom *rom);

/* The songs that have music in them (the table also lists empty entries):
 * gba_song_count of them, gba_song_id the engine's song number of the nth. */
int gba_song_count(const gba_rom *rom);
int gba_song_id(const gba_rom *rom, int n);

/* Starts a song. rate is the output rate (44100); loops is how many times the
 * song repeats before it fades out (1 = once; -1 = forever); quality 0 is the
 * cheap resampler (for the MiSTer's ARM), 1 the engine's default. NULL on failure. */
gba_player *gba_start(gba_rom *rom, int song_id, int rate, int loops, int quality);
void gba_stop(gba_player *p);

/* Renders up to `frames` stereo frames (L, R floats, -1..1) into out; returns
 * how many were rendered: less than asked for means the song has ended. */
int gba_render(gba_player *p, float *out, int frames);

#ifdef __cplusplus
}
#endif
#endif
