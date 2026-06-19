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

	// Decode runs in a producer goroutine so CPU frame decode overlaps the GPU
	// metric compute; two buffer pairs rotate between the producer and the
	// consumer. All GPU calls stay on this goroutine.
	ctx, cancelDecode := context.WithCancel(ctx)
	defer cancelDecode()

	type framePair struct {
		srcBuf, distBuf       []byte
		srcPlanes, distPlanes FramePlanes
	}
	frameSize := yuv420p10Size(opts.Width, opts.Height)
	freeCh := make(chan *framePair, 2)
	for i := 0; i < 2; i++ {
		pair := &framePair{
			srcBuf:  make([]byte, frameSize),
			distBuf: make([]byte, frameSize),
		}
		if pair.srcPlanes, err = PlanesFromYUV420P10(pair.srcBuf, opts.Width, opts.Height); err != nil {
			return CVVDPResult{}, err
		}
		if pair.distPlanes, err = PlanesFromYUV420P10(pair.distBuf, opts.Width, opts.Height); err != nil {
			return CVVDPResult{}, err
		}
		freeCh <- pair
	}

	type decodedFrame struct {
		pair *framePair
		err  error
	}
	decodedCh := make(chan decodedFrame, 2)
	// On early return, stop the producer and wait for it to exit before the
	// deferred decoder Closes run; the producer still holds src/dist.
	defer func() {
		cancelDecode()
		for range decodedCh { //nolint:revive // drain until the producer closes the channel
		}
	}()
	go func() {
		defer close(decodedCh)
		for i := 0; i < opts.Chunk.Frames(); i++ {
			var pair *framePair
			select {
			case pair = <-freeCh:
			case <-ctx.Done():
				decodedCh <- decodedFrame{err: ctx.Err()}
				return
			}
			if err := src.ReadFrame(opts.Chunk.Start+i, pair.srcBuf, opts.Info, opts.CropRect); err != nil {
				decodedCh <- decodedFrame{err: fmt.Errorf("failed to read source frame %d for CVVDP: %w", opts.Chunk.Start+i, err)}
				return
			}
			probeFrame := opts.ProbeStartFrame + i
			if err := dist.ReadFrame(probeFrame, pair.distBuf, probeInfo, nil); err != nil {
				decodedCh <- decodedFrame{err: fmt.Errorf("failed to read probe frame %d for CVVDP: %w", probeFrame, err)}
				return
			}
			decodedCh <- decodedFrame{pair: pair}
		}
	}()

	// Each metric worker owns a DISTINCT VshipProcessor, so this whole
	// Reset->Compute sequence runs concurrently with other workers' handlers and
	// needs no locking. CORRECTNESS DEPENDS on libvship being built with
	// MITIGATE_MALLOC_ASYNC: the default cudaMallocAsync allocator races across
	// coexisting handlers and silently corrupts scores ~50% of the time (the
	// build script enforces the flag; see docs/PERFORMANCE_TESTING_LOG.md).
	if err := opts.Processor.ResetCVVDP(); err != nil {
		return CVVDPResult{}, err
	}

	start := time.Now()
	var score float32
	computed := 0
	for decoded := range decodedCh {
		if decoded.err != nil {
			return CVVDPResult{}, decoded.err
		}
		score, err = opts.Processor.ComputeCVVDP(decoded.pair.srcPlanes, decoded.pair.distPlanes)
		if err != nil {
			return CVVDPResult{}, fmt.Errorf("CVVDP failed on frame %d: %w", computed, err)
		}
		computed++
		freeCh <- decoded.pair
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
