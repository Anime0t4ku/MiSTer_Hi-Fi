#ifndef MISTER_HIFI_VIRTUAL_CD_CHD_H
#define MISTER_HIFI_VIRTUAL_CD_CHD_H
#include <stdint.h>
typedef struct mh_chd_disc mh_chd_disc;
typedef struct {
    int track_number;
    int is_audio;
    uint64_t start_frame;
    uint64_t frames;
} mh_chd_track_info;
int mh_chd_open_disc(const char* path, mh_chd_disc** out);
void mh_chd_close_disc(mh_chd_disc* disc);
int mh_chd_get_track_count(mh_chd_disc* disc);
int mh_chd_get_track(mh_chd_disc* disc, int index, mh_chd_track_info* out);
int mh_chd_read_audio_frame(mh_chd_disc* disc, uint64_t frame, void* out2352);
const char* mh_chd_last_error(void);
#endif
