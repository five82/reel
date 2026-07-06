package encoder

/*
#cgo pkg-config: SvtAv1Enc

#include <EbSvtAv1Enc.h>
#include <malloc.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>

#if SVT_AV1_CHECK_VERSION(4, 0, 0)
static void noop_log_callback(void* context, SvtAv1LogLevel level, const char* tag, const char* fmt, va_list args) {
    (void)context;
    (void)level;
    (void)tag;
    (void)fmt;
    (void)args;
}

static SvtAv1LogCallback get_noop_log_callback(void) {
    return noop_log_callback;
}
#endif

static void reel_disable_svt_logs(void) {
#if SVT_AV1_CHECK_VERSION(4, 0, 0)
    svt_av1_set_log_callback(get_noop_log_callback(), NULL);
#endif
}

static EbErrorType reel_svt_init_handle(EbComponentType** handle, EbSvtAv1EncConfiguration* config) {
#if SVT_AV1_CHECK_VERSION(4, 0, 0)
    return svt_av1_enc_init_handle(handle, config);
#else
    return svt_av1_enc_init_handle(handle, NULL, config);
#endif
}

static void reel_set_level_of_parallelism(EbSvtAv1EncConfiguration* config, uint32_t value) {
#if SVT_AV1_CHECK_VERSION(2, 0, 0)
    config->level_of_parallelism = value;
#else
    (void)config;
    (void)value;
#endif
}

static void reel_malloc_trim(void) {
    malloc_trim(0);
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"codeberg.org/five82/reel/internal/quality"
	"codeberg.org/five82/reel/internal/video"
)

// SVTVersion returns the version string reported by the linked SVT-AV1 library.
func SVTVersion() string {
	version := C.svt_av1_get_version()
	if version == nil {
		return "unknown"
	}
	return C.GoString(version)
}

// svtEncoder wraps a single SVT-AV1 library encoder instance.
type svtEncoder struct {
	handle   *C.EbComponentType
	ioFormat unsafe.Pointer
	inHdr    unsafe.Pointer
	width    uint16
	height   uint16
}

// newSvtEncoder creates and initializes an SVT-AV1 encoder.
func newSvtEncoder(cfg *EncConfig) (*svtEncoder, error) {
	C.reel_disable_svt_logs()

	var handle *C.EbComponentType
	var config C.EbSvtAv1EncConfiguration

	ret := C.reel_svt_init_handle(&handle, &config)
	if ret != C.EB_ErrorNone {
		return nil, fmt.Errorf("svt_av1_enc_init_handle failed: %d", int32(ret))
	}

	if err := setSvtConfig(&config, cfg); err != nil {
		C.svt_av1_enc_deinit_handle(handle)
		return nil, err
	}

	ret = C.svt_av1_enc_set_parameter(handle, &config)
	if ret != C.EB_ErrorNone {
		C.svt_av1_enc_deinit_handle(handle)
		return nil, fmt.Errorf("svt_av1_enc_set_parameter failed: %d", int32(ret))
	}

	ret = C.svt_av1_enc_init(handle)
	if ret != C.EB_ErrorNone {
		C.svt_av1_enc_deinit_handle(handle)
		return nil, fmt.Errorf("svt_av1_enc_init failed: %d", int32(ret))
	}

	ioFmt := C.malloc(C.sizeof_EbSvtIOFormat)
	if ioFmt == nil {
		C.svt_av1_enc_deinit(handle)
		C.svt_av1_enc_deinit_handle(handle)
		return nil, fmt.Errorf("failed to allocate SVT-AV1 IO format")
	}
	C.memset(ioFmt, 0, C.sizeof_EbSvtIOFormat)

	inHdr := C.malloc(C.sizeof_EbBufferHeaderType)
	if inHdr == nil {
		C.free(ioFmt)
		C.svt_av1_enc_deinit(handle)
		C.svt_av1_enc_deinit_handle(handle)
		return nil, fmt.Errorf("failed to allocate SVT-AV1 buffer header")
	}
	C.memset(inHdr, 0, C.sizeof_EbBufferHeaderType)

	hdr := (*C.EbBufferHeaderType)(inHdr)
	hdr.size = C.uint32_t(C.sizeof_EbBufferHeaderType)
	hdr.p_buffer = (*C.uint8_t)(ioFmt)
	hdr.n_filled_len = C.uint32_t(video.Calc10BitSize(cfg.Width, cfg.Height))
	hdr.n_alloc_len = hdr.n_filled_len

	return &svtEncoder{
		handle:   handle,
		ioFormat: ioFmt,
		inHdr:    inHdr,
		width:    uint16(cfg.Width),
		height:   uint16(cfg.Height),
	}, nil
}

// sendFrame sends a single frame to the encoder. frame must be 10-bit YUV420.
func (e *svtEncoder) sendFrame(frame []byte, pts int64) error {
	if len(frame) == 0 {
		return fmt.Errorf("empty frame buffer")
	}

	ySize := int(e.width) * int(e.height) * 2
	uvSize := int(e.width/2) * int(e.height/2) * 2

	ioFmt := (*C.EbSvtIOFormat)(e.ioFormat)
	ioFmt.luma = (*C.uint8_t)(unsafe.Pointer(&frame[0]))
	ioFmt.cb = (*C.uint8_t)(unsafe.Pointer(&frame[ySize]))
	ioFmt.cr = (*C.uint8_t)(unsafe.Pointer(&frame[ySize+uvSize]))
	ioFmt.y_stride = C.uint32_t(e.width)
	ioFmt.cb_stride = C.uint32_t(e.width / 2)
	ioFmt.cr_stride = C.uint32_t(e.width / 2)

	hdr := (*C.EbBufferHeaderType)(e.inHdr)
	hdr.pts = C.int64_t(pts)

	ret := C.svt_av1_enc_send_picture(e.handle, hdr)
	if ret != C.EB_ErrorNone {
		return fmt.Errorf("svt_av1_enc_send_picture failed: %d", int32(ret))
	}
	return nil
}

// sendEOS signals the end of the stream.
func (e *svtEncoder) sendEOS() error {
	var eos C.EbBufferHeaderType
	C.memset(unsafe.Pointer(&eos), 0, C.sizeof_EbBufferHeaderType)
	eos.size = C.uint32_t(C.sizeof_EbBufferHeaderType)
	eos.flags = C.EB_BUFFERFLAG_EOS

	ret := C.svt_av1_enc_send_picture(e.handle, &eos)
	if ret != C.EB_ErrorNone {
		return fmt.Errorf("svt_av1_enc_send_picture (EOS) failed: %d", int32(ret))
	}
	return nil
}

// drainPackets retrieves encoded packets from the encoder and writes them as IVF frames.
// If done is true, the call blocks until all remaining packets are returned.
// Returns the number of frames written.
func (e *svtEncoder) drainPackets(writeFrame func([]byte, int64) error, done bool) (int, error) {
	count := 0
	for {
		var pkt *C.EbBufferHeaderType
		var doneU8 C.uint8_t
		if done {
			doneU8 = 1
		}
		ret := C.svt_av1_enc_get_packet(e.handle, &pkt, doneU8)
		if ret != C.EB_ErrorNone {
			break
		}
		if pkt.n_filled_len > 0 {
			data := unsafe.Slice((*byte)(unsafe.Pointer(pkt.p_buffer)), int(pkt.n_filled_len))
			if err := writeFrame(data, int64(pkt.pts)); err != nil {
				C.svt_av1_enc_release_out_buffer(&pkt)
				return count, err
			}
			count++
		}
		eos := pkt.flags&C.EB_BUFFERFLAG_EOS != 0
		C.svt_av1_enc_release_out_buffer(&pkt)
		if eos {
			break
		}
	}
	return count, nil
}

// close deinitializes and destroys the encoder.
func (e *svtEncoder) close() {
	if e.handle != nil {
		C.svt_av1_enc_deinit(e.handle)
		C.svt_av1_enc_deinit_handle(e.handle)
		e.handle = nil
	}
	if e.ioFormat != nil {
		C.free(e.ioFormat)
		e.ioFormat = nil
	}
	if e.inHdr != nil {
		C.free(e.inHdr)
		e.inHdr = nil
	}
	C.reel_malloc_trim()
}

// Playback compatibility contract: every encode signals AV1 level 5.1 main
// tier and caps the bitstream at that level's main-tier maximum bitrate.
// SVT's auto level derives only from resolution/fps, so uncapped CRF encodes
// of grainy content can burst far past the signaled level's bitrate bound.
// Hardware decoders provision buffers and throughput from the signaled level
// and stall on the violation (observed: Pixel 10 Pro stalling on a 56 Mbps
// burst in a 1080p stream that signaled level 4.0 / 12 Mbps). Level 5.1
// (4096x2176@60) covers every disc source Reel encodes and is the baseline
// capability of AV1 hardware decoders. The cap engages SVT's capped-CRF mode.
//
// On chunks where the cap binds hard, lowering CRF degrades CVVDP (the rate
// regulator thrashes), so the target-quality search's monotonicity guard
// fires and the chunk falls back to its initial CRF with the cap holding the
// rate. That fallback is the intended behavior, not a search failure.

// signaledLevel is EbSvtAv1EncConfiguration.level's encoding of AV1
// level 5.1 (major*10 + minor).
const signaledLevel = 51

// maxBitRateBps is AV1 level 5.1 main tier MaxBitrate in bits/second.
// A var so tests can encode an uncapped baseline; production never mutates it.
var maxBitRateBps = uint32(40_000_000)

// MaxBitRateBps returns the bitstream cap applied to every encode, for the
// target-quality search to reject probes whose encodes escaped it.
func MaxBitRateBps() uint32 {
	return maxBitRateBps
}

// setSvtConfig populates the SVT-AV1 configuration using parse_parameter.
func setSvtConfig(config *C.EbSvtAv1EncConfiguration, cfg *EncConfig) error {
	fps := float64(cfg.Inf.FPSNum) / float64(cfg.Inf.FPSDen)
	keyintFrames := int(fps * 10)

	params := [][2]string{
		{"input-depth", "10"},
		{"color-format", "1"},
		{"profile", "0"},
		{"tile-rows", "0"},
		{"tile-columns", "0"},
		{"keyint", fmt.Sprintf("%d", keyintFrames)},
		{"rc", "0"},
		{"scd", "0"},
		{"scm", "0"},
		{"width", fmt.Sprintf("%d", cfg.Width)},
		{"forced-max-frame-width", fmt.Sprintf("%d", cfg.Width)},
		{"height", fmt.Sprintf("%d", cfg.Height)},
		{"forced-max-frame-height", fmt.Sprintf("%d", cfg.Height)},
		{"fps-num", fmt.Sprintf("%d", cfg.Inf.FPSNum)},
		{"fps-denom", fmt.Sprintf("%d", cfg.Inf.FPSDen)},
		{"crf", quality.FormatCRF(cfg.CRF)},
		{"preset", fmt.Sprintf("%d", cfg.Preset)},
		{"tune", fmt.Sprintf("%d", cfg.Tune)},
	}
	for _, param := range params {
		if err := parseSvtParam(config, param[0], param[1]); err != nil {
			return err
		}
	}
	if cfg.LevelOfParallelism > 0 {
		C.reel_set_level_of_parallelism(config, C.uint32_t(cfg.LevelOfParallelism))
	}

	// Set directly: "level" has no parse_parameter name, and max_bit_rate's
	// struct field is unambiguously bits/second while the CLI's "mbr" is kbps.
	config.level = C.uint32_t(signaledLevel)
	config.max_bit_rate = C.uint32_t(maxBitRateBps)
	if maxBitRateBps > 0 {
		// Plain max_bit_rate is only loosely enforced (observed ~60%
		// overshoot at the default allowance). gop-constraint-rc would be the
		// principled fix but SVT restricts it to VBR. Tightening the
		// overshoot allowance is the remaining lever in capped-CRF mode.
		if err := parseSvtParam(config, "mbr-overshoot-pct", "0"); err != nil {
			return err
		}
	}

	var optionalParams [][2]string
	if cfg.Inf.ColorPrimaries != nil {
		optionalParams = append(optionalParams, [2]string{"color-primaries", fmt.Sprintf("%d", *cfg.Inf.ColorPrimaries)})
	}
	if cfg.Inf.TransferCharacteristics != nil {
		optionalParams = append(optionalParams, [2]string{"transfer-characteristics", fmt.Sprintf("%d", *cfg.Inf.TransferCharacteristics)})
	}
	if cfg.Inf.MatrixCoefficients != nil {
		optionalParams = append(optionalParams, [2]string{"matrix-coefficients", fmt.Sprintf("%d", *cfg.Inf.MatrixCoefficients)})
	}
	if cfg.Inf.ColorRange != nil {
		optionalParams = append(optionalParams, [2]string{"color-range", fmt.Sprintf("%d", *cfg.Inf.ColorRange)})
	}
	if cfg.Inf.ChromaSamplePosition != nil {
		optionalParams = append(optionalParams, [2]string{"chroma-sample-position", chromaSamplePosition(*cfg.Inf.ChromaSamplePosition)})
	}
	if cfg.Inf.MasteringDisplay != nil {
		optionalParams = append(optionalParams, [2]string{"mastering-display", *cfg.Inf.MasteringDisplay})
	}
	if cfg.Inf.ContentLight != nil {
		optionalParams = append(optionalParams, [2]string{"content-light", *cfg.Inf.ContentLight})
	}
	if cfg.ACBias != 0 {
		optionalParams = append(optionalParams, [2]string{"ac-bias", fmt.Sprintf("%.2f", cfg.ACBias)})
	}
	if cfg.EnableVarianceBoost {
		optionalParams = append(optionalParams,
			[2]string{"enable-variance-boost", "1"},
			[2]string{"variance-boost-strength", fmt.Sprintf("%d", cfg.VarianceBoostStrength)},
			[2]string{"variance-octile", fmt.Sprintf("%d", cfg.VarianceOctile)},
		)
	}
	for _, param := range optionalParams {
		if err := parseSvtParam(config, param[0], param[1]); err != nil {
			return err
		}
	}

	return nil
}

func parseSvtParam(config *C.EbSvtAv1EncConfiguration, name, value string) error {
	cName := C.CString(name)
	cValue := C.CString(value)
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cValue))

	if ret := C.svt_av1_enc_parse_parameter(config, cName, cValue); ret != C.EB_ErrorNone {
		return fmt.Errorf("svt-av1 rejected parameter %s=%q: %d", name, value, int32(ret))
	}
	return nil
}
