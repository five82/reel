package encode

import (
	"testing"

	"codeberg.org/five82/reel/internal/util"
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

func TestAdaptiveLimiterTestsRampBeforeIncreasingAgain(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, nil)
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
		limiter.maybeAdjustTarget(true, 103)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target before ramp test completes = %d, want 2", target)
	}

	limiter.maybeAdjustTarget(true, 103)
	_, target, _ = limiter.stats()
	if target != 3 {
		t.Fatalf("target after successful ramp test = %d, want 3", target)
	}
}

func TestAdaptiveLimiterBacksOffOnThroughputPlateau(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, nil)
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
		limiter.maybeAdjustTarget(true, 101)
	}
	_, target, _ = limiter.stats()
	if target != 1 {
		t.Fatalf("target after plateau = %d, want 1", target)
	}

	limiter.active = 1
	for range rampEvaluationTicks {
		limiter.maybeAdjustTarget(true, 120)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after materially faster plateau = %d, want 2", target)
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
