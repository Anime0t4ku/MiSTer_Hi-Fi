#define _POSIX_C_SOURCE 200809L
#include "audio_engine.h"
#define MINIAUDIO_IMPLEMENTATION
#include "miniaudio.h"
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
#include <pthread.h>
#include <limits.h>

#define MH_RATE 44100
#define MH_CHANNELS 2
#define MH_RING_FRAMES (MH_RATE * 4)
#define MH_FILE_RING_FRAMES (MH_RATE)
#define MH_FILE_PREFILL_FRAMES 2048
#define MH_FILE_DECODE_CHUNK 4096
#define MH_PI 3.14159265358979323846

typedef struct {
    float b0,b1,b2,a1,a2,z1,z2;
} mh_biquad;

typedef struct {
    ma_device device;
    ma_decoder decoder;
    ma_pcm_rb ring;
    pthread_t decoder_thread;
    int decoder_thread_init;
    volatile int decoder_thread_stop;
    volatile int decoder_eof;
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
    char next_path[PATH_MAX];
    volatile int next_path_ready;
    pthread_mutex_t next_mutex;
    float levels[10];
    float viz_pcm[2][1024 * MH_CHANNELS];
    ma_uint32 viz_frames[2];
    volatile int viz_ready;
    int eq_enabled;
    mh_biquad eq[2][5];
    char err[256];
} mh_audio_state;

static mh_audio_state g = { .next_mutex = PTHREAD_MUTEX_INITIALIZER };
static const float g_eq_freq[5] = {60.0f,250.0f,1000.0f,4000.0f,12000.0f};
static const float g_viz_freq[10] = {60.0f,120.0f,250.0f,500.0f,1000.0f,2000.0f,4000.0f,8000.0f,12000.0f,16000.0f};

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
        if (r != MA_SUCCESS || got < want) {
            char next[PATH_MAX];
            next[0] = '\0';
            pthread_mutex_lock(&g.next_mutex);
            if (g.next_path_ready) {
                snprintf(next, sizeof(next), "%s", g.next_path);
                g.next_path_ready = 0;
                g.next_path[0] = '\0';
            }
            pthread_mutex_unlock(&g.next_mutex);
            if (next[0] != '\0') {
                ma_uint64 boundary = g.frames_written;
                ma_decoder next_decoder;
                ma_decoder_config dc = ma_decoder_config_init(ma_format_f32, MH_CHANNELS, MH_RATE);
                if (ma_decoder_init_file(next, &dc, &next_decoder) == MA_SUCCESS) {
                    ma_uint64 next_frames = 0;
                    double next_duration = 0.0;
                    if (ma_decoder_get_length_in_pcm_frames(&next_decoder, &next_frames) == MA_SUCCESS) {
                        next_duration = (double)next_frames / (double)MH_RATE;
                    }
                    ma_decoder_uninit(&g.decoder);
                    g.decoder = next_decoder;
                    g.decoder_init = 1;
                    g.pending_duration = next_duration;
                    g.transition_frame = boundary;
                    g.transition_armed = 1;
                    g.decoder_eof = 0;
                    continue;
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
    if (g.mode==1 || g.mode==2) {
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
        if (g.mode==1 && g.decoder_eof && ma_pcm_rb_available_read(&g.ring)==0) g.ended=1;
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
    if (g.ring_init) { ma_pcm_rb_uninit(&g.ring); g.ring_init=0; }
    g.mode=0; g.paused=0; g.ended=0; g.pcm_eof=0; g.decoder_eof=0;
    g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_frame=0;
    g.transition_armed=0; g.transitioned=0; g.duration=0.0; g.pending_duration=0.0;
    pthread_mutex_lock(&g.next_mutex);
    g.next_path_ready=0; g.next_path[0]='\0';
    pthread_mutex_unlock(&g.next_mutex);
    for (int i=0;i<10;++i) g.levels[i]=0;
    g.viz_frames[0]=0; g.viz_frames[1]=0; g.viz_ready=0;
}

int mh_audio_start_file(const char* path, int eq_enabled, float bass, float lowmid, float mid, float highmid, float treble) {
    mh_audio_stop();
    ma_decoder_config dc=ma_decoder_config_init(ma_format_f32,MH_CHANNELS,MH_RATE);
    if (ma_decoder_init_file(path,&dc,&g.decoder)!=MA_SUCCESS) { mh_seterr("unable to decode audio file"); return -1; }
    g.decoder_init=1; g.mode=1; g.ended=0; g.decoder_eof=0; g.frames_played=0; g.frames_written=0; g.track_start_frame=0; g.transition_armed=0; g.transitioned=0;
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
int mh_audio_queue_next_file(const char* path) {
    if (g.mode != 1 || !g.decoder_init || !path || !path[0]) return -1;
    pthread_mutex_lock(&g.next_mutex);
    snprintf(g.next_path, sizeof(g.next_path), "%s", path);
    g.next_path_ready = 1;
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
    return g.mode==1 ? g.duration : 0.0;
}
int mh_audio_seek(double seconds) {
    if (g.mode != 1 || !g.decoder_init) return -1;
    if (seconds < 0) seconds = 0;
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
