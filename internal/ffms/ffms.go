// Package ffms provides CGO bindings to FFMS2 for video frame extraction.
package ffms

/*
#cgo pkg-config: ffms2
#include <ffms.h>
#include <stdlib.h>
#include <string.h>

#define ERR_BUF_SIZE 1024

// Helper to create an error info struct with C-allocated buffer
static FFMS_ErrorInfo* create_error_info() {
	FFMS_ErrorInfo* err = (FFMS_ErrorInfo*)malloc(sizeof(FFMS_ErrorInfo));
	err->Buffer = (char*)malloc(ERR_BUF_SIZE);
	err->BufferSize = ERR_BUF_SIZE;
	err->Buffer[0] = '\0';
	return err;
}

// Helper to free error info struct
static void free_error_info(FFMS_ErrorInfo* err) {
	if (err) {
		free(err->Buffer);
		free(err);
	}
}

// Helper to get error message from FFMS_ErrorInfo
static const char* get_error_message(FFMS_ErrorInfo* err) {
	return err->Buffer;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"
)

var initOnce sync.Once

// Init initializes the FFMS2 library. Safe to call multiple times.
func Init() {
	initOnce.Do(func() {
		C.FFMS_Init(0, 0)
	})
}

// VidIdx wraps an FFMS_Index pointer.
type VidIdx struct {
	ptr       *C.FFMS_Index
	videoPath string
}

// VidSrc wraps an FFMS_VideoSource pointer.
type VidSrc struct {
	ptr *C.FFMS_VideoSource
}

// VidInf contains video properties and HDR metadata.
type VidInf struct {
	Width                   uint32
	Height                  uint32
	FPSNum                  uint32
	FPSDen                  uint32
	Frames                  int
	ColorPrimaries          *int32
	TransferCharacteristics *int32
	MatrixCoefficients      *int32
	Is10Bit                 bool
	MasteringDisplay        *string
	ContentLight            *string
	PixelFormat             int
}

// DecodeStrat represents the decoding strategy for frame extraction.
type DecodeStrat int

const (
	// B10Fast is fast 10-bit decoding without cropping
	B10Fast DecodeStrat = iota
	// B10Stride is 10-bit decoding with stride handling
	B10Stride
	// B8Fast is fast 8-bit decoding without cropping
	B8Fast
	// B8Stride is 8-bit decoding with stride handling
	B8Stride
	// B10CropFast is fast 10-bit decoding with cropping
	B10CropFast
	// B10CropStride is 10-bit decoding with cropping and stride handling
	B10CropStride
	// B8CropFast is fast 8-bit decoding with cropping
	B8CropFast
	// B8CropStride is 8-bit decoding with cropping and stride handling
	B8CropStride
)

// FFmpeg pixel format constants for 10-bit detection.
// These correspond to AVPixelFormat values from libavutil/pixfmt.h.
const (
	pixFmtYUV420P10LE = 62 // AV_PIX_FMT_YUV420P10LE
	pixFmtYUV420P10BE = 63 // AV_PIX_FMT_YUV420P10BE
	pixFmtYUV422P10LE = 64 // AV_PIX_FMT_YUV422P10LE
	pixFmtYUV422P10BE = 65 // AV_PIX_FMT_YUV422P10BE
	pixFmtYUV444P10LE = 66 // AV_PIX_FMT_YUV444P10LE
	pixFmtYUV444P10BE = 67 // AV_PIX_FMT_YUV444P10BE
)

// CropCalc contains crop calculation parameters for frame extraction.
type CropCalc struct {
	NewW  uint32 // Cropped width
	NewH  uint32 // Cropped height
	CropX uint32 // Left crop offset
	CropY uint32 // Top crop offset
}

// CropRect describes the exact source rectangle to encode.
type CropRect struct {
	X      uint32 // Left offset in source pixels
	Y      uint32 // Top offset in source pixels
	Width  uint32 // Output width in pixels
	Height uint32 // Output height in pixels
}

// NewVidIdx creates a new video index for the given file path.
func NewVidIdx(path string, showProgress bool) (*VidIdx, error) {
	Init()

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	// Create indexer
	indexer := C.FFMS_CreateIndexer(cPath, errInfo)
	if indexer == nil {
		return nil, fmt.Errorf("failed to create indexer: %s", C.GoString(C.get_error_message(errInfo)))
	}

	// Index all tracks
	C.FFMS_TrackIndexSettings(indexer, -1, 1, 0)

	// Run indexing
	idx := C.FFMS_DoIndexing2(indexer, C.int(0), errInfo)
	if idx == nil {
		return nil, fmt.Errorf("failed to index: %s", C.GoString(C.get_error_message(errInfo)))
	}

	return &VidIdx{ptr: idx, videoPath: path}, nil
}

// Close releases the index resources.
func (v *VidIdx) Close() {
	if v.ptr != nil {
		C.FFMS_DestroyIndex(v.ptr)
		v.ptr = nil
	}
}

// GetVidInf retrieves video information from the index.
func GetVidInf(idx *VidIdx) (*VidInf, error) {
	if idx == nil || idx.ptr == nil {
		return nil, fmt.Errorf("nil index")
	}

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	// Get video track number
	trackNum := C.FFMS_GetFirstTrackOfType(idx.ptr, C.FFMS_TYPE_VIDEO, errInfo)
	if trackNum < 0 {
		return nil, fmt.Errorf("no video track found: %s", C.GoString(C.get_error_message(errInfo)))
	}

	cPath := C.CString(idx.videoPath)
	defer C.free(unsafe.Pointer(cPath))

	// Create video source to get properties
	src := C.FFMS_CreateVideoSource(cPath, C.int(trackNum), idx.ptr, 0, C.FFMS_SEEK_NORMAL, errInfo)
	if src == nil {
		return nil, fmt.Errorf("failed to create video source: %s", C.GoString(C.get_error_message(errInfo)))
	}
	defer C.FFMS_DestroyVideoSource(src)

	// Get video properties
	props := C.FFMS_GetVideoProperties(src)
	if props == nil {
		return nil, fmt.Errorf("failed to get video properties")
	}

	// Get first frame to determine pixel format
	frame := C.FFMS_GetFrame(src, 0, errInfo)
	if frame == nil {
		return nil, fmt.Errorf("failed to get first frame: %s", C.GoString(C.get_error_message(errInfo)))
	}

	inf := &VidInf{
		Width:       uint32(frame.EncodedWidth),
		Height:      uint32(frame.EncodedHeight),
		FPSNum:      uint32(props.FPSNumerator),
		FPSDen:      uint32(props.FPSDenominator),
		Frames:      int(props.NumFrames),
		PixelFormat: int(frame.ConvertedPixelFormat),
	}

	// Determine if 10-bit based on pixel format
	pixFmt := int(frame.ConvertedPixelFormat)
	inf.Is10Bit = pixFmt >= pixFmtYUV420P10LE && pixFmt <= pixFmtYUV444P10BE

	// Extract color metadata if available
	if frame.ColorPrimaries > 0 {
		cp := int32(frame.ColorPrimaries)
		inf.ColorPrimaries = &cp
	}
	// Note: FFMS2 header has typo "TransferCharateristics" (missing 'i')
	if frame.TransferCharateristics > 0 {
		tc := int32(frame.TransferCharateristics)
		inf.TransferCharacteristics = &tc
	}
	if frame.ColorSpace > 0 {
		mc := int32(frame.ColorSpace)
		inf.MatrixCoefficients = &mc
	}

	// Extract mastering display metadata (SMPTE 2086) if available
	if props.HasMasteringDisplayPrimaries != 0 && props.HasMasteringDisplayLuminance != 0 {
		// Format: G(x,y)B(x,y)R(x,y)WP(x,y)L(max,min)
		// Array indices: 0=Red, 1=Green, 2=Blue (matching SVT-AV1 expected order)
		md := fmt.Sprintf(
			"G(%.4f,%.4f)B(%.4f,%.4f)R(%.4f,%.4f)WP(%.4f,%.4f)L(%.4f,%.4f)",
			float64(props.MasteringDisplayPrimariesX[1]),
			float64(props.MasteringDisplayPrimariesY[1]),
			float64(props.MasteringDisplayPrimariesX[2]),
			float64(props.MasteringDisplayPrimariesY[2]),
			float64(props.MasteringDisplayPrimariesX[0]),
			float64(props.MasteringDisplayPrimariesY[0]),
			float64(props.MasteringDisplayWhitePointX),
			float64(props.MasteringDisplayWhitePointY),
			float64(props.MasteringDisplayMaxLuminance),
			float64(props.MasteringDisplayMinLuminance),
		)
		inf.MasteringDisplay = &md
	}

	// Extract content light level (MaxCLL, MaxFALL) if available
	if props.HasContentLightLevel != 0 {
		cl := fmt.Sprintf("%d,%d",
			props.ContentLightLevelMax,
			props.ContentLightLevelAverage,
		)
		inf.ContentLight = &cl
	}

	return inf, nil
}

// GetDecodeStrat determines the optimal decoding strategy based on video properties.
func GetDecodeStrat(idx *VidIdx, inf *VidInf, cropH, cropV uint32) (DecodeStrat, *CropCalc, error) {
	var rect *CropRect
	if cropH > 0 || cropV > 0 {
		if inf == nil {
			return B8Fast, nil, fmt.Errorf("nil video info")
		}
		if cropH >= inf.Width || cropH >= inf.Width-cropH || cropV >= inf.Height || cropV >= inf.Height-cropV {
			return B8Fast, nil, fmt.Errorf("invalid symmetric crop %dx%d for source %dx%d", cropH, cropV, inf.Width, inf.Height)
		}
		rect = &CropRect{
			X:      cropH,
			Y:      cropV,
			Width:  inf.Width - 2*cropH,
			Height: inf.Height - 2*cropV,
		}
	}
	return GetDecodeStratForRect(idx, inf, rect)
}

// GetDecodeStratForRect determines the optimal decoding strategy for an exact crop rectangle.
func GetDecodeStratForRect(_ *VidIdx, inf *VidInf, rect *CropRect) (DecodeStrat, *CropCalc, error) {
	if inf == nil {
		return B8Fast, nil, fmt.Errorf("nil video info")
	}

	hasCrop := rect != nil && (rect.X != 0 || rect.Y != 0 || rect.Width != inf.Width || rect.Height != inf.Height)
	if rect != nil {
		if err := validateCropRect(inf, *rect); err != nil {
			return B8Fast, nil, err
		}
	}

	strat := B8Fast
	if inf.Is10Bit {
		strat = B10Fast
	}
	if hasCrop {
		if inf.Is10Bit {
			strat = B10CropFast
		} else {
			strat = B8CropFast
		}
	}

	var cropCalc *CropCalc
	if hasCrop {
		cropCalc = &CropCalc{
			NewW:  rect.Width,
			NewH:  rect.Height,
			CropX: rect.X,
			CropY: rect.Y,
		}
	}

	return strat, cropCalc, nil
}

func validateCropRect(inf *VidInf, rect CropRect) error {
	if rect.Width == 0 || rect.Height == 0 {
		return fmt.Errorf("invalid crop rectangle: width and height must be non-zero")
	}
	if rect.X > inf.Width || rect.Width > inf.Width-rect.X || rect.Y > inf.Height || rect.Height > inf.Height-rect.Y {
		return fmt.Errorf("invalid crop rectangle %dx%d+%d+%d for source %dx%d", rect.Width, rect.Height, rect.X, rect.Y, inf.Width, inf.Height)
	}
	if rect.X%2 != 0 || rect.Y%2 != 0 || rect.Width%2 != 0 || rect.Height%2 != 0 {
		return fmt.Errorf("invalid crop rectangle %dx%d+%d+%d: YUV420 crop offsets and dimensions must be even", rect.Width, rect.Height, rect.X, rect.Y)
	}
	return nil
}

// ThrVidSrc creates a threaded video source from an index.
func ThrVidSrc(idx *VidIdx, threads int) (*VidSrc, error) {
	if idx == nil || idx.ptr == nil {
		return nil, fmt.Errorf("nil index")
	}

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	// Get video track number
	trackNum := C.FFMS_GetFirstTrackOfType(idx.ptr, C.FFMS_TYPE_VIDEO, errInfo)
	if trackNum < 0 {
		return nil, fmt.Errorf("no video track found: %s", C.GoString(C.get_error_message(errInfo)))
	}

	cPath := C.CString(idx.videoPath)
	defer C.free(unsafe.Pointer(cPath))

	// Create video source with threading
	src := C.FFMS_CreateVideoSource(cPath, C.int(trackNum), idx.ptr, C.int(threads), C.FFMS_SEEK_NORMAL, errInfo)
	if src == nil {
		return nil, fmt.Errorf("failed to create video source: %s", C.GoString(C.get_error_message(errInfo)))
	}

	return &VidSrc{ptr: src}, nil
}

// Close releases the video source resources.
func (v *VidSrc) Close() {
	if v.ptr != nil {
		C.FFMS_DestroyVideoSource(v.ptr)
		v.ptr = nil
	}
}

// ExtractFrame extracts a single frame from the video source.
// Output is always 10-bit YUV420 (16-bit little-endian per sample).
// 8-bit sources are converted to 10-bit by left-shifting by 2.
func ExtractFrame(src *VidSrc, frameIdx int, output []byte, inf *VidInf, strat DecodeStrat, cropCalc *CropCalc) error {
	if src == nil || src.ptr == nil {
		return fmt.Errorf("nil video source")
	}

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	// Get the frame
	frame := C.FFMS_GetFrame(src.ptr, C.int(frameIdx), errInfo)
	if frame == nil {
		return fmt.Errorf("failed to get frame %d: %s", frameIdx, C.GoString(C.get_error_message(errInfo)))
	}

	// Extract data based on strategy
	width := inf.Width
	height := inf.Height
	if cropCalc != nil {
		width = cropCalc.NewW
		height = cropCalc.NewH
	}

	// Output is always 10-bit (16 bits per sample)
	yPlaneSize := int(width) * int(height) * 2     // Y: 2 bytes per pixel
	uPlaneSize := int(width) * int(height) / 4 * 2 // U: 1/4 pixels, 2 bytes each
	vPlaneSize := int(width) * int(height) / 4 * 2 // V: 1/4 pixels, 2 bytes each

	expectedSize := yPlaneSize + uPlaneSize + vPlaneSize
	if len(output) < expectedSize {
		return fmt.Errorf("output buffer too small: need %d, got %d", expectedSize, len(output))
	}

	// Get source data pointers with nil checks
	if frame.Data[0] == nil || frame.Data[1] == nil || frame.Data[2] == nil {
		return fmt.Errorf("frame %d has nil plane data", frameIdx)
	}
	planes := [3][]byte{
		unsafe.Slice((*byte)(unsafe.Pointer(frame.Data[0])), int(frame.Linesize[0])*int(inf.Height)),
		unsafe.Slice((*byte)(unsafe.Pointer(frame.Data[1])), int(frame.Linesize[1])*int(inf.Height/2)),
		unsafe.Slice((*byte)(unsafe.Pointer(frame.Data[2])), int(frame.Linesize[2])*int(inf.Height/2)),
	}
	linesizes := [3]int{int(frame.Linesize[0]), int(frame.Linesize[1]), int(frame.Linesize[2])}

	return copyFrameTo10Bit(output[:expectedSize], planes, linesizes, inf, cropCalc)
}

func copyFrameTo10Bit(output []byte, planes [3][]byte, linesizes [3]int, inf *VidInf, cropCalc *CropCalc) error {
	width := inf.Width
	height := inf.Height
	cropX := uint32(0)
	cropY := uint32(0)
	if cropCalc != nil {
		width = cropCalc.NewW
		height = cropCalc.NewH
		cropX = cropCalc.CropX
		cropY = cropCalc.CropY
	}

	yPlaneSize := int(width) * int(height) * 2
	uPlaneSize := int(width) * int(height) / 4 * 2
	vPlaneSize := int(width) * int(height) / 4 * 2
	expectedSize := yPlaneSize + uPlaneSize + vPlaneSize
	if len(output) < expectedSize {
		return fmt.Errorf("output buffer too small: need %d, got %d", expectedSize, len(output))
	}

	if inf.Is10Bit {
		bytesPerPixel := 2
		yStart := int(cropY)*linesizes[0] + int(cropX)*bytesPerPixel
		uStart := int(cropY/2)*linesizes[1] + int(cropX/2)*bytesPerPixel
		vStart := int(cropY/2)*linesizes[2] + int(cropX/2)*bytesPerPixel
		yRowLen := int(width) * bytesPerPixel
		uvRowLen := int(width/2) * bytesPerPixel

		if err := validatePlaneBounds("Y", planes[0], yStart, int(height), yRowLen, linesizes[0]); err != nil {
			return err
		}
		if err := validatePlaneBounds("U", planes[1], uStart, int(height/2), uvRowLen, linesizes[1]); err != nil {
			return err
		}
		if err := validatePlaneBounds("V", planes[2], vStart, int(height/2), uvRowLen, linesizes[2]); err != nil {
			return err
		}

		copyPlane10bit(output[:yPlaneSize], planes[0][yStart:], int(height), yRowLen, linesizes[0])
		copyPlane10bit(output[yPlaneSize:yPlaneSize+uPlaneSize], planes[1][uStart:], int(height/2), uvRowLen, linesizes[1])
		copyPlane10bit(output[yPlaneSize+uPlaneSize:], planes[2][vStart:], int(height/2), uvRowLen, linesizes[2])
		return nil
	}

	// Source is 8-bit, convert to 10-bit (left shift by 2).
	yStart := int(cropY)*linesizes[0] + int(cropX)
	uStart := int(cropY/2)*linesizes[1] + int(cropX/2)
	vStart := int(cropY/2)*linesizes[2] + int(cropX/2)
	yRowLen := int(width)
	uvRowLen := int(width / 2)

	if err := validatePlaneBounds("Y", planes[0], yStart, int(height), yRowLen, linesizes[0]); err != nil {
		return err
	}
	if err := validatePlaneBounds("U", planes[1], uStart, int(height/2), uvRowLen, linesizes[1]); err != nil {
		return err
	}
	if err := validatePlaneBounds("V", planes[2], vStart, int(height/2), uvRowLen, linesizes[2]); err != nil {
		return err
	}

	convert8to10bit(output[:yPlaneSize], planes[0][yStart:], int(width), int(height), linesizes[0])
	convert8to10bit(output[yPlaneSize:yPlaneSize+uPlaneSize], planes[1][uStart:], int(width/2), int(height/2), linesizes[1])
	convert8to10bit(output[yPlaneSize+uPlaneSize:], planes[2][vStart:], int(width/2), int(height/2), linesizes[2])
	return nil
}

func validatePlaneBounds(name string, plane []byte, start, rows, rowLen, stride int) error {
	if start < 0 || rows < 0 || rowLen < 0 || stride < rowLen {
		return fmt.Errorf("invalid %s plane geometry: start=%d rows=%d rowLen=%d stride=%d", name, start, rows, rowLen, stride)
	}
	if rows == 0 {
		if start > len(plane) {
			return fmt.Errorf("%s plane crop starts beyond plane data: start=%d len=%d", name, start, len(plane))
		}
		return nil
	}
	end := start + (rows-1)*stride + rowLen
	if end > len(plane) {
		return fmt.Errorf("%s plane crop exceeds plane data: end=%d len=%d", name, end, len(plane))
	}
	return nil
}

// copyPlane10bit copies a 10-bit plane handling stride differences.
// Copies dstStride bytes per row, reading from src with srcStride spacing.
func copyPlane10bit(dst, src []byte, rows, dstStride, srcStride int) {
	srcOff := 0
	dstOff := 0
	// Copy the minimum of src available bytes and dst needed bytes
	copyLen := dstStride
	if srcStride < dstStride {
		copyLen = srcStride
	}
	for row := 0; row < rows; row++ {
		copy(dst[dstOff:dstOff+copyLen], src[srcOff:srcOff+copyLen])
		srcOff += srcStride
		dstOff += dstStride
	}
}

// convert8to10bit converts 8-bit YUV data to 10-bit by left-shifting by 2.
// Output is 16-bit little-endian per sample.
func convert8to10bit(dst, src []byte, width, height, srcStride int) {
	dstOff := 0
	for row := 0; row < height; row++ {
		srcRowStart := row * srcStride
		for col := 0; col < width; col++ {
			// Read 8-bit sample and convert to 10-bit (left shift by 2)
			sample8 := uint16(src[srcRowStart+col])
			sample10 := sample8 << 2

			binary.LittleEndian.PutUint16(dst[dstOff:], sample10)
			dstOff += 2
		}
	}
}

// CalcPackedSize calculates the buffer size for 10-bit packed YUV420 format.
func CalcPackedSize(w, h uint32) int {
	// YUV420 10-bit: Y = w*h*2, U = w*h/4*2, V = w*h/4*2
	return int(w) * int(h) * 3 // 2 bytes per Y + 0.5 bytes per U + 0.5 bytes per V = 3 bytes total per pixel pair
}

// Calc8BitSize calculates the buffer size for 8-bit YUV420 format.
func Calc8BitSize(w, h uint32) int {
	// YUV420 8-bit: Y = w*h, U = w*h/4, V = w*h/4
	return int(w) * int(h) * 3 / 2
}

// CalcFrameSize returns the buffer size needed for a frame given video info.
// Always returns 10-bit size since we convert 8-bit sources to 10-bit for encoding.
func CalcFrameSize(inf *VidInf, cropCalc *CropCalc) int {
	w := inf.Width
	h := inf.Height
	if cropCalc != nil {
		w = cropCalc.NewW
		h = cropCalc.NewH
	}

	// Always use 10-bit size - 8-bit sources are converted to 10-bit
	return CalcPackedSize(w, h)
}

// LumaFrame is a borrowed view of a decoded frame's Y plane.
// Data remains valid until the next frame request on the same VidSrc or until the VidSrc is closed.
type LumaFrame struct {
	Data    []byte
	Stride  int
	Width   int
	Height  int
	Is10Bit bool
}

// Frame represents a decoded video frame with plane pointers.
type Frame struct {
	Data     [3]unsafe.Pointer // Y, U, V plane pointers
	Linesize [3]int            // Stride for each plane
}

// ExtractLumaFrame retrieves a borrowed view of a frame's luma plane.
func ExtractLumaFrame(src *VidSrc, frameIdx int, inf *VidInf) (*LumaFrame, error) {
	if src == nil || src.ptr == nil {
		return nil, fmt.Errorf("nil video source")
	}
	if inf == nil {
		return nil, fmt.Errorf("nil video info")
	}

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	frame := C.FFMS_GetFrame(src.ptr, C.int(frameIdx), errInfo)
	if frame == nil {
		return nil, fmt.Errorf("failed to get frame %d: %s", frameIdx, C.GoString(C.get_error_message(errInfo)))
	}
	if frame.Data[0] == nil {
		return nil, fmt.Errorf("frame %d has nil luma data", frameIdx)
	}

	stride := int(frame.Linesize[0])
	height := int(inf.Height)
	if stride <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid luma geometry for frame %d: stride=%d height=%d", frameIdx, stride, height)
	}

	return &LumaFrame{
		Data:    unsafe.Slice((*byte)(unsafe.Pointer(frame.Data[0])), stride*height),
		Stride:  stride,
		Width:   int(inf.Width),
		Height:  height,
		Is10Bit: inf.Is10Bit,
	}, nil
}

// GetFrame retrieves a single frame from the video source.
// Returns a Frame struct with plane pointers and strides.
func GetFrame(src *VidSrc, frameIdx int) (*Frame, error) {
	if src == nil || src.ptr == nil {
		return nil, fmt.Errorf("nil video source")
	}

	errInfo := C.create_error_info()
	defer C.free_error_info(errInfo)

	frame := C.FFMS_GetFrame(src.ptr, C.int(frameIdx), errInfo)
	if frame == nil {
		return nil, fmt.Errorf("failed to get frame %d: %s", frameIdx, C.GoString(C.get_error_message(errInfo)))
	}

	return &Frame{
		Data: [3]unsafe.Pointer{
			unsafe.Pointer(frame.Data[0]),
			unsafe.Pointer(frame.Data[1]),
			unsafe.Pointer(frame.Data[2]),
		},
		Linesize: [3]int{
			int(frame.Linesize[0]),
			int(frame.Linesize[1]),
			int(frame.Linesize[2]),
		},
	}, nil
}
