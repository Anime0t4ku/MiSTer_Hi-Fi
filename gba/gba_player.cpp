// See gba_player.h. Wraps agbplay's core (LGPL-3.0).
#include "gba_player.h"

#include "MP2KContext.hpp"
#include "MP2KScanner.hpp"
#include "Rom.hpp"

#include <algorithm>
#include <cstdio>
#include <cstring>
#include <exception>
#include <memory>
#include <string>
#include <vector>

struct gba_rom {
    std::unique_ptr<Rom> rom;
    MP2KScanner::Result scan;
    std::vector<int> songs; // song ids with at least one track
    std::string title, code;
    std::vector<int> all_songs, loops; // every entry of the song table with tracks, and those that loop; songs is the part that is music
};

struct gba_player {
    gba_rom *rom = nullptr;
    std::unique_ptr<MP2KContext> ctx;
    size_t pos = 0;   // frames of masterAudioBuffer already handed out
    size_t avail = 0; // frames in it
    bool ended = false;
};

static void set_err(char *err, int errlen, const std::string &msg)
{
    if (err && errlen > 0) {
        std::snprintf(err, static_cast<size_t>(errlen), "%s", msg.c_str());
    }
}


// Whether a song loops: one of its tracks ends in GOTO (0xB2) rather than FINE
// (0xB1). Sound effects, jingles and fanfares end; the songs that play on do not.
// The track data is walked from its start with the MP2K commands' argument
// sizes; a pattern call is not followed (the patterns lie behind the loop).
static bool track_loops(const Rom &rom, size_t pos)
{
    for (int steps = 0; steps < 16384 && pos < 0x2000000u; steps++) {
        const uint8_t c = rom.ReadU8(pos++);
        if (c == 0xB1) {
            return false;
        }
        if (c == 0xB2) {
            return true;
        }
        if (c < 0x80 || (c >= 0x80 && c <= 0xB0) || c == 0xB4 || c == 0xB6 || c == 0xB7 || c == 0xB8 || (c >= 0xC6 && c <= 0xC7) ||
            (c >= 0xC9 && c <= 0xCC)) {
            continue; // waits and commands without arguments
        }
        if (c == 0xB3) {
            pos += 4;
        } else if (c == 0xB5) {
            pos += 5;
        } else if (c == 0xB9) {
            pos += 3;
        } else if (c == 0xCD) {
            pos += 2;
        } else if (c >= 0xCE) {
            while (pos < 0x2000000u && rom.ReadU8(pos) < 0x80) {
                pos++; // a note or EOT/TIE: its optional key, velocity and gate time
            }
        } else {
            pos += 1; // 0xBA..0xC5, 0xC8: one argument
        }
    }
    return false;
}

static bool song_loops(const Rom &rom, size_t header)
{
    const int tracks = rom.ReadU8(header);
    for (int t = 0; t < tracks && t < 16; t++) {
        const uint32_t ptr = rom.ReadU32(header + 8u + 4u * static_cast<size_t>(t));
        if (ptr >= 0x08000000u && ptr < 0x0A000000u && track_loops(rom, ptr - 0x08000000u)) {
            return true;
        }
    }
    return false;
}

extern "C" gba_rom *gba_open(const char *path, char *err, int errlen)
{
    try {
        auto r = std::make_unique<gba_rom>();
        r->rom = std::make_unique<Rom>(Rom::LoadFromFile(path));
        MP2KScanner scanner(*r->rom);
        const auto results = scanner.Scan(nullptr);
        if (results.empty()) {
            set_err(err, errlen, "no MP2K sound engine found (this game uses another one)");
            return nullptr;
        }
        r->scan = results.front();
        for (int i = 0; i < 12; i++) {
            char c = static_cast<char>((*r->rom)[0xA0 + static_cast<size_t>(i)]);
            if (c != 0) {
                r->title.push_back(c);
            }
        }
        r->code = r->rom->GetROMCode();
        // an entry is {u32 song header pointer, u16 player, u16 pad}; the header starts with the track count
        const size_t table = r->scan.songTableInfo.pos;
        for (int i = 0; i < r->scan.songTableInfo.count; i++) {
            const uint32_t ptr = r->rom->ReadU32(table + 8u * static_cast<size_t>(i));
            if (ptr < 0x08000000u || ptr >= 0x0A000000u) {
                continue;
            }
            const size_t header = ptr - 0x08000000u;
            if (r->rom->ReadU8(header) > 0) {
                r->all_songs.push_back(i);
                if (song_loops(*r->rom, header)) {
                    r->loops.push_back(i);
                    // music has several tracks (drums, bass, melodies); a looping effect or ambience has one or two
                    if (r->rom->ReadU8(header) >= 3) {
                        r->songs.push_back(i);
                    }
                }
            }
        }
        // a game whose music does not fit this (or whose commands this walk misreads): the looping songs, else all
        if (r->songs.size() < 3) {
            r->songs = r->loops.size() >= 3 ? r->loops : r->all_songs;
        }
        if (r->songs.empty()) {
            set_err(err, errlen, "no songs found in this ROM");
            return nullptr;
        }
        return r.release();
    } catch (const std::exception &e) {
        set_err(err, errlen, e.what());
        return nullptr;
    }
}

extern "C" void gba_close(gba_rom *rom) { delete rom; }
extern "C" const char *gba_title(const gba_rom *rom) { return rom ? rom->title.c_str() : ""; }
extern "C" const char *gba_code(const gba_rom *rom) { return rom ? rom->code.c_str() : ""; }
extern "C" int gba_song_count(const gba_rom *rom) { return rom ? static_cast<int>(rom->songs.size()) : 0; }
extern "C" int gba_song_id(const gba_rom *rom, int n)
{
    if (!rom || n < 0 || static_cast<size_t>(n) >= rom->songs.size()) {
        return -1;
    }
    return rom->songs[static_cast<size_t>(n)];
}

extern "C" gba_player *gba_start(gba_rom *rom, int song_id, int rate, int loops, int quality)
{
    if (!rom || song_id < 0 || song_id > 0xFFFF || rate < 8000) {
        return nullptr;
    }
    try {
        auto p = std::make_unique<gba_player>();
        p->rom = rom;
        AgbplaySoundMode mode;
        if (quality == 0) {
            mode.resamplerTypeNormal = ResamplerType::LINEAR;
            mode.resamplerTypeFixed = ResamplerType::LINEAR;
        }
        const int8_t maxLoops = loops < 0 ? static_cast<int8_t>(-1) /* LOOP_ENDLESS */ : static_cast<int8_t>(std::min(loops, 100));
        p->ctx = std::make_unique<MP2KContext>(static_cast<uint32_t>(rate), maxLoops, *rom->rom, rom->scan.mp2kSoundMode, mode,
                                               rom->scan.songTableInfo, rom->scan.playerTableInfo);
        p->ctx->m4aSongNumStart(static_cast<uint16_t>(song_id));
        return p.release();
    } catch (const std::exception &) {
        return nullptr;
    }
}

extern "C" void gba_stop(gba_player *p) { delete p; }

extern "C" int gba_render(gba_player *p, float *out, int frames)
{
    if (!p || !out || frames <= 0) {
        return 0;
    }
    int done = 0;
    try {
        while (done < frames) {
            if (p->pos >= p->avail) {
                if (p->ended || p->ctx->SongEnded()) {
                    p->ended = true;
                    break;
                }
                p->ctx->m4aSoundMain();
                p->avail = p->ctx->masterAudioBuffer.size();
                p->pos = 0;
                if (p->avail == 0) {
                    p->ended = true;
                    break;
                }
            }
            const size_t n = std::min(static_cast<size_t>(frames - done), p->avail - p->pos);
            const auto *src = &p->ctx->masterAudioBuffer[p->pos];
            for (size_t i = 0; i < n; i++) {
                out[2 * (static_cast<size_t>(done) + i)] = src[i].left;
                out[2 * (static_cast<size_t>(done) + i) + 1] = src[i].right;
            }
            p->pos += n;
            done += static_cast<int>(n);
        }
    } catch (const std::exception &) {
        p->ended = true;
    }
    return done;
}
