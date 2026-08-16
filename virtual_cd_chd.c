#include "virtual_cd_chd.h"
#include <libchdr/chd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define MH_MAX_TRACKS 99
#define MH_CD_DATA 2352
#define MH_CD_FRAME_WITH_SUB 2448

struct mh_chd_disc {
    chd_file* chd;
    mh_chd_track_info tracks[MH_MAX_TRACKS];
    int track_count;
    uint32_t hunkbytes;
    uint32_t framebytes;
    uint32_t frames_per_hunk;
    uint8_t* hunk;
    uint32_t cached_hunk;
    int have_cached_hunk;
};

static char g_chd_error[256];

static void mh_set_chd_error(const char* text) {
    snprintf(g_chd_error, sizeof(g_chd_error), "%s", text ? text : "CHD error");
}

const char* mh_chd_last_error(void) { return g_chd_error; }

static uint64_t round_track_frames(uint64_t frames) {
    return (frames + 3u) & ~3u;
}

static int load_tracks(mh_chd_disc* d) {
    char meta[512];
    uint64_t physical = 0;
    int use_v2 = 1;
    for (uint32_t index = 0; index < MH_MAX_TRACKS; ++index) {
        uint32_t len = 0, tag = 0;
        uint8_t flags = 0;
        chd_error err = chd_get_metadata(d->chd, use_v2 ? CDROM_TRACK_METADATA2_TAG : CDROM_TRACK_METADATA_TAG,
                                         index, meta, sizeof(meta)-1, &len, &tag, &flags);
        if (err != CHDERR_NONE && index == 0 && use_v2) {
            use_v2 = 0;
            index = (uint32_t)-1;
            continue;
        }
        if (err == CHDERR_METADATA_NOT_FOUND) break;
        if (err != CHDERR_NONE) {
            mh_set_chd_error(chd_error_string(err));
            return -1;
        }
        if (len >= sizeof(meta)) len = sizeof(meta)-1;
        meta[len] = 0;
        int number = 0, frames = 0, pregap = 0, postgap = 0;
        char type[32] = {0}, subtype[32] = {0}, pgtype[32] = {0}, pgsub[32] = {0};
        int n;
        if (use_v2) {
            n = sscanf(meta, "TRACK:%d TYPE:%31s SUBTYPE:%31s FRAMES:%d PREGAP:%d PGTYPE:%31s PGSUB:%31s POSTGAP:%d",
                       &number, type, subtype, &frames, &pregap, pgtype, pgsub, &postgap);
            if (n < 4) { mh_set_chd_error("invalid CHD CD track metadata"); return -1; }
        } else {
            n = sscanf(meta, "TRACK:%d TYPE:%31s SUBTYPE:%31s FRAMES:%d", &number, type, subtype, &frames);
            if (n < 4) { mh_set_chd_error("invalid CHD CD track metadata"); return -1; }
        }
        if (frames < 0) { mh_set_chd_error("invalid CHD CD track length"); return -1; }
        mh_chd_track_info* t = &d->tracks[d->track_count++];
        t->track_number = number;
        t->is_audio = strcmp(type, "AUDIO") == 0;
        uint64_t playable_pregap = (pregap > 0 && pregap < frames) ? (uint64_t)pregap : 0;
        t->start_frame = physical + playable_pregap;
        t->frames = (uint64_t)frames - playable_pregap;
        physical += round_track_frames((uint64_t)frames);
    }
    if (d->track_count == 0) { mh_set_chd_error("CHD contains no CD tracks"); return -1; }
    return 0;
}

int mh_chd_open_disc(const char* path, mh_chd_disc** out) {
    if (!path || !out) { mh_set_chd_error("invalid CHD path"); return -1; }
    *out = NULL;
    mh_chd_disc* d = (mh_chd_disc*)calloc(1, sizeof(*d));
    if (!d) { mh_set_chd_error("out of memory"); return -1; }
    chd_error err = chd_open(path, CHD_OPEN_READ, NULL, &d->chd);
    if (err != CHDERR_NONE) { mh_set_chd_error(chd_error_string(err)); free(d); return -1; }
    const chd_header* h = chd_get_header(d->chd);
    if (!h || h->hunkbytes == 0) { mh_set_chd_error("invalid CHD header"); mh_chd_close_disc(d); return -1; }
    d->hunkbytes = h->hunkbytes;
    if ((d->hunkbytes % MH_CD_FRAME_WITH_SUB) == 0) d->framebytes = MH_CD_FRAME_WITH_SUB;
    else if ((d->hunkbytes % MH_CD_DATA) == 0) d->framebytes = MH_CD_DATA;
    else { mh_set_chd_error("unsupported CHD CD hunk layout"); mh_chd_close_disc(d); return -1; }
    d->frames_per_hunk = d->hunkbytes / d->framebytes;
    d->hunk = (uint8_t*)malloc(d->hunkbytes);
    if (!d->hunk) { mh_set_chd_error("out of memory"); mh_chd_close_disc(d); return -1; }
    if (load_tracks(d) != 0) { mh_chd_close_disc(d); return -1; }
    *out = d;
    return 0;
}

void mh_chd_close_disc(mh_chd_disc* d) {
    if (!d) return;
    free(d->hunk);
    if (d->chd) chd_close(d->chd);
    free(d);
}

int mh_chd_get_track_count(mh_chd_disc* d) { return d ? d->track_count : 0; }

int mh_chd_get_track(mh_chd_disc* d, int index, mh_chd_track_info* out) {
    if (!d || !out || index < 0 || index >= d->track_count) return -1;
    *out = d->tracks[index];
    return 0;
}

int mh_chd_read_audio_frame(mh_chd_disc* d, uint64_t frame, void* out2352) {
    if (!d || !out2352 || d->frames_per_hunk == 0) return -1;
    uint64_t h64 = frame / d->frames_per_hunk;
    if (h64 > 0xffffffffu) { mh_set_chd_error("CHD frame out of range"); return -1; }
    uint32_t hunknum = (uint32_t)h64;
    if (!d->have_cached_hunk || d->cached_hunk != hunknum) {
        chd_error err = chd_read(d->chd, hunknum, d->hunk);
        if (err != CHDERR_NONE) { mh_set_chd_error(chd_error_string(err)); return -1; }
        d->cached_hunk = hunknum;
        d->have_cached_hunk = 1;
    }
    uint32_t within = (uint32_t)(frame % d->frames_per_hunk);
    memcpy(out2352, d->hunk + (size_t)within * d->framebytes, MH_CD_DATA);
    /* CHD stores standard CDDA samples big-endian; MiSTer/miniaudio consumes s16 LE. */
    uint8_t* pcm = (uint8_t*)out2352;
    for (uint32_t i = 0; i < MH_CD_DATA; i += 2) {
        uint8_t tmp = pcm[i];
        pcm[i] = pcm[i + 1];
        pcm[i + 1] = tmp;
    }
    return 0;
}
