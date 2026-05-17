package encode

import (
	"testing"

	"codeberg.org/five82/reel/internal/util"
	"codeberg.org/five82/reel/internal/video"
)

func TestInitialAdaptiveWorkers(t *testing.T) {
	tests := []struct {
		name       string
		maxWorkers int
		width      uint32
		height     uint32
		want       int
	}{
		{
			name:       "single worker",
			maxWorkers: 1,
			width:      1920,
			height:     1080,
			want:       1,
		},
		{
			name:       "4k starts conservatively",
			maxWorkers: 32,
			width:      3840,
			height:     2160,
			want:       1,
		},
		{
			name:       "hd uses quarter of large machines",
			maxWorkers: 32,
			width:      1920,
			height:     1080,
			want:       8,
		},
		{
			name:       "hd keeps small machine floor",
			maxWorkers: 8,
			width:      1920,
			height:     1080,
			want:       3,
		},
		{
			name:       "hd caps to max workers",
			maxWorkers: 2,
			width:      1920,
			height:     1080,
			want:       2,
		},
		{
			name:       "sd starts at four",
			maxWorkers: 32,
			width:      1280,
			height:     720,
			want:       4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initialAdaptiveWorkers(tt.maxWorkers, tt.width, tt.height)
			if got != tt.want {
				t.Fatalf("initialAdaptiveWorkers() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDecodeModeForWorkerSlot(t *testing.T) {
	tests := []struct {
		name       string
		decodeMode video.DecodeMode
		width      uint32
		height     uint32
		activeSlot int
		want       video.DecodeMode
	}{
		{
			name:       "software stays software",
			decodeMode: video.DecodeSoftware,
			width:      3840,
			height:     2160,
			activeSlot: 4,
			want:       video.DecodeSoftware,
		},
		{
			name:       "cuda hd stays cuda above three workers",
			decodeMode: video.DecodeCUDA,
			width:      1920,
			height:     1080,
			activeSlot: 4,
			want:       video.DecodeCUDA,
		},
		{
			name:       "cuda uhd keeps first three workers on cuda",
			decodeMode: video.DecodeCUDA,
			width:      3840,
			height:     1600,
			activeSlot: 3,
			want:       video.DecodeCUDA,
		},
		{
			name:       "cuda uhd uses software above three workers",
			decodeMode: video.DecodeCUDA,
			width:      3840,
			height:     1600,
			activeSlot: 4,
			want:       video.DecodeSoftware,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeModeForWorkerSlot(tt.decodeMode, tt.width, tt.height, tt.activeSlot)
			if got != tt.want {
				t.Fatalf("decodeModeForWorkerSlot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdaptiveLimiterTestsRampBeforeIncreasingAgain(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, 0, nil)
	limiter.active = 1

	for range rampEvaluationTicks - 1 {
		limiter.maybeAdjustTarget(true, 100)
	}
	_, target, _ := limiter.stats()
	if target != 1 {
		t.Fatalf("target before evaluation completes = %d, want 1", target)
	}

	limiter.maybeAdjustTarget(true, 100)
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after initial evaluation = %d, want 2", target)
	}

	limiter.active = 2
	for range rampEvaluationTicks - 1 {
		limiter.maybeAdjustTarget(true, 107)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target before ramp test completes = %d, want 2", target)
	}

	limiter.maybeAdjustTarget(true, 107)
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after successful ramp test = %d, want 2", target)
	}

	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 107)
	}
	_, target, _ = limiter.stats()
	if target != 3 {
		t.Fatalf("target after next stable window = %d, want 3", target)
	}
}

func TestAdaptiveLimiterRetriesAfterMarginalRamp(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, 0, nil)
	limiter.active = 1

	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 100)
	}
	_, target, _ := limiter.stats()
	if target != 2 {
		t.Fatalf("target after initial evaluation = %d, want 2", target)
	}

	limiter.active = 2
	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 103)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after marginal ramp = %d, want 2", target)
	}

	for range rampRetryCooldownTicks {
		limiter.maybeAdjustTarget(true, 120)
	}
	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 120)
	}
	_, target, _ = limiter.stats()
	if target != 3 {
		t.Fatalf("target after marginal-ramp cooldown = %d, want 3", target)
	}
}

func TestAdaptiveLimiterHoldsAndBlocksAfterThroughputDrop(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, 0, nil)
	limiter.active = 1

	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 100)
	}
	_, target, _ := limiter.stats()
	if target != 2 {
		t.Fatalf("target after initial evaluation = %d, want 2", target)
	}

	limiter.active = 2
	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 97)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after failed ramp = %d, want 2", target)
	}

	limiter.active = 2
	for range plateauCooldownTicks {
		limiter.maybeAdjustTarget(true, 120)
	}
	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 120)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after blocked-ramp cooldown = %d, want 2", target)
	}
}

func TestSwapGrowthStableForRamp(t *testing.T) {
	stats := util.MemoryStats{SwapTotal: 64 << 30}

	if !swapGrowthStableForRamp(stats, 150<<20, 1<<20) {
		t.Fatal("expected small swap growth on a large swap device to allow ramping")
	}

	if swapGrowthStableForRamp(stats, 150<<20, swapStableGrowthBytes+1) {
		t.Fatal("expected active swap growth to block ramping")
	}

	if swapGrowthStableForRamp(stats, swapRampTotalGrowthLimit(stats)+1, 0) {
		t.Fatal("expected excessive total swap growth to block ramping")
	}
}

func TestAdaptiveLimiterDoesNotRampLateInEncode(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, 100, nil)
	limiter.active = 1
	limiter.observeProgress(80)

	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 100)
	}

	_, target, _ := limiter.stats()
	if target != 1 {
		t.Fatalf("target after late stable window = %d, want 1", target)
	}
}
