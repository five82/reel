package quality

import (
	"context"
	"fmt"
	"sync"
	"time"

	"codeberg.org/five82/reel/internal/chunk"
	"codeberg.org/five82/reel/internal/video"
)

// gpuMu serializes GPU (VSHIP/CUDA) scoring. Concurrent CVVDP compute on a
// single GPU corrupts results nondeterministically: a 2026-06-16 diagnostic
// found full-chunk scoring is byte-identical at one metric worker but garbles
// worst-window scores at >1 worker, which then trips the floor guard and
// cascades into ~9x output-size swings (LOG "Cascade root cause"). CVVDP is a
// temporal metric accumulated per handler, so each chunk's whole reset->compute
// sequence is held under the lock as one unit (interleaving two sequences on
// the shared handler still perturbs the result). Frame *decode* runs unlocked,
// and the per-chunk producer is started before the lock is taken, so other
// workers keep decoding while one holds the GPU. This does cost throughput --
// the old (buggy) N-handler path overlapped CVVDP compute across per-handler
// CUDA streams, so serializing it slows a 4K probe-heavy encode by ~1.5-1.7x
// (measured on the preset-8 sullyhv clip; smaller share at slower presets).
// That is the price of correctness until VSHIP supports isolated handlers.
var gpuMu sync.Mutex

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

	// Serialize the GPU work as one atomic per-chunk unit. The producer above is
	// already decoding into the buffer, so other workers keep decoding while this
	// one waits for / holds the GPU. Reset must be inside the lock so no other
	// handler's compute interleaves this chunk's temporal accumulation.
	gpuMu.Lock()
	defer gpuMu.Unlock()
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
