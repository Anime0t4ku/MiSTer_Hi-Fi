#ifndef MISTER_HIFI_M4A_BRIDGE_H
#define MISTER_HIFI_M4A_BRIDGE_H
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
void* mh_m4a_open_fd(int fd);
void mh_m4a_close(void* decoder);
int mh_m4a_read_stereo_f32(void* decoder, float* out, int max_frames);
int mh_m4a_seek(void* decoder, double seconds);
int mh_m4a_sample_rate(void* decoder);
int mh_m4a_bits_per_sample(void* decoder);
int mh_m4a_codec(void* decoder);
double mh_m4a_duration(void* decoder);
int mh_m4a_probe_fd(int fd, int* codec, int* sample_rate, int* bits_per_sample, double* duration);
#ifdef __cplusplus
}
#endif
#endif
