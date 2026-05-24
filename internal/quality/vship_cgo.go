//go:build cgo && !no_vship

package quality

/*
#cgo LDFLAGS: -lvship
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct { int id; } VshipCVVDPHandler;

typedef enum {
	Vship_SampleFLOAT = 0,
	Vship_SampleHALF = 1,
	Vship_SampleUINT8 = 2,
	Vship_SampleUINT9 = 3,
	Vship_SampleUINT10 = 5,
	Vship_SampleUINT12 = 7,
	Vship_SampleUINT14 = 9,
	Vship_SampleUINT16 = 11,
} Vship_Sample;

typedef enum { Vship_RangeLimited = 0, Vship_RangeFull = 1 } Vship_Range;
typedef struct { int subw; int subh; } Vship_ChromaSubsample;
typedef enum { Vship_ChromaLeft = 0, Vship_ChromaCenter = 1, Vship_ChromaTopLeft = 2, Vship_ChromaTop = 3 } Vship_ChromaLocation;
typedef enum { Vship_ColorYUV = 0, Vship_ColorRGB = 1 } Vship_ColorFamily;
typedef enum {
	Vship_MATRIX_RGB = 0,
	Vship_MATRIX_BT709 = 1,
	Vship_MATRIX_BT470BG = 5,
	Vship_MATRIX_ST170M = 6,
	Vship_MATRIX_BT2020_NCL = 9,
	Vship_MATRIX_BT2020_CL = 10,
	Vship_MATRIX_BT2100_ICTCP = 14,
} Vship_YUVMatrix;
typedef enum {
	Vship_TRC_BT709 = 1,
	Vship_TRC_BT470M = 4,
	Vship_TRC_BT470BG = 5,
	Vship_TRC_BT601 = 6,
	Vship_TRC_LINEAR = 8,
	Vship_TRC_SRGB = 13,
	Vship_TRC_PQ = 16,
	Vship_TRC_ST428 = 17,
	Vship_TRC_HLG = 18,
} Vship_TransferFunction;
typedef enum {
	Vship_PRIMARIES_INTERNAL = -1,
	Vship_PRIMARIES_BT709 = 1,
	Vship_PRIMARIES_BT470M = 4,
	Vship_PRIMARIES_BT470BG = 5,
	Vship_PRIMARIES_BT2020 = 9,
} Vship_Primaries;
typedef struct { int top; int bottom; int left; int right; } Vship_CropRectangle;
typedef struct {
	int64_t width;
	int64_t height;
	int64_t target_width;
	int64_t target_height;
	Vship_Sample sample;
	Vship_Range range_;
	Vship_ChromaSubsample subsampling;
	Vship_ChromaLocation chroma_location;
	Vship_ColorFamily colorFamily;
	Vship_YUVMatrix YUVMatrix;
	Vship_TransferFunction transferFunction;
	Vship_Primaries primaries;
	Vship_CropRectangle crop;
} Vship_Colorspace_t;

typedef int Vship_Exception;

extern Vship_Exception Vship_SetDevice(int gpu_id);
extern Vship_Exception Vship_CVVDPInit2(VshipCVVDPHandler* handler, Vship_Colorspace_t src_colorspace, Vship_Colorspace_t dis_colorspace, float fps, bool resizeToDisplay, const char* model_key_cstr, const char* model_config_json_cstr);
extern Vship_Exception Vship_CVVDPFree(VshipCVVDPHandler handler);
extern Vship_Exception Vship_ResetCVVDP(VshipCVVDPHandler handler);
extern Vship_Exception Vship_ComputeCVVDP(VshipCVVDPHandler handler, double* score, const uint8_t *dstp, int64_t dststride, const uint8_t* srcp1[3], const uint8_t* srcp2[3], const int64_t lineSize[3], const int64_t lineSize2[3]);
extern int Vship_GetDetailedLastError(char* out_message, int len);
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"codeberg.org/five82/reel/internal/video"
)

var vshipDeviceOnce struct {
	sync.Once
	err error
}

type VshipProcessor struct {
	handler C.VshipCVVDPHandler
	closed  bool
}

func VshipBuildEnabled() bool { return true }

func NewVshipProcessor(width, height uint32, inf *video.Info, displayPath string) (*VshipProcessor, error) {
	vshipDeviceOnce.Do(func() {
		if ret := C.Vship_SetDevice(0); ret != 0 {
			vshipDeviceOnce.err = fmt.Errorf("Vship_SetDevice failed: %s", vshipLastError())
		}
	})
	if vshipDeviceOnce.err != nil {
		return nil, vshipDeviceOnce.err
	}

	if inf == nil {
		return nil, fmt.Errorf("nil video info")
	}
	fps := C.float(float64(inf.FPSNum) / float64(inf.FPSDen))
	srcColorspace := createYUVColorspace(width, height, inf)
	disColorspace := createYUVColorspace(width, height, inf)
	modelKey := C.CString(DisplayModelKey)
	defer C.free(unsafe.Pointer(modelKey))
	configPath := C.CString(displayPath)
	defer C.free(unsafe.Pointer(configPath))

	var handler C.VshipCVVDPHandler
	ret := C.Vship_CVVDPInit2(&handler, srcColorspace, disColorspace, fps, C.bool(true), modelKey, configPath)
	if ret != 0 {
		return nil, fmt.Errorf("Vship_CVVDPInit2 failed: %s", vshipLastError())
	}
	return &VshipProcessor{handler: handler}, nil
}

func (p *VshipProcessor) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	if ret := C.Vship_CVVDPFree(p.handler); ret != 0 {
		return fmt.Errorf("Vship_CVVDPFree failed: %s", vshipLastError())
	}
	return nil
}

func (p *VshipProcessor) ResetCVVDP() error {
	if p == nil || p.closed {
		return fmt.Errorf("CVVDP handler is closed")
	}
	if ret := C.Vship_ResetCVVDP(p.handler); ret != 0 {
		return fmt.Errorf("Vship_ResetCVVDP failed: %s", vshipLastError())
	}
	return nil
}

func (p *VshipProcessor) ComputeCVVDP(src, dist FramePlanes) (float32, error) {
	if p == nil || p.closed {
		return 0, fmt.Errorf("CVVDP handler is closed")
	}
	var score C.double
	srcPlanes := [3]*C.uint8_t{
		(*C.uint8_t)(unsafe.Pointer(src.Planes[0])),
		(*C.uint8_t)(unsafe.Pointer(src.Planes[1])),
		(*C.uint8_t)(unsafe.Pointer(src.Planes[2])),
	}
	distPlanes := [3]*C.uint8_t{
		(*C.uint8_t)(unsafe.Pointer(dist.Planes[0])),
		(*C.uint8_t)(unsafe.Pointer(dist.Planes[1])),
		(*C.uint8_t)(unsafe.Pointer(dist.Planes[2])),
	}
	srcStrides := [3]C.int64_t{C.int64_t(src.Strides[0]), C.int64_t(src.Strides[1]), C.int64_t(src.Strides[2])}
	distStrides := [3]C.int64_t{C.int64_t(dist.Strides[0]), C.int64_t(dist.Strides[1]), C.int64_t(dist.Strides[2])}
	ret := C.Vship_ComputeCVVDP(
		p.handler,
		&score,
		nil,
		0,
		(**C.uint8_t)(unsafe.Pointer(&srcPlanes[0])),
		(**C.uint8_t)(unsafe.Pointer(&distPlanes[0])),
		(*C.int64_t)(unsafe.Pointer(&srcStrides[0])),
		(*C.int64_t)(unsafe.Pointer(&distStrides[0])),
	)
	if ret != 0 {
		return 0, fmt.Errorf("Vship_ComputeCVVDP failed: %s", vshipLastError())
	}
	return float32(score), nil
}

func createYUVColorspace(width, height uint32, inf *video.Info) C.Vship_Colorspace_t {
	return C.Vship_Colorspace_t{
		width:            C.int64_t(width),
		height:           C.int64_t(height),
		target_width:     -1,
		target_height:    -1,
		sample:           C.Vship_SampleUINT10,
		range_:           vshipRange(inf),
		subsampling:      C.Vship_ChromaSubsample{subw: 1, subh: 1},
		chroma_location:  vshipChromaLocation(inf),
		colorFamily:      C.Vship_ColorYUV,
		YUVMatrix:        vshipMatrix(inf),
		transferFunction: vshipTransfer(inf),
		primaries:        vshipPrimaries(inf),
		crop:             C.Vship_CropRectangle{},
	}
}

func vshipChromaLocation(inf *video.Info) C.Vship_ChromaLocation {
	if inf != nil && inf.ChromaSamplePosition != nil && *inf.ChromaSamplePosition == 2 {
		return C.Vship_ChromaTopLeft
	}
	return C.Vship_ChromaLeft
}

func vshipMatrix(inf *video.Info) C.Vship_YUVMatrix {
	if inf == nil || inf.MatrixCoefficients == nil {
		return C.Vship_MATRIX_BT709
	}
	switch *inf.MatrixCoefficients {
	case 0:
		return C.Vship_MATRIX_RGB
	case 5:
		return C.Vship_MATRIX_BT470BG
	case 6:
		return C.Vship_MATRIX_ST170M
	case 9:
		return C.Vship_MATRIX_BT2020_NCL
	case 10:
		return C.Vship_MATRIX_BT2020_CL
	case 14:
		return C.Vship_MATRIX_BT2100_ICTCP
	default:
		return C.Vship_MATRIX_BT709
	}
}

func vshipTransfer(inf *video.Info) C.Vship_TransferFunction {
	if inf == nil || inf.TransferCharacteristics == nil {
		return C.Vship_TRC_BT709
	}
	switch *inf.TransferCharacteristics {
	case 4:
		return C.Vship_TRC_BT470M
	case 5:
		return C.Vship_TRC_BT470BG
	case 6:
		return C.Vship_TRC_BT601
	case 8:
		return C.Vship_TRC_LINEAR
	case 13:
		return C.Vship_TRC_SRGB
	case 16:
		return C.Vship_TRC_PQ
	case 17:
		return C.Vship_TRC_ST428
	case 18:
		return C.Vship_TRC_HLG
	default:
		return C.Vship_TRC_BT709
	}
}

func vshipPrimaries(inf *video.Info) C.Vship_Primaries {
	if inf == nil || inf.ColorPrimaries == nil {
		return C.Vship_PRIMARIES_BT709
	}
	switch *inf.ColorPrimaries {
	case -1:
		return C.Vship_PRIMARIES_INTERNAL
	case 4:
		return C.Vship_PRIMARIES_BT470M
	case 5:
		return C.Vship_PRIMARIES_BT470BG
	case 9:
		return C.Vship_PRIMARIES_BT2020
	default:
		return C.Vship_PRIMARIES_BT709
	}
}

func vshipRange(inf *video.Info) C.Vship_Range {
	if inf != nil && inf.ColorRange != nil && *inf.ColorRange == 2 {
		return C.Vship_RangeFull
	}
	return C.Vship_RangeLimited
}

func vshipLastError() string {
	buf := make([]C.char, 1024)
	C.Vship_GetDetailedLastError(&buf[0], 1024)
	return C.GoString(&buf[0])
}
