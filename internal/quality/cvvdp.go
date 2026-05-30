package quality

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/video"
)

type FramePlanes struct {
	Planes  [3]*byte
	Strides [3]int64
}

type CVVDPOptions struct {
	SourcePath      string
	ProbePath       string
	ProbeStartFrame int
	Info            *video.Info
	Chunk           chunk.Chunk
	CropRect        *video.CropRect
	Width           uint32
	Height          uint32
	Processor       *VshipProcessor
}

type CVVDPResult struct {
	Score         float32
	Frames        int
	MetricSeconds float64
}

func ComputeChunkCVVDP(ctx context.Context, opts CVVDPOptions) (CVVDPResult, error) {
	if opts.Processor == nil {
		return CVVDPResult{}, fmt.Errorf("nil VSHIP processor")
	}
	if opts.Info == nil {
		return CVVDPResult{}, fmt.Errorf("nil video info")
	}
	if opts.Width == 0 || opts.Height == 0 {
		return CVVDPResult{}, fmt.Errorf("invalid CVVDP dimensions %dx%d", opts.Width, opts.Height)
	}

	src, err := video.Open(opts.SourcePath, 1)
	if err != nil {
		return CVVDPResult{}, fmt.Errorf("failed to open source for CVVDP: %w", err)
	}
	defer src.Close()

	probeInfo, err := video.Probe(opts.ProbePath)
	if err != nil {
		return CVVDPResult{}, fmt.Errorf("failed to probe encoded chunk for CVVDP: %w", err)
	}
	dist, err := video.Open(opts.ProbePath, 1)
	if err != nil {
		return CVVDPResult{}, fmt.Errorf("failed to open encoded chunk for CVVDP: %w", err)
	}
	defer dist.Close()

	if err := opts.Processor.ResetCVVDP(); err != nil {
		return CVVDPResult{}, err
	}

	frameSize := yuv420p10Size(opts.Width, opts.Height)
	srcBuf := make([]byte, frameSize)
	distBuf := make([]byte, frameSize)
	srcPlanes, err := PlanesFromYUV420P10(srcBuf, opts.Width, opts.Height)
	if err != nil {
		return CVVDPResult{}, err
	}
	distPlanes, err := PlanesFromYUV420P10(distBuf, opts.Width, opts.Height)
	if err != nil {
		return CVVDPResult{}, err
	}

	start := time.Now()
	var score float32
	for i := 0; i < opts.Chunk.Frames(); i++ {
		select {
		case <-ctx.Done():
			return CVVDPResult{}, ctx.Err()
		default:
		}

		if err := src.ReadFrame(opts.Chunk.Start+i, srcBuf, opts.Info, opts.CropRect); err != nil {
			return CVVDPResult{}, fmt.Errorf("failed to read source frame %d for CVVDP: %w", opts.Chunk.Start+i, err)
		}
		probeFrame := opts.ProbeStartFrame + i
		if err := dist.ReadFrame(probeFrame, distBuf, probeInfo, nil); err != nil {
			return CVVDPResult{}, fmt.Errorf("failed to read probe frame %d for CVVDP: %w", probeFrame, err)
		}
		score, err = opts.Processor.ComputeCVVDP(srcPlanes, distPlanes)
		if err != nil {
			return CVVDPResult{}, fmt.Errorf("CVVDP failed on frame %d: %w", i, err)
		}
	}

	return CVVDPResult{
		Score:         score,
		Frames:        opts.Chunk.Frames(),
		MetricSeconds: time.Since(start).Seconds(),
	}, nil
}

func PlanesFromYUV420P10(buf []byte, width, height uint32) (FramePlanes, error) {
	required := yuv420p10Size(width, height)
	if len(buf) < required {
		return FramePlanes{}, fmt.Errorf("YUV420P10 buffer too small: got %d, need %d", len(buf), required)
	}
	ySize := int(width * height * 2)
	uvSize := int(width * height / 2)
	return FramePlanes{
		Planes: [3]*byte{&buf[0], &buf[ySize], &buf[ySize+uvSize]},
		Strides: [3]int64{
			int64(width * 2),
			int64((width / 2) * 2),
			int64((width / 2) * 2),
		},
	}, nil
}

func yuv420p10Size(width, height uint32) int {
	return int(width * height * 3)
}
