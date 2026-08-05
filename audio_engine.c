#define _POSIX_C_SOURCE 200809L
#include "audio_engine.h"
#include "m4a_bridge.h"
#define STB_VORBIS_HEADER_ONLY
#include "stb_vorbis.h"
#define MINIAUDIO_IMPLEMENTATION
#include "miniaudio.h"
#undef STB_VORBIS_HEADER_ONLY
#include "stb_vorbis.h"
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
#include <pthread.h>
#include <limits.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>

#define MH_RATE 44100
#define MH_CHANNELS 2
#define MH_RING_FRAMES (MH_RATE * 4)
#define MH_FILE_RING_FRAMES (MH_RATE)
#define MH_FILE_PREFILL_FRAMES 2048
#define MH_FILE_DECODE_CHUNK 4096
#define MH_STREAM_REPLAY_BYTES (256 * 1024)
#define MH_PI 3.14159265358979323846

typedef struct {
    float b0,b1,b2,a1,a2,z1,z2;
} mh_biquad;

typedef struct {
    int fd;
    ma_uint64 pos;
    ma_uint64 physical_pos;
    unsigned char* replay;
    size_t replay_size;
    size_t replay_cap;
} mh_fd_source;

typedef struct {
    ma_device device;
    ma_decoder decoder;
    ma_pcm_rb ring;
    void* m4a_decoder;
    ma_resampler m4a_resampler;
    int m4a_resampler_init;
    int m4a_rate;
    pthread_t decoder_thread;
    int decoder_thread_init;
    volatile int decoder_thread_stop;
    volatile int decoder_eof;
    int decoder_stream;
    ma_dr_flac* radio_ogg_flac;
    ma_dr_mp3 radio_mp3;
    int radio_mp3_init;
    ma_resampler radio_mp3_resampler;
    int radio_mp3_resampler_init;
    int radio_mp3_rate;
    int radio_mp3_channels;
    ma_resampler radio_flac_resampler;
    int radio_flac_resampler_init;
    int radio_flac_rate;
    int radio_flac_channels;
    int decoder_init;
    int device_init;
    int ring_init;
    volatile int mode;
    volatile int paused;
    volatile int ended;
    volatile int pcm_eof;
    volatile uint64_t frames_played;
    volatile uint64_t frames_written;
    volatile uint64_t track_start_frame;
    volatile uint64_t transition_frame;
    volatile int transition_armed;
    volatile int transitioned;
    double duration;
    double pending_duration;
    mh_fd_source* decoder_source;
    int next_fd;
    volatile int next_fd_ready;
    pthread_mutex_t next_mutex;
    float levels[10];
    float viz_pcm[2][1024 * MH_CHANNELS];
    ma_uint32 viz_frames[2];
    volatile int viz_ready;
    int eq_enabled;
    mh_biquad eq[2][5];
    char err[256];
} mh_audio_state;

static mh_audio_state g = { .next_mutex = PTHREAD_MUTEX_INITIALIZER, .next_fd = -1 };
static const float g_eq_freq[5] = {60.0f,250.0f,1000.0f,4000.0f,12000.0f};
static const float g_viz_freq[10] = {60.0f,120.0f,250.0f,500.0f,1000.0f,2000.0f,4000.0f,8000.0f,12000.0f,16000.0f};

static size_t mh_source_read(mh_fd_source* src, void* pBufferOut, size_t bytesToRead) {
    if (!src || src->fd < 0 || !pBufferOut || bytesToRead == 0) return 0;
    unsigned char* out = (unsigned char*)pBufferOut;
    size_t done = 0;

    while (done < bytesToRead) {
        if (src->pos < src->physical_pos) {
            if (src->pos >= src->replay_size) return done;
            ma_uint64 cached64 = (ma_uint64)src->replay_size - src->pos;
            size_t need = bytesToRead - done;
            size_t cached = cached64 > (ma_uint64)need ? need : (size_t)cached64;
            memcpy(out + done, src->replay + (size_t)src->pos, cached);
            src->pos += (ma_uint64)cached;
            done += cached;
            continue;
        }

        ssize_t n;
        do { n = read(src->fd, out + done, bytesToRead - done); } while (n < 0 && errno == EINTR);
        if (n <= 0) break;

        if (src->physical_pos < src->replay_cap) {
            size_t room = src->replay_cap - (size_t)src->physical_pos;
            size_t keep = (size_t)n < room ? (size_t)n : room;
            if (keep > 0) {
                memcpy(src->replay + (size_t)src->physical_pos, out + done, keep);
                size_t end = (size_t)src->physical_pos + keep;
                if (end > src->replay_size) src->replay_size = end;
            }
        }
        src->physical_pos += (ma_uint64)n;
        src->pos += (ma_uint64)n;
        done += (size_t)n;
    }
    return done;
}

static ma_result mh_fd_read(ma_decoder* pDecoder, void* pBufferOut, size_t bytesToRead, size_t* pBytesRead) {
    mh_fd_source* src = (mh_fd_source*)pDecoder->pUserData;
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    ssize_t n;
    do { n = read(src->fd, pBufferOut, bytesToRead); } while (n < 0 && errno == EINTR);
    if (n < 0) return MA_ERROR;
    if (n > 0) src->pos += (ma_uint64)n;
    if (pBytesRead) *pBytesRead = (size_t)n;
    return MA_SUCCESS;
}

static ma_result mh_stream_read(ma_decoder* pDecoder, void* pBufferOut, size_t bytesToRead, size_t* pBytesRead) {
    mh_fd_source* src = (mh_fd_source*)pDecoder->pUserData;
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    size_t n = mh_source_read(src, pBufferOut, bytesToRead);
    if (pBytesRead) *pBytesRead = n;
    return MA_SUCCESS;
}

static ma_result mh_fd_seek(ma_decoder* pDecoder, ma_int64 byteOffset, ma_seek_origin origin) {
    mh_fd_source* src = (mh_fd_source*)pDecoder->pUserData;
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    int whence = origin == ma_seek_origin_current ? SEEK_CUR : SEEK_SET;
    off_t r;
    do { r = lseek(src->fd, (off_t)byteOffset, whence); } while (r == (off_t)-1 && errno == EINTR);
    if (r == (off_t)-1 && errno == ESPIPE) return MA_NOT_IMPLEMENTED;
    if (r == (off_t)-1) return MA_ERROR;
    src->pos = (ma_uint64)r;
    src->physical_pos = src->pos;
    return MA_SUCCESS;
}

static mh_fd_source* mh_fd_source_dup(int fd) {
    int owned = dup(fd);
    if (owned < 0) return NULL;
    mh_fd_source* src = (mh_fd_source*)calloc(1, sizeof(*src));
    if (!src) { close(owned); return NULL; }
    src->replay = (unsigned char*)malloc(MH_STREAM_REPLAY_BYTES);
    if (!src->replay) { close(owned); free(src); return NULL; }
    src->fd = owned;
    src->pos = 0;
    src->physical_pos = 0;
    src->replay_size = 0;
    src->replay_cap = MH_STREAM_REPLAY_BYTES;
    return src;
}

static void mh_fd_source_free(mh_fd_source* src) {
    if (!src) return;
    if (src->fd >= 0) close(src->fd);
    free(src->replay);
    free(src);
}

static ma_result mh_decoder_init_fd_source(mh_fd_source* src, const ma_decoder_config* dc, ma_decoder* decoder) {
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    off_t r = lseek(src->fd, 0, SEEK_SET);
    if (r == (off_t)-1 && errno != ESPIPE) return MA_ERROR;
    return ma_decoder_init(mh_fd_read, mh_fd_seek, src, dc, decoder);
}

static int mh_source_seek(mh_fd_source* src, ma_int64 byteOffset, int originStart) {
    if (!src || src->fd < 0) return 0;
    ma_int64 base = originStart ? 0 : (ma_int64)src->pos;
    ma_int64 target64 = base + byteOffset;
    if (target64 < 0) return 0;
    ma_uint64 target = (ma_uint64)target64;

    if (target <= src->physical_pos) {
        if (target > (ma_uint64)src->replay_size) return 0;
        src->pos = target;
        return 1;
    }

    unsigned char discard[2048];
    while (src->pos < target) {
        ma_uint64 remaining = target - src->pos;
        size_t want = remaining > sizeof(discard) ? sizeof(discard) : (size_t)remaining;
        size_t got = mh_source_read(src, discard, want);
        if (got == 0) return 0;
    }
    return 1;
}

static ma_result mh_stream_seek(ma_decoder* pDecoder, ma_int64 byteOffset, ma_seek_origin origin) {
    mh_fd_source* src = (mh_fd_source*)pDecoder->pUserData;
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    if (origin != ma_seek_origin_start && origin != ma_seek_origin_current) return MA_NOT_IMPLEMENTED;
    return mh_source_seek(src, byteOffset, origin == ma_seek_origin_start) ? MA_SUCCESS : MA_NOT_IMPLEMENTED;
}

static ma_result mh_decoder_init_stream_source(mh_fd_source* src, const ma_decoder_config* dc, ma_decoder* decoder) {
    if (!src || src->fd < 0) return MA_INVALID_ARGS;
    src->pos = 0;
    return ma_decoder_init(mh_stream_read, mh_stream_seek, src, dc, decoder);
}

static void mh_sleep_us(long usec) {
    struct timespec ts;
    ts.tv_sec = usec / 1000000L;
    ts.tv_nsec = (usec % 1000000L) * 1000L;
    while (nanosleep(&ts, &ts) == -1) {}
}

static void mh_seterr(const char* s) {
    if (!s) s = "unknown audio error";
    snprintf(g.err, sizeof(g.err), "%s", s);
}

static void mh_biquad_peak(mh_biquad* q, float freq, float gain_db) {
    float A = powf(10.0f, gain_db / 40.0f);
    float w0 = 2.0f * (float)MH_PI * freq / (float)MH_RATE;
    float alpha = sinf(w0) / 2.0f;
    float c = cosf(w0);
    float b0 = 1.0f + alpha * A;
    float b1 = -2.0f * c;
    float b2 = 1.0f - alpha * A;
    float a0 = 1.0f + alpha / A;
    float a1 = -2.0f * c;
    float a2 = 1.0f - alpha / A;
    q->b0=b0/a0; q->b1=b1/a0; q->b2=b2/a0; q->a1=a1/a0; q->a2=a2/a0; q->z1=0; q->z2=0;
}

static inline float mh_biquad_run(mh_biquad* q, float x) {
    float y = q->b0*x + q->z1;
    q->z1 = q->b1*x - q->a1*y + q->z2;
    q->z2 = q->b2*x - q->a2*y;
    return y;
}

static void mh_setup_eq(int enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    float gains[5] = {bass,lowmid,mid,highmid,treble};
    g.eq_enabled = enabled;
    for (int ch=0; ch<2; ++ch) for (int i=0; i<5; ++i) mh_biquad_peak(&g.eq[ch][i], g_eq_freq[i], gains[i]);
}

static void mh_analyze_snapshot(const float* pcm, ma_uint32 frames) {
    if (frames < 8) return;
    ma_uint32 n = frames > 1024 ? 1024 : frames;
    for (int b=0; b<10; ++b) {
        float w = 2.0f * (float)MH_PI * g_viz_freq[b] / (float)MH_RATE;
        float coeff = 2.0f * cosf(w);
        float s0=0,s1=0,s2=0;
        for (ma_uint32 i=0;i<n;++i) {
            float mono = 0.5f*(pcm[i*2]+pcm[i*2+1]);
            s0 = mono + coeff*s1 - s2;
            s2=s1; s1=s0;
        }
        float power = s1*s1+s2*s2-coeff*s1*s2;
        float v = sqrtf(fmaxf(power,0.0f))/(float)n*5.0f;
        if (v > 1.0f) v=1.0f;
        g.levels[b] = g.levels[b]*0.72f + v*0.28f;
    }
}

static void mh_capture_visualizer(const float* pcm, ma_uint32 frames) {
    if (frames == 0) return;
    ma_uint32 n = frames > 1024 ? 1024 : frames;
    int slot = g.viz_ready == 0 ? 1 : 0;
    memcpy(g.viz_pcm[slot], pcm, n * MH_CHANNELS * sizeof(float));
    g.viz_frames[slot] = n;
    g.viz_ready = slot;
}


static int mh_ring_write_f32(const float* src, ma_uint32 frames) {
    ma_uint32 done = 0;
    while (done < frames && !g.decoder_thread_stop) {
        ma_uint32 avail = ma_pcm_rb_available_write(&g.ring);
        if (avail == 0) { mh_sleep_us(1000); continue; }
        ma_uint32 n = frames - done;
        if (n > avail) n = avail;
        void* pWrite = 0;
        ma_uint32 cont = n;
        if (ma_pcm_rb_acquire_write(&g.ring, &cont, &pWrite) != MA_SUCCESS || cont == 0) {
            mh_sleep_us(500);
            continue;
        }
        memcpy(pWrite, src + (size_t)done * MH_CHANNELS, (size_t)cont * MH_CHANNELS * sizeof(float));
        ma_pcm_rb_commit_write(&g.ring, cont);
        g.frames_written += cont;
        done += cont;
    }
    return done == frames ? 0 : -1;
}


static size_t mh_drflac_stream_read(void* pUserData, void* pBufferOut, size_t bytesToRead) {
    return mh_source_read((mh_fd_source*)pUserData, pBufferOut, bytesToRead);
}

static ma_bool32 mh_drflac_stream_seek(void* pUserData, int offset, ma_dr_flac_seek_origin origin) {
    if (origin != MA_DR_FLAC_SEEK_SET && origin != MA_DR_FLAC_SEEK_CUR) return MA_FALSE;
    return mh_source_seek((mh_fd_source*)pUserData, (ma_int64)offset, origin == MA_DR_FLAC_SEEK_SET) ? MA_TRUE : MA_FALSE;
}

static ma_bool32 mh_drflac_stream_tell(void* pUserData, ma_int64* pCursor) {
    mh_fd_source* src = (mh_fd_source*)pUserData;
    if (!src || !pCursor) return MA_FALSE;
    *pCursor = (ma_int64)src->pos;
    return MA_TRUE;
}


static size_t mh_drmp3_stream_read(void* pUserData, void* pBufferOut, size_t bytesToRead) {
    return mh_source_read((mh_fd_source*)pUserData, pBufferOut, bytesToRead);
}

static int mh_radio_flac_decode_to_ring(ma_uint32 source_frames) {
    if (!g.radio_ogg_flac) return -1;
    float raw[MH_FILE_DECODE_CHUNK * 8];
    float stereo[MH_FILE_DECODE_CHUNK * MH_CHANNELS];
    float out[MH_FILE_DECODE_CHUNK * 2 * MH_CHANNELS];
    while (source_frames > 0 && !g.decoder_thread_stop) {
        ma_uint32 want = source_frames > MH_FILE_DECODE_CHUNK ? MH_FILE_DECODE_CHUNK : source_frames;
        ma_uint64 got = ma_dr_flac_read_pcm_frames_f32(g.radio_ogg_flac, want, raw);
        if (got == 0) { g.decoder_eof = 1; return 0; }
        if (g.radio_flac_channels == 2) {
            memcpy(stereo, raw, (size_t)got * 2 * sizeof(float));
        } else if (g.radio_flac_channels == 1) {
            for (ma_uint64 i=0;i<got;++i) stereo[i*2] = stereo[i*2+1] = raw[i];
        } else {
            for (ma_uint64 i=0;i<got;++i) { stereo[i*2] = raw[i*g.radio_flac_channels]; stereo[i*2+1] = raw[i*g.radio_flac_channels+1]; }
        }
        if (g.radio_flac_resampler_init) {
            ma_uint64 in_frames = got;
            ma_uint64 out_frames = (ma_uint64)((double)got * (double)MH_RATE / (double)g.radio_flac_rate) + 32;
            ma_uint64 out_cap = MH_FILE_DECODE_CHUNK * 2;
            if (out_frames > out_cap) out_frames = out_cap;
            if (ma_resampler_process_pcm_frames(&g.radio_flac_resampler, stereo, &in_frames, out, &out_frames) != MA_SUCCESS) return -1;
            if (out_frames && mh_ring_write_f32(out, (ma_uint32)out_frames) != 0) return -1;
        } else {
            if (mh_ring_write_f32(stereo, (ma_uint32)got) != 0) return -1;
        }
        if ((ma_uint32)got >= source_frames) break;
        source_frames -= (ma_uint32)got;
    }
    return 0;
}

static void* mh_radio_flac_worker(void* arg) {
    (void)arg;
    while (!g.decoder_thread_stop) {
        ma_uint32 avail = ma_pcm_rb_available_write(&g.ring);
        if (avail < 512) { mh_sleep_us(1000); continue; }
        if (mh_radio_flac_decode_to_ring(MH_FILE_DECODE_CHUNK) != 0) { g.decoder_eof = 1; break; }
        if (g.decoder_eof) break;
    }
    return NULL;
}


static int mh_radio_mp3_decode_to_ring(ma_uint32 source_frames) {
    if (!g.radio_mp3_init) return -1;
    while (source_frames > 0 && !g.decoder_thread_stop) {
        ma_uint32 want = source_frames > MH_FILE_DECODE_CHUNK ? MH_FILE_DECODE_CHUNK : source_frames;
        float raw[MH_FILE_DECODE_CHUNK * 2];
        ma_uint64 got = ma_dr_mp3_read_pcm_frames_f32(&g.radio_mp3, want, raw);
        if (got == 0) { g.decoder_eof = 1; return -1; }
        float stereo[MH_FILE_DECODE_CHUNK * 2];
        if (g.radio_mp3_channels == 2) {
            memcpy(stereo, raw, (size_t)got * 2 * sizeof(float));
        } else if (g.radio_mp3_channels == 1) {
            for (ma_uint64 i=0;i<got;++i) stereo[i*2]=stereo[i*2+1]=raw[i];
        } else {
            for (ma_uint64 i=0;i<got;++i) { stereo[i*2]=raw[i*g.radio_mp3_channels]; stereo[i*2+1]=raw[i*g.radio_mp3_channels+1]; }
        }
        const float* out = stereo;
        ma_uint64 out_frames = got;
        float resampled[MH_FILE_DECODE_CHUNK * 4];
        if (g.radio_mp3_resampler_init) {
            ma_uint64 in_frames = got;
            out_frames = (ma_uint64)((double)got * (double)MH_RATE / (double)g.radio_mp3_rate) + 32;
            if (out_frames > MH_FILE_DECODE_CHUNK * 2) out_frames = MH_FILE_DECODE_CHUNK * 2;
            if (ma_resampler_process_pcm_frames(&g.radio_mp3_resampler, stereo, &in_frames, resampled, &out_frames) != MA_SUCCESS) return -1;
            out = resampled;
        }
        ma_uint64 written = 0;
        while (written < out_frames && !g.decoder_thread_stop) {
            void* dst = NULL;
            ma_uint32 avail = (ma_uint32)(out_frames - written);
            if (ma_pcm_rb_acquire_write(&g.ring, &avail, &dst) != MA_SUCCESS) return -1;
            if (avail == 0) { mh_sleep_us(2000); continue; }
            memcpy(dst, out + written*2, (size_t)avail * 2 * sizeof(float));
            ma_pcm_rb_commit_write(&g.ring, avail);
            g.frames_written += avail;
            written += avail;
        }
        source_frames -= (ma_uint32)got;
    }
    return 0;
}

static void* mh_radio_mp3_worker(void* arg) {
    (void)arg;
    while (!g.decoder_thread_stop && !g.decoder_eof) {
        if (ma_pcm_rb_available_write(&g.ring) < MH_FILE_DECODE_CHUNK) { mh_sleep_us(3000); continue; }
        if (mh_radio_mp3_decode_to_ring(MH_FILE_DECODE_CHUNK) != 0) { g.decoder_eof = 1; break; }
    }
    return NULL;
}

static int mh_start_radio_mp3_thread(void) {
    g.decoder_thread_stop = 0;
    if (pthread_create(&g.decoder_thread, NULL, mh_radio_mp3_worker, NULL) != 0) {
        mh_seterr("unable to start MP3 radio worker");
        return -1;
    }
    g.decoder_thread_init = 1;
    return 0;
}

static int mh_start_radio_flac_thread(void) {
    g.decoder_thread_stop = 0;
    if (pthread_create(&g.decoder_thread, NULL, mh_radio_flac_worker, NULL) != 0) {
        mh_seterr("unable to start Ogg FLAC radio worker");
        return -1;
    }
    g.decoder_thread_init = 1;
    return 0;
}

static int mh_m4a_decode_to_ring(ma_uint32 source_frames) {
    if (!g.m4a_decoder || !g.m4a_resampler_init) return -1;
    float inbuf[MH_FILE_DECODE_CHUNK * MH_CHANNELS];
    float outbuf[MH_FILE_DECODE_CHUNK * 8 * MH_CHANNELS];
    while (source_frames > 0 && !g.decoder_thread_stop) {
        int want = (int)source_frames;
        if (want > MH_FILE_DECODE_CHUNK) want = MH_FILE_DECODE_CHUNK;
        int got = mh_m4a_read_stereo_f32(g.m4a_decoder, inbuf, want);
        if (got < 0) return -1;
        if (got == 0) { g.decoder_eof = 1; return 0; }
        ma_uint64 in_frames = (ma_uint64)got;
        ma_uint64 out_frames = (ma_uint64)((double)got * (double)MH_RATE / (double)g.m4a_rate) + 32;
        ma_uint64 out_cap = MH_FILE_DECODE_CHUNK * 8;
        if (out_frames > out_cap) out_frames = out_cap;
        if (ma_resampler_process_pcm_frames(&g.m4a_resampler, inbuf, &in_frames, outbuf, &out_frames) != MA_SUCCESS) return -1;
        if (out_frames > 0 && mh_ring_write_f32(outbuf, (ma_uint32)out_frames) != 0) return -1;
        if ((ma_uint32)got >= source_frames) break;
        source_frames -= (ma_uint32)got;
    }
    return 0;
}

static void* mh_m4a_decode_worker(void* arg) {
    (void)arg;
    while (!g.decoder_thread_stop) {
        ma_uint32 avail = ma_pcm_rb_available_write(&g.ring);
        if (avail < 512) { mh_sleep_us(1000); continue; }
        ma_uint32 want = MH_FILE_DECODE_CHUNK;
        if (mh_m4a_decode_to_ring(want) != 0) {
            g.decoder_eof = 1;
            break;
        }
        if (g.decoder_eof) break;
    }
    return NULL;
}

static int mh_start_m4a_decoder_thread(void) {
    g.decoder_thread_stop = 0;
    if (pthread_create(&g.decoder_thread, NULL, mh_m4a_decode_worker, NULL) != 0) {
        mh_seterr("unable to start M4A read-ahead worker");
        return -1;
    }
    g.decoder_thread_init = 1;
    return 0;
}

static void* mh_file_decode_worker(void* arg) {
    (void)arg;
    float pcm[MH_FILE_DECODE_CHUNK * MH_CHANNELS];
    while (!g.decoder_thread_stop) {
        ma_uint32 avail = ma_pcm_rb_available_write(&g.ring);
        if (avail < 512) { mh_sleep_us(1000); continue; }
        ma_uint32 want = avail;
        if (want > MH_FILE_DECODE_CHUNK) want = MH_FILE_DECODE_CHUNK;
        ma_uint64 got = 0;
        ma_result r = ma_decoder_read_pcm_frames(&g.decoder, pcm, want, &got);
        if (got > 0 && mh_ring_write_f32(pcm, (ma_uint32)got) != 0) break;
        if (g.decoder_stream) {
            if (r == MA_AT_END) { g.decoder_eof = 1; break; }
            if (r != MA_SUCCESS) { g.decoder_eof = 1; break; }
            if (got == 0) mh_sleep_us(2000);
            continue;
        }
        if (r != MA_SUCCESS || got < want) {
            int next_fd = -1;
            pthread_mutex_lock(&g.next_mutex);
            if (g.next_fd_ready) {
                next_fd = g.next_fd;
                g.next_fd = -1;
                g.next_fd_ready = 0;
            }
            pthread_mutex_unlock(&g.next_mutex);
            if (next_fd >= 0) {
                ma_uint64 boundary = g.frames_written;
                ma_decoder next_decoder;
                ma_decoder_config dc = ma_decoder_config_init(ma_format_f32, MH_CHANNELS, MH_RATE);
                mh_fd_source* next_source = (mh_fd_source*)malloc(sizeof(*next_source));
                if (next_source) {
                    next_source->fd = next_fd;
                    if (mh_decoder_init_fd_source(next_source, &dc, &next_decoder) == MA_SUCCESS) {
                        ma_uint64 next_frames = 0;
                        double next_duration = 0.0;
                        if (ma_decoder_get_length_in_pcm_frames(&next_decoder, &next_frames) == MA_SUCCESS) {
                            next_duration = (double)next_frames / (double)MH_RATE;
                        }
                        ma_decoder_uninit(&g.decoder);
                        mh_fd_source_free(g.decoder_source);
                        g.decoder = next_decoder;
                        g.decoder_source = next_source;
                        g.decoder_init = 1;
                        g.pending_duration = next_duration;
                        g.transition_frame = boundary;
                        g.transition_armed = 1;
                        g.decoder_eof = 0;
                        continue;
                    }
                    mh_fd_source_free(next_source);
                } else {
                    close(next_fd);
                }
            }
            g.decoder_eof = 1;
            break;
        }
    }
    return NULL;
}

static void mh_stop_decoder_thread(void) {
    if (!g.decoder_thread_init) return;
    g.decoder_thread_stop = 1;
    pthread_join(g.decoder_thread, NULL);
    g.decoder_thread_init = 0;
}

static int mh_start_decoder_thread(void) {
    g.decoder_thread_stop = 0;
    if (pthread_create(&g.decoder_thread, NULL, mh_file_decode_worker, NULL) != 0) {
        mh_seterr("unable to start audio read-ahead worker");
        return -1;
    }
    g.decoder_thread_init = 1;
    return 0;
}

static int mh_prefill_file_ring(ma_uint32 frames) {
    float pcm[MH_FILE_PREFILL_FRAMES * MH_CHANNELS];
    while (frames > 0) {
        ma_uint32 want = frames;
        if (want > MH_FILE_PREFILL_FRAMES) want = MH_FILE_PREFILL_FRAMES;
        ma_uint64 got = 0;
        ma_result r = ma_decoder_read_pcm_frames(&g.decoder, pcm, want, &got);
        if (got > 0 && mh_ring_write_f32(pcm, (ma_uint32)got) != 0) return -1;
        if (g.decoder_stream) {
            if (r == MA_AT_END) { g.decoder_eof = 1; break; }
            if (r != MA_SUCCESS) return -1;
            if (got == 0) { mh_sleep_us(2000); continue; }
            frames -= (ma_uint32)got;
            continue;
        }
        if (r != MA_SUCCESS || got < want) {
            g.decoder_eof = 1;
            break;
        }
        frames -= (ma_uint32)got;
    }
    return 0;
}

static void mh_process(float* pcm, ma_uint32 frames) {
    if (g.eq_enabled) {
        for (ma_uint32 i=0;i<frames;++i) {
            for (int ch=0;ch<2;++ch) {
                float x=pcm[i*2+ch];
                for (int b=0;b<5;++b) x=mh_biquad_run(&g.eq[ch][b],x);
                if (x>1) x=1; else if (x<-1) x=-1;
                pcm[i*2+ch]=x;
            }
        }
    }
}

static void mh_callback(ma_device* d, void* out, const void* in, ma_uint32 frameCount) {
    (void)d; (void)in;
    float* dst=(float*)out;
    memset(dst,0,frameCount*MH_CHANNELS*sizeof(float));
    if (g.paused || g.mode==0) return;
    ma_uint64 got=0;
    if (g.mode==1 || g.mode==2 || g.mode==3) {
        ma_uint32 remaining=frameCount;
        while (remaining>0) {
            ma_uint32 avail=ma_pcm_rb_available_read(&g.ring);
            if (avail==0) break;
            ma_uint32 want=remaining;
            if (want>avail) want=avail;
            void* pRead=0;
            ma_uint32 cont=want;
            if (ma_pcm_rb_acquire_read(&g.ring,&cont,&pRead)!=MA_SUCCESS || cont==0) break;
            memcpy(dst+(size_t)got*MH_CHANNELS,pRead,(size_t)cont*MH_CHANNELS*sizeof(float));
            ma_pcm_rb_commit_read(&g.ring,cont);
            got+=cont;
            remaining-=cont;
        }
        if ((g.mode==1 || g.mode==3) && g.decoder_eof && ma_pcm_rb_available_read(&g.ring)==0) g.ended=1;
        if (g.mode==2 && g.pcm_eof && ma_pcm_rb_available_read(&g.ring)==0) g.ended=1;
    }
    if (got>0) {
        mh_process(dst,(ma_uint32)got);
        mh_capture_visualizer(dst,(ma_uint32)got);
        g.frames_played += got;
        if (g.transition_armed && g.frames_played >= g.transition_frame) {
            g.track_start_frame = g.transition_frame;
            if (g.pending_duration > 0.0) g.duration = g.pending_duration;
            g.pending_duration = 0.0;
            g.transition_armed = 0;
            g.transitioned = 1;
        }
    }
}

static int mh_start_device(void) {
    ma_device_config c=ma_device_config_init(ma_device_type_playback);
    c.playback.format=ma_format_f32;
    c.playback.channels=MH_CHANNELS;
    c.sampleRate=MH_RATE;
    c.dataCallback=mh_callback;
    c.periodSizeInMilliseconds=20;
    if (ma_device_init(NULL,&c,&g.device)!=MA_SUCCESS) { mh_seterr("unable to open MiSTer audio output"); return -1; }
    g.device_init=1;
    if (ma_device_start(&g.device)!=MA_SUCCESS) { mh_seterr("unable to start MiSTer audio output"); ma_device_uninit(&g.device); g.device_init=0; return -1; }
    return 0;
}

void mh_audio_stop(void) {
    if (g.device_init) { ma_device_uninit(&g.device); g.device_init=0; }
    mh_stop_decoder_thread();
    if (g.decoder_init) { ma_decoder_uninit(&g.decoder); g.decoder_init=0; }
    if (g.radio_ogg_flac) { ma_dr_flac_close(g.radio_ogg_flac); g.radio_ogg_flac=NULL; }
    if (g.radio_mp3_init) { ma_dr_mp3_uninit(&g.radio_mp3); g.radio_mp3_init=0; }
    if (g.radio_mp3_resampler_init) { ma_resampler_uninit(&g.radio_mp3_resampler, NULL); g.radio_mp3_resampler_init=0; }
    g.radio_mp3_rate=0; g.radio_mp3_channels=0;
    if (g.radio_flac_resampler_init) { ma_resampler_uninit(&g.radio_flac_resampler, NULL); g.radio_flac_resampler_init=0; }
    g.radio_flac_rate=0; g.radio_flac_channels=0;
    mh_fd_source_free(g.decoder_source); g.decoder_source=NULL;
    if (g.m4a_resampler_init) { ma_resampler_uninit(&g.m4a_resampler, NULL); g.m4a_resampler_init=0; }
    if (g.m4a_decoder) { mh_m4a_close(g.m4a_decoder); g.m4a_decoder=NULL; }
    g.m4a_rate=0;
    if (g.ring_init) { ma_pcm_rb_uninit(&g.ring); g.ring_init=0; }
    g.mode=0; g.paused=0; g.ended=0; g.pcm_eof=0; g.decoder_eof=0; g.decoder_stream=0;
    g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_frame=0;
    g.transition_armed=0; g.transitioned=0; g.duration=0.0; g.pending_duration=0.0;
    pthread_mutex_lock(&g.next_mutex);
    if (g.next_fd >= 0) close(g.next_fd);
    g.next_fd=-1; g.next_fd_ready=0;
    pthread_mutex_unlock(&g.next_mutex);
    for (int i=0;i<10;++i) g.levels[i]=0;
    g.viz_frames[0]=0; g.viz_frames[1]=0; g.viz_ready=0;
}

int mh_audio_start_fd(int fd, int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_audio_stop();
    g.decoder_source = mh_fd_source_dup(fd);
    if (!g.decoder_source) { mh_seterr("unable to open audio file descriptor"); return -1; }
    ma_decoder_config dc=ma_decoder_config_init(ma_format_f32,MH_CHANNELS,MH_RATE);
    if (mh_decoder_init_fd_source(g.decoder_source,&dc,&g.decoder)!=MA_SUCCESS) { mh_seterr("unable to decode audio file"); mh_fd_source_free(g.decoder_source); g.decoder_source=NULL; return -1; }
    g.decoder_init=1; g.decoder_stream=0; g.mode=1; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0;
    ma_uint64 duration_frames=0;
    if (ma_decoder_get_length_in_pcm_frames(&g.decoder,&duration_frames)==MA_SUCCESS) g.duration=(double)duration_frames/(double)MH_RATE;
    if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create audio read-ahead buffer"); mh_audio_stop(); return -1; }
    g.ring_init=1;
    g.decoder_thread_stop=0;
    mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
    if (mh_prefill_file_ring(MH_FILE_PREFILL_FRAMES)!=0) { mh_seterr("unable to prefill audio buffer"); mh_audio_stop(); return -1; }
    if (!g.decoder_eof && mh_start_decoder_thread()!=0) { mh_audio_stop(); return -1; }
    if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
    return 0;
}

int mh_audio_start_stream_fd(int fd, int encoding, int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_audio_stop();
    g.decoder_source = mh_fd_source_dup(fd);
    if (!g.decoder_source) { mh_seterr("unable to open radio stream"); return -1; }

    if (encoding == 5) {
        g.radio_ogg_flac = ma_dr_flac_open_relaxed(mh_drflac_stream_read, mh_drflac_stream_seek, mh_drflac_stream_tell, ma_dr_flac_container_ogg, g.decoder_source, NULL);
        if (!g.radio_ogg_flac) { mh_seterr("unable to decode Ogg FLAC radio stream"); mh_fd_source_free(g.decoder_source); g.decoder_source=NULL; return -1; }
        g.radio_flac_rate = (int)g.radio_ogg_flac->sampleRate;
        g.radio_flac_channels = (int)g.radio_ogg_flac->channels;
        if (g.radio_flac_rate <= 0 || g.radio_flac_channels <= 0 || g.radio_flac_channels > 8) { mh_seterr("invalid Ogg FLAC radio format"); mh_audio_stop(); return -1; }
        if (g.radio_flac_rate != MH_RATE) {
            ma_resampler_config rc = ma_resampler_config_init(ma_format_f32, MH_CHANNELS, (ma_uint32)g.radio_flac_rate, MH_RATE, ma_resample_algorithm_linear);
            if (ma_resampler_init(&rc, NULL, &g.radio_flac_resampler) != MA_SUCCESS) { mh_seterr("unable to create radio resampler"); mh_audio_stop(); return -1; }
            g.radio_flac_resampler_init=1;
        }
        g.decoder_stream=1; g.mode=1; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0; g.duration=0.0;
        if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create radio read-ahead buffer"); mh_audio_stop(); return -1; }
        g.ring_init=1; g.decoder_thread_stop=0;
        mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
        if (mh_radio_flac_decode_to_ring(MH_FILE_PREFILL_FRAMES)!=0 || g.decoder_eof) { mh_seterr("unable to prefill Ogg FLAC radio buffer"); mh_audio_stop(); return -1; }
        if (mh_start_radio_flac_thread()!=0) { mh_audio_stop(); return -1; }
        if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
        return 0;
    }


    if (encoding == 2) {
        if (!ma_dr_mp3_init(&g.radio_mp3, mh_drmp3_stream_read, NULL, NULL, NULL, g.decoder_source, NULL)) {
            mh_seterr("unable to decode MP3 radio stream"); mh_fd_source_free(g.decoder_source); g.decoder_source=NULL; return -1;
        }
        g.radio_mp3_init = 1;
        g.radio_mp3_rate = (int)g.radio_mp3.sampleRate;
        g.radio_mp3_channels = (int)g.radio_mp3.channels;
        if (g.radio_mp3_rate <= 0 || g.radio_mp3_channels <= 0 || g.radio_mp3_channels > 2) { mh_seterr("invalid MP3 radio format"); mh_audio_stop(); return -1; }
        if (g.radio_mp3_rate != MH_RATE) {
            ma_resampler_config rc = ma_resampler_config_init(ma_format_f32, MH_CHANNELS, (ma_uint32)g.radio_mp3_rate, MH_RATE, ma_resample_algorithm_linear);
            if (ma_resampler_init(&rc, NULL, &g.radio_mp3_resampler) != MA_SUCCESS) { mh_seterr("unable to create MP3 radio resampler"); mh_audio_stop(); return -1; }
            g.radio_mp3_resampler_init=1;
        }
        g.decoder_stream=1; g.mode=1; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0; g.duration=0.0;
        if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create MP3 radio read-ahead buffer"); mh_audio_stop(); return -1; }
        g.ring_init=1; g.decoder_thread_stop=0;
        mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
        if (mh_radio_mp3_decode_to_ring(MH_FILE_PREFILL_FRAMES)!=0 || g.decoder_eof) { mh_seterr("unable to prefill MP3 radio buffer"); mh_audio_stop(); return -1; }
        if (mh_start_radio_mp3_thread()!=0) { mh_audio_stop(); return -1; }
        if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
        return 0;
    }

    ma_decoder_config dc=ma_decoder_config_init(ma_format_f32,MH_CHANNELS,MH_RATE);
    if (encoding == 1) dc.encodingFormat = ma_encoding_format_flac;
    else if (encoding == 2) dc.encodingFormat = ma_encoding_format_mp3;
    else if (encoding == 3) dc.encodingFormat = ma_encoding_format_vorbis;
    else if (encoding == 4) dc.encodingFormat = ma_encoding_format_wav;
    if (mh_decoder_init_stream_source(g.decoder_source,&dc,&g.decoder)!=MA_SUCCESS) { mh_seterr("unable to decode radio stream"); mh_fd_source_free(g.decoder_source); g.decoder_source=NULL; return -1; }
    g.decoder_init=1; g.decoder_stream=1; g.mode=1; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0;
    g.duration=0.0;
    if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create radio read-ahead buffer"); mh_audio_stop(); return -1; }
    g.ring_init=1; g.decoder_thread_stop=0;
    mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
    if (mh_prefill_file_ring(MH_FILE_PREFILL_FRAMES)!=0) { mh_seterr("unable to prefill radio buffer"); mh_audio_stop(); return -1; }
    if (!g.decoder_eof && mh_start_decoder_thread()!=0) { mh_audio_stop(); return -1; }
    if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
    return 0;
}

int mh_audio_start_m4a_fd(int fd, int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_audio_stop();
    g.m4a_decoder = mh_m4a_open_fd(fd);
    if (!g.m4a_decoder) { mh_seterr("unable to decode M4A file"); return -1; }
    g.m4a_rate = mh_m4a_sample_rate(g.m4a_decoder);
    if (g.m4a_rate <= 0) { mh_seterr("invalid M4A sample rate"); mh_audio_stop(); return -1; }
    ma_resampler_config rc = ma_resampler_config_init(ma_format_f32, MH_CHANNELS, (ma_uint32)g.m4a_rate, MH_RATE, ma_resample_algorithm_linear);
    if (ma_resampler_init(&rc, NULL, &g.m4a_resampler) != MA_SUCCESS) { mh_seterr("unable to create M4A resampler"); mh_audio_stop(); return -1; }
    g.m4a_resampler_init=1;
    g.mode=3; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0;
    g.duration=mh_m4a_duration(g.m4a_decoder);
    if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create M4A read-ahead buffer"); mh_audio_stop(); return -1; }
    g.ring_init=1;
    g.decoder_thread_stop=0;
    mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
    if (mh_m4a_decode_to_ring(MH_FILE_PREFILL_FRAMES)!=0) { mh_seterr("unable to prefill M4A buffer"); mh_audio_stop(); return -1; }
    if (!g.decoder_eof && mh_start_m4a_decoder_thread()!=0) { mh_audio_stop(); return -1; }
    if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
    return 0;
}

int mh_audio_start_pcm(int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_audio_stop();
    if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) { mh_seterr("unable to create CD audio buffer"); return -1; }
    g.ring_init=1; g.mode=2; g.ended=0; g.pcm_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0;
    mh_setup_eq(eq_enabled,bass,lowmid,mid,highmid,treble);
    if (mh_start_device()!=0) { mh_audio_stop(); return -1; }
    return 0;
}

int mh_audio_write_pcm(const void* data, size_t bytes) {
    if (!g.ring_init || !data) return -1;
    const int16_t* src=(const int16_t*)data;
    size_t frames=bytes/(sizeof(int16_t)*2);
    size_t done=0;
    while (done<frames && g.mode==2) {
        ma_uint32 avail=ma_pcm_rb_available_write(&g.ring);
        if (avail==0) { mh_sleep_us(2000); continue; }
        ma_uint32 n=(ma_uint32)(frames-done); if (n>avail) n=avail;
        void* pWrite=0; ma_uint32 cont=n;
        if (ma_pcm_rb_acquire_write(&g.ring,&cont,&pWrite)!=MA_SUCCESS || cont==0) { mh_sleep_us(1000); continue; }
        float* dst=(float*)pWrite;
        for (ma_uint32 i=0;i<cont;++i) {
            dst[i*2]=(float)src[(done+i)*2]/32768.0f;
            dst[i*2+1]=(float)src[(done+i)*2+1]/32768.0f;
        }
        ma_pcm_rb_commit_write(&g.ring,cont);
        g.frames_written += cont;
        done+=cont;
    }
    return done==frames ? 0 : -1;
}

void mh_audio_finish_pcm(void) { g.pcm_eof=1; }
int mh_audio_queue_next_fd(int fd) {
    if (g.mode != 1 || !g.decoder_init || fd < 0) return -1;
    int owned = dup(fd);
    if (owned < 0) return -1;
    pthread_mutex_lock(&g.next_mutex);
    if (g.next_fd >= 0) close(g.next_fd);
    g.next_fd = owned;
    g.next_fd_ready = 1;
    pthread_mutex_unlock(&g.next_mutex);
    return 0;
}

int mh_audio_mark_pcm_transition(double next_duration) {
    if (g.mode != 2 || !g.ring_init || g.transition_armed) return -1;
    g.pending_duration = next_duration;
    g.transition_frame = g.frames_written;
    g.transition_armed = 1;
    return 0;
}

int mh_audio_take_transition(void) {
    if (!g.transitioned) return 0;
    g.transitioned = 0;
    return 1;
}

void mh_audio_set_eq(int enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_setup_eq(enabled,bass,lowmid,mid,highmid,treble);
}

void mh_audio_pause(int paused) { g.paused=paused?1:0; }
double mh_audio_position(void) { return (double)(g.frames_played-g.track_start_frame)/(double)MH_RATE; }
double mh_audio_duration(void) {
    return (g.mode==1 || g.mode==3) ? g.duration : 0.0;
}
int mh_audio_seek(double seconds) {
    if (seconds < 0) seconds = 0;
    if (g.mode == 3 && g.m4a_decoder) {
        int was_started = g.device_init;
        int was_paused = g.paused;
        if (was_started) ma_device_stop(&g.device);
        mh_stop_decoder_thread();
        if (g.ring_init) { ma_pcm_rb_uninit(&g.ring); g.ring_init=0; }
        if (mh_m4a_seek(g.m4a_decoder, seconds) != 0) { if (was_started) ma_device_start(&g.device); return -1; }
        if (g.m4a_resampler_init) { ma_resampler_uninit(&g.m4a_resampler, NULL); g.m4a_resampler_init=0; }
        ma_resampler_config rc = ma_resampler_config_init(ma_format_f32, MH_CHANNELS, (ma_uint32)g.m4a_rate, MH_RATE, ma_resample_algorithm_linear);
        if (ma_resampler_init(&rc, NULL, &g.m4a_resampler) != MA_SUCCESS) return -1;
        g.m4a_resampler_init=1;
        if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) return -1;
        g.ring_init=1; g.decoder_eof=0; g.ended=0; g.frames_played=(ma_uint64)(seconds*MH_RATE); g.frames_written=g.frames_played; g.track_start_frame=0; g.paused=was_paused;
        if (mh_m4a_decode_to_ring(MH_FILE_PREFILL_FRAMES)!=0) return -1;
        if (!g.decoder_eof && mh_start_m4a_decoder_thread()!=0) return -1;
        if (was_started && ma_device_start(&g.device)!=MA_SUCCESS) return -1;
        return 0;
    }
    if (g.mode != 1 || !g.decoder_init) return -1;
    ma_uint64 frame = (ma_uint64)(seconds * (double)MH_RATE);
    int was_started = g.device_init;
    int was_paused = g.paused;
    if (was_started) ma_device_stop(&g.device);
    mh_stop_decoder_thread();
    if (g.ring_init) { ma_pcm_rb_uninit(&g.ring); g.ring_init=0; }
    if (ma_decoder_seek_to_pcm_frame(&g.decoder, frame) != MA_SUCCESS) {
        if (was_started) ma_device_start(&g.device);
        return -1;
    }
    if (ma_pcm_rb_init(ma_format_f32,MH_CHANNELS,MH_FILE_RING_FRAMES,NULL,NULL,&g.ring)!=MA_SUCCESS) return -1;
    g.ring_init=1;
    g.decoder_thread_stop=0;
    g.decoder_eof=0;
    g.frames_played = frame;
    g.frames_written = frame;
    g.track_start_frame = 0;
    g.transition_armed = 0;
    g.transitioned = 0;
    g.ended = 0;
    g.paused = was_paused;
    if (mh_prefill_file_ring(MH_FILE_PREFILL_FRAMES)!=0) return -1;
    if (!g.decoder_eof && mh_start_decoder_thread()!=0) return -1;
    if (was_started && ma_device_start(&g.device) != MA_SUCCESS) return -1;
    return 0;
}
int mh_audio_ended(void) { return g.ended; }
void mh_audio_levels(float* out10) {
    if (!out10) return;
    float snapshot[1024 * MH_CHANNELS];
    int slot = g.viz_ready;
    ma_uint32 frames = g.viz_frames[slot];
    if (frames > 1024) frames = 1024;
    if (frames > 0) {
        memcpy(snapshot, g.viz_pcm[slot], frames * MH_CHANNELS * sizeof(float));
        mh_analyze_snapshot(snapshot, frames);
    }
    for (int i=0;i<10;++i) out10[i]=g.levels[i];
}
const char* mh_audio_last_error(void) { return g.err[0]?g.err:"audio engine error"; }
