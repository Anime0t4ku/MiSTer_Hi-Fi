use libc::{c_double, c_float, c_int, c_void};
use std::fs::File;
use std::os::fd::FromRawFd;
use std::ptr;
use symphonia::core::codecs::audio::well_known::{CODEC_ID_AAC, CODEC_ID_ALAC};
use symphonia::core::codecs::audio::{AudioDecoder, AudioDecoderOptions};
use symphonia::core::errors::Error;
use symphonia::core::formats::probe::Hint;
use symphonia::core::formats::{FormatOptions, FormatReader, SeekMode, SeekTo, TrackType};
use symphonia::core::io::MediaSourceStream;
use symphonia::core::meta::MetadataOptions;
use symphonia::core::units::Time;

struct M4aDecoder {
    format: Box<dyn FormatReader>,
    decoder: Box<dyn AudioDecoder>,
    track_id: u32,
    sample_rate: u32,
    channels: usize,
    bits: u32,
    codec: i32,
    duration: f64,
    buffered: Vec<f32>,
    buffered_pos: usize,
}

fn alac_bits_from_extra_data(extra: Option<&[u8]>) -> u32 {
    let Some(data) = extra else {
        return 0;
    };

    // ALACSpecificConfig is 24 bytes. The bit-depth/sample-size byte is at
    // offset 5 within that structure. Some M4A files prepend the 4-byte size
    // and 4-byte 'alac' atom header, so accept both layouts.
    for off in [0usize, 8usize] {
        if data.len() >= off + 24 {
            let bits = data[off + 5] as u32;
            if matches!(bits, 16 | 20 | 24 | 32) {
                return bits;
            }
        }
    }
    0
}

fn open_decoder(fd: c_int) -> Result<M4aDecoder, ()> {
    let owned = unsafe { libc::dup(fd) };
    if owned < 0 {
        return Err(());
    }

    let file = unsafe { File::from_raw_fd(owned) };
    let mss = MediaSourceStream::new(Box::new(file), Default::default());
    let mut hint = Hint::new();
    hint.with_extension("m4a");

    let format = symphonia::default::get_probe()
        .probe(
            &hint,
            mss,
            FormatOptions::default(),
            MetadataOptions::default(),
        )
        .map_err(|_| ())?;

    let (track_id, params, duration) = {
        let track = format.default_track(TrackType::Audio).ok_or(())?;
        let params = track
            .codec_params
            .as_ref()
            .and_then(|p| p.audio())
            .ok_or(())?
            .clone();

        let duration = if let (Some(tb), Some(dur)) = (track.time_base, track.duration) {
            tb.calc_time(symphonia::core::units::Timestamp::new(dur.get() as i64)).map(|t| t.as_secs_f64()).unwrap_or(0.0)
        } else if let (Some(frames), Some(rate)) = (track.num_frames, params.sample_rate) {
            frames as f64 / rate as f64
        } else {
            0.0
        };

        (track.id, params, duration)
    };

    let codec = if params.codec == CODEC_ID_AAC {
        1
    } else if params.codec == CODEC_ID_ALAC {
        2
    } else {
        return Err(());
    };

    let sample_rate = params.sample_rate.ok_or(())?;
    let channels = params.channels.as_ref().map(|c| c.count()).unwrap_or(2);
    let mut bits = params
        .bits_per_sample
        .or(params.bits_per_coded_sample)
        .unwrap_or(0);

    if codec == 2 && bits == 0 {
        bits = alac_bits_from_extra_data(params.extra_data.as_deref());
    }

    let decoder = symphonia::default::get_codecs()
        .make_audio_decoder(&params, &AudioDecoderOptions::default())
        .map_err(|_| ())?;

    Ok(M4aDecoder {
        format,
        decoder,
        track_id,
        sample_rate,
        channels,
        bits,
        codec,
        duration,
        buffered: Vec::new(),
        buffered_pos: 0,
    })
}

impl M4aDecoder {
    fn refill(&mut self) -> Result<bool, ()> {
        loop {
            let packet = match self.format.next_packet() {
                Ok(Some(packet)) => packet,
                Ok(None) => return Ok(false),
                Err(Error::ResetRequired) => return Err(()),
                Err(_) => return Err(()),
            };

            if packet.track_id != self.track_id {
                continue;
            }

            let decoded = match self.decoder.decode(&packet) {
                Ok(decoded) => decoded,
                Err(Error::IoError(_)) | Err(Error::DecodeError(_)) => continue,
                Err(Error::ResetRequired) => {
                    self.decoder.reset();
                    continue;
                }
                Err(_) => return Err(()),
            };

            let spec = decoded.spec();
            let ch = spec.channels().count().max(1);
            let sample_count = decoded.samples_interleaved();
            if sample_count == 0 {
                continue;
            }

            let mut samples = vec![0.0f32; sample_count];
            decoded.copy_to_slice_interleaved(&mut samples);

            let frames = samples.len() / ch;
            self.buffered.clear();
            self.buffered.reserve(frames * 2);

            if ch == 1 {
                for &v in samples.iter().take(frames) {
                    self.buffered.push(v);
                    self.buffered.push(v);
                }
            } else {
                for frame in samples.chunks_exact(ch) {
                    self.buffered.push(frame[0]);
                    self.buffered.push(frame[1]);
                }
            }

            self.buffered_pos = 0;
            self.channels = ch;
            return Ok(!self.buffered.is_empty());
        }
    }

    fn read(&mut self, out: &mut [f32], max_frames: usize) -> Result<usize, ()> {
        let mut written = 0usize;
        while written < max_frames {
            if self.buffered_pos >= self.buffered.len() {
                if !self.refill()? {
                    break;
                }
            }

            let avail_frames = (self.buffered.len() - self.buffered_pos) / 2;
            let n = avail_frames.min(max_frames - written);
            let src0 = self.buffered_pos;
            let src1 = src0 + n * 2;
            out[written * 2..(written + n) * 2]
                .copy_from_slice(&self.buffered[src0..src1]);
            self.buffered_pos = src1;
            written += n;
        }
        Ok(written)
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_open_fd(fd: c_int) -> *mut c_void {
    match open_decoder(fd) {
        Ok(d) => Box::into_raw(Box::new(d)) as *mut c_void,
        Err(_) => ptr::null_mut(),
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_close(decoder: *mut c_void) {
    if decoder.is_null() {
        return;
    }
    unsafe {
        drop(Box::from_raw(decoder as *mut M4aDecoder));
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_read_stereo_f32(
    decoder: *mut c_void,
    out: *mut c_float,
    max_frames: c_int,
) -> c_int {
    if decoder.is_null() || out.is_null() || max_frames <= 0 {
        return -1;
    }
    let d = unsafe { &mut *(decoder as *mut M4aDecoder) };
    let buf = unsafe {
        std::slice::from_raw_parts_mut(out as *mut f32, max_frames as usize * 2)
    };
    match d.read(buf, max_frames as usize) {
        Ok(n) => n as c_int,
        Err(_) => -1,
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_seek(decoder: *mut c_void, seconds: c_double) -> c_int {
    if decoder.is_null() || !seconds.is_finite() || seconds < 0.0 {
        return -1;
    }

    let d = unsafe { &mut *(decoder as *mut M4aDecoder) };
    let Some(time) = Time::try_from_secs_f64(seconds) else {
        return -1;
    };

    if d.format
        .seek(
            SeekMode::Accurate,
            SeekTo::Time {
                time,
                track_id: Some(d.track_id),
            },
        )
        .is_err()
    {
        return -1;
    }

    d.decoder.reset();
    d.buffered.clear();
    d.buffered_pos = 0;
    0
}

#[no_mangle]
pub extern "C" fn mh_m4a_sample_rate(decoder: *mut c_void) -> c_int {
    if decoder.is_null() {
        0
    } else {
        unsafe { (*(decoder as *mut M4aDecoder)).sample_rate as c_int }
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_bits_per_sample(decoder: *mut c_void) -> c_int {
    if decoder.is_null() {
        0
    } else {
        unsafe { (*(decoder as *mut M4aDecoder)).bits as c_int }
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_codec(decoder: *mut c_void) -> c_int {
    if decoder.is_null() {
        0
    } else {
        unsafe { (*(decoder as *mut M4aDecoder)).codec as c_int }
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_duration(decoder: *mut c_void) -> c_double {
    if decoder.is_null() {
        0.0
    } else {
        unsafe { (*(decoder as *mut M4aDecoder)).duration as c_double }
    }
}

#[no_mangle]
pub extern "C" fn mh_m4a_probe_fd(
    fd: c_int,
    codec: *mut c_int,
    sample_rate: *mut c_int,
    bits_per_sample: *mut c_int,
    duration: *mut c_double,
) -> c_int {
    let d = match open_decoder(fd) {
        Ok(d) => d,
        Err(_) => return -1,
    };

    unsafe {
        if !codec.is_null() {
            *codec = d.codec;
        }
        if !sample_rate.is_null() {
            *sample_rate = d.sample_rate as c_int;
        }
        if !bits_per_sample.is_null() {
            *bits_per_sample = d.bits as c_int;
        }
        if !duration.is_null() {
            *duration = d.duration as c_double;
        }
    }
    0
}
