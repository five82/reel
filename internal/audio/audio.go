// Package audio provides native libav decoding and libopusenc audio encoding.
package audio

/*
#cgo pkg-config: libavformat libavcodec libavutil libswresample
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <errno.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/channel_layout.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/frame.h>
#include <libavutil/log.h>
#include <libavutil/samplefmt.h>
#include <libswresample/swresample.h>
#include <stdlib.h>
#include <string.h>

// libopusenc is loaded dynamically so Reel can still build on systems where the
// optional runtime audio encoder library is not installed.
typedef struct OggOpusComments OggOpusComments;
typedef struct OggOpusEnc OggOpusEnc;

typedef OggOpusComments* (*ope_comments_create_fn)(void);
typedef void (*ope_comments_destroy_fn)(OggOpusComments *comments);
typedef OggOpusEnc* (*ope_encoder_create_file_fn)(const char *path, OggOpusComments *comments, int rate, int channels, int family, int *error);
typedef int (*ope_encoder_write_float_fn)(OggOpusEnc *enc, const float *pcm, int samples_per_channel);
typedef int (*ope_encoder_drain_fn)(OggOpusEnc *enc);
typedef void (*ope_encoder_destroy_fn)(OggOpusEnc *enc);
typedef int (*ope_encoder_ctl_fn)(OggOpusEnc *enc, int request, ...);
typedef const char* (*ope_strerror_fn)(int error);

static void *reel_ope_handle = NULL;
static ope_comments_create_fn reel_ope_comments_create = NULL;
static ope_comments_destroy_fn reel_ope_comments_destroy = NULL;
static ope_encoder_create_file_fn reel_ope_encoder_create_file = NULL;
static ope_encoder_write_float_fn reel_ope_encoder_write_float = NULL;
static ope_encoder_drain_fn reel_ope_encoder_drain = NULL;
static ope_encoder_destroy_fn reel_ope_encoder_destroy = NULL;
static ope_encoder_ctl_fn reel_ope_encoder_ctl = NULL;
static ope_strerror_fn reel_ope_strerror = NULL;

static int reel_load_sym(void **target, const char *name, char *errbuf, size_t errlen) {
	*target = dlsym(reel_ope_handle, name);
	if (*target == NULL) {
		snprintf(errbuf, errlen, "libopusenc missing symbol %s", name);
		return -1;
	}
	return 0;
}

int reel_ope_load(char *errbuf, size_t errlen) {
	if (reel_ope_handle != NULL) {
		return 0;
	}

	reel_ope_handle = dlopen("libopusenc.so.0", RTLD_LAZY);
	if (reel_ope_handle == NULL) {
		reel_ope_handle = dlopen("libopusenc.so", RTLD_LAZY);
	}
	if (reel_ope_handle == NULL) {
		const char *err = dlerror();
		snprintf(errbuf, errlen, "libopusenc not found: %s", err == NULL ? "unknown error" : err);
		return -1;
	}

	if (reel_load_sym((void **)&reel_ope_comments_create, "ope_comments_create", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_comments_destroy, "ope_comments_destroy", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_encoder_create_file, "ope_encoder_create_file", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_encoder_write_float, "ope_encoder_write_float", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_encoder_drain, "ope_encoder_drain", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_encoder_destroy, "ope_encoder_destroy", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_encoder_ctl, "ope_encoder_ctl", errbuf, errlen) < 0 ||
		reel_load_sym((void **)&reel_ope_strerror, "ope_strerror", errbuf, errlen) < 0) {
		return -1;
	}
	return 0;
}

static const int REEL_OPUS_SET_APPLICATION_REQUEST = 4000;
static const int REEL_OPUS_SET_BITRATE_REQUEST = 4002;
static const int REEL_OPUS_SET_VBR_REQUEST = 4006;
static const int REEL_OPUS_SET_MAX_BANDWIDTH_REQUEST = 4004;
static const int REEL_OPUS_SET_COMPLEXITY_REQUEST = 4010;
static const int REEL_OPUS_SET_VBR_CONSTRAINT_REQUEST = 4020;
static const int REEL_OPUS_APPLICATION_AUDIO = 2049;
static const int REEL_OPUS_BANDWIDTH_FULLBAND = 1105;
static const int REEL_OPUS_FAMILY_MONO_STEREO = 0;
static const int REEL_OPUS_FAMILY_SURROUND = 1;

OggOpusComments* reel_ope_comments_create_call(void) { return reel_ope_comments_create(); }
void reel_ope_comments_destroy_call(OggOpusComments *comments) { reel_ope_comments_destroy(comments); }
OggOpusEnc* reel_ope_encoder_create_file_call(const char *path, OggOpusComments *comments, int rate, int channels, int family, int *error) {
	return reel_ope_encoder_create_file(path, comments, rate, channels, family, error);
}
int reel_ope_encoder_write_float_call(OggOpusEnc *enc, const float *pcm, int samples_per_channel) {
	return reel_ope_encoder_write_float(enc, pcm, samples_per_channel);
}
int reel_ope_encoder_drain_call(OggOpusEnc *enc) { return reel_ope_encoder_drain(enc); }
void reel_ope_encoder_destroy_call(OggOpusEnc *enc) { reel_ope_encoder_destroy(enc); }
int reel_ope_encoder_ctl_int(OggOpusEnc *enc, int request, int value) { return reel_ope_encoder_ctl(enc, request, value); }
const char* reel_ope_strerror_call(int error) { return reel_ope_strerror(error); }

static void reel_audio_init_av_log(void) {
	av_log_set_level(AV_LOG_ERROR);
}

AVStream* reel_audio_stream_at(AVFormatContext *ctx, int idx) {
	return ctx->streams[idx];
}

static int reel_audio_stream_index(AVStream *stream) {
	return stream->index;
}

int reel_audio_find_stream_pos(AVFormatContext *ctx, int stream_index) {
	for (unsigned int i = 0; i < ctx->nb_streams; i++) {
		if (ctx->streams[i]->index == stream_index) {
			return (int)i;
		}
	}
	return -1;
}

void reel_audio_discard_except(AVFormatContext *ctx, int stream_pos) {
	for (unsigned int i = 0; i < ctx->nb_streams; i++) {
		AVStream *stream = ctx->streams[i];
		stream->discard = ((int)i == stream_pos) ? AVDISCARD_DEFAULT : AVDISCARD_ALL;
	}
}

static const char* reel_audio_metadata(AVStream *stream, const char *key) {
	AVDictionaryEntry *entry = av_dict_get(stream->metadata, key, NULL, AV_DICT_IGNORE_SUFFIX);
	if (entry == NULL) {
		return NULL;
	}
	return entry->value;
}

static const char* reel_audio_codec_name(const AVCodecParameters *par) {
	return avcodec_get_name(par->codec_id);
}

int reel_audio_channels(const AVCodecParameters *par) {
	return par->ch_layout.nb_channels;
}

int reel_swr_alloc_for_stream(struct SwrContext **swr, const AVChannelLayout *layout, int in_fmt, int in_rate) {
	return swr_alloc_set_opts2(swr, layout, AV_SAMPLE_FMT_FLT, 48000, layout, in_fmt, in_rate, 0, NULL);
}

int reel_swr_convert_frame(struct SwrContext *swr, float *out, int out_count, const AVFrame *frame) {
	uint8_t *out_data = (uint8_t *)out;
	return swr_convert(swr, &out_data, out_count, (const uint8_t **)frame->extended_data, frame->nb_samples);
}

int reel_swr_flush(struct SwrContext *swr, float *out, int out_count) {
	uint8_t *out_data = (uint8_t *)out;
	return swr_convert(swr, &out_data, out_count, NULL, 0);
}

int reel_audio_averror_eagain(void) {
	return AVERROR(EAGAIN);
}

int reel_audio_averror_eof(void) {
	return AVERROR_EOF;
}

void reel_audio_strerror(int errnum, char *buf, size_t buflen) {
	av_strerror(errnum, buf, buflen);
}
*/
import "C"

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/five82/reel/internal/ffmpeg"
	"github.com/five82/reel/internal/ffprobe"
)

const outputSampleRate = 48000

var initOnce sync.Once

func initLibav() {
	initOnce.Do(func() {
		C.reel_audio_init_av_log()
	})
}

// EncodedStream is a native Opus output for one source audio stream.
type EncodedStream struct {
	Info ffprobe.AudioStreamInfo
	Path string
}

// GetAudioChannels returns channel counts for every decodable audio stream.
func GetAudioChannels(inputPath string) ([]uint32, error) {
	streams, err := GetAudioStreamInfo(inputPath)
	if err != nil {
		return nil, err
	}
	channels := make([]uint32, len(streams))
	for i, stream := range streams {
		channels[i] = stream.Channels
	}
	return channels, nil
}

// GetAudioStreamInfo returns detailed audio stream information from libav.
func GetAudioStreamInfo(inputPath string) ([]ffprobe.AudioStreamInfo, error) {
	initLibav()

	cPath := C.CString(inputPath)
	defer C.free(unsafe.Pointer(cPath))

	var fmtCtx *C.AVFormatContext
	if ret := C.avformat_open_input(&fmtCtx, cPath, nil, nil); ret < 0 {
		return nil, fmt.Errorf("audio probe: open failed: %s", avError(ret))
	}
	defer C.avformat_close_input(&fmtCtx)

	if ret := C.avformat_find_stream_info(fmtCtx, nil); ret < 0 {
		return nil, fmt.Errorf("audio probe: stream info failed: %s", avError(ret))
	}

	streams := make([]ffprobe.AudioStreamInfo, 0)
	for i := 0; i < int(fmtCtx.nb_streams); i++ {
		stream := C.reel_audio_stream_at(fmtCtx, C.int(i))
		par := stream.codecpar
		if par.codec_type != C.AVMEDIA_TYPE_AUDIO {
			continue
		}

		channels := int(C.reel_audio_channels(par))
		if channels <= 0 {
			continue
		}

		streams = append(streams, ffprobe.AudioStreamInfo{
			Channels:    uint32(channels),
			CodecName:   cString(C.reel_audio_codec_name(par)),
			Index:       len(streams),
			StreamIndex: int(C.reel_audio_stream_index(stream)),
			Language:    metadata(stream, "language"),
			Title:       metadata(stream, "title"),
			IsSpatial:   false,
			Disposition: streamDisposition(int(stream.disposition)),
		})
	}

	return streams, nil
}

// EncodeStreams encodes all given streams to Opus in parallel.
func EncodeStreams(ctx context.Context, inputPath, workDir string, streams []ffprobe.AudioStreamInfo) ([]EncodedStream, error) {
	if len(streams) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]EncodedStream, len(streams))
	errCh := make(chan error, len(streams))
	var wg sync.WaitGroup

	for i, stream := range streams {
		i, stream := i, stream
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := AudioPath(workDir, i)
			if err := encodeOne(ctx, inputPath, path, stream); err != nil {
				cancel()
				errCh <- fmt.Errorf("stream %d: %w", stream.StreamIndex, err)
				return
			}
			results[i] = EncodedStream{Info: stream, Path: path}
		}()
	}

	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return nil, err
	}
	return results, nil
}

// AudioPath returns the per-stream Opus output path.
func AudioPath(workDir string, outputIndex int) string {
	return filepath.Join(workDir, fmt.Sprintf("audio_%02d.opus", outputIndex))
}

func encodeOne(ctx context.Context, inputPath, outputPath string, stream ffprobe.AudioStreamInfo) error {
	if stream.Channels == 0 {
		return fmt.Errorf("audio stream has no channels")
	}
	if stream.Channels > 8 {
		return fmt.Errorf("unsupported audio channel count: %d channels", stream.Channels)
	}

	dec, err := openDecoder(inputPath, stream.StreamIndex)
	if err != nil {
		return err
	}
	defer dec.close()

	enc, err := newOpusEncoder(outputPath, stream.Channels, ffmpeg.CalculateAudioBitrate(stream.Channels))
	if err != nil {
		return err
	}
	defer enc.close()

	channels := int(stream.Channels)
	return dec.decodeTo(ctx, func(pcm []float32) error {
		if channels > 2 {
			reorderSurround(pcm, channels)
		}
		return enc.writeFloat(pcm, channels)
	})
}

func metadata(stream *C.AVStream, key string) string {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))
	return cString(C.reel_audio_metadata(stream, cKey))
}

func cString(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

func streamDisposition(disposition int) ffprobe.StreamDisposition {
	enabled := func(flag C.int) int {
		if disposition&int(flag) != 0 {
			return 1
		}
		return 0
	}
	return ffprobe.StreamDisposition{
		Default:         enabled(C.AV_DISPOSITION_DEFAULT),
		Dub:             enabled(C.AV_DISPOSITION_DUB),
		Original:        enabled(C.AV_DISPOSITION_ORIGINAL),
		Comment:         enabled(C.AV_DISPOSITION_COMMENT),
		Lyrics:          enabled(C.AV_DISPOSITION_LYRICS),
		Karaoke:         enabled(C.AV_DISPOSITION_KARAOKE),
		Forced:          enabled(C.AV_DISPOSITION_FORCED),
		HearingImpaired: enabled(C.AV_DISPOSITION_HEARING_IMPAIRED),
		VisualImpaired:  enabled(C.AV_DISPOSITION_VISUAL_IMPAIRED),
		CleanEffects:    enabled(C.AV_DISPOSITION_CLEAN_EFFECTS),
		AttachedPic:     enabled(C.AV_DISPOSITION_ATTACHED_PIC),
		TimedThumbnails: enabled(C.AV_DISPOSITION_TIMED_THUMBNAILS),
	}
}

func reorderSurround(buf []float32, channels int) {
	var order []int
	switch channels {
	case 6:
		order = []int{0, 2, 1, 4, 5, 3}
	case 7:
		order = []int{0, 2, 1, 5, 6, 4, 3}
	case 8:
		order = []int{0, 2, 1, 6, 7, 4, 5, 3}
	default:
		return
	}

	var tmp [8]float32
	for sample := 0; sample < len(buf)/channels; sample++ {
		base := sample * channels
		for out, in := range order {
			tmp[out] = buf[base+in]
		}
		copy(buf[base:base+channels], tmp[:channels])
	}
}

func avError(code C.int) string {
	buf := make([]C.char, 256)
	C.reel_audio_strerror(code, &buf[0], C.size_t(len(buf)))
	return strings.TrimRight(C.GoString(&buf[0]), "\x00")
}
