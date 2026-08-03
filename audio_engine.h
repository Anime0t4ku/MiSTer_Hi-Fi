#ifndef MISTER_HIFI_AUDIO_ENGINE_H
#define MISTER_HIFI_AUDIO_ENGINE_H
#include <stddef.h>
int mh_audio_start_file(const char* path, int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble);
int mh_audio_start_pcm(int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble);
int mh_audio_write_pcm(const void* data, size_t bytes);
void mh_audio_finish_pcm(void);
void mh_audio_pause(int paused);
void mh_audio_set_eq(int enabled, float bass, float lowmid, float mid, float highmid, float treble);
void mh_audio_stop(void);
double mh_audio_position(void);
double mh_audio_duration(void);
int mh_audio_seek(double seconds);
int mh_audio_ended(void);
void mh_audio_levels(float* out10);
const char* mh_audio_last_error(void);
#endif
