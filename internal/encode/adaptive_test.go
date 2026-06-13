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
		available  uint64
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
			name:       "4k without memory reading starts at bandwidth ceiling",
			maxWorkers: 32,
			width:      3840,
			height:     2160,
			available:  0, // ceiling 32/6 = 5
			want:       5,
		},
		{
			name:       "4k with abundant memory starts at bandwidth ceiling",
			maxWorkers: 32,
			width:      3840,
			height:     2160,
			available:  60 << 30, // mem allows 10, ceiling 5 wins
			want:       5,
		},
		{
			name:       "4k memory-limited below ceiling",
			maxWorkers: 32,
			width:      3840,
			height:     2160,
			available:  18 << 30, // 0.5*18/3 = 3 < ceiling 5
			want:       3,
		},
		{
			name:       "4k never starts below floor",
			maxWorkers: 32,
			width:      3840,
			height:     2160,
			available:  8 << 30, // mem allows 1, floor 3 wins
			want:       3,
		},
		{
			name:       "4k small machine ceiling and floor coincide",
			maxWorkers: 8,
			width:      3840,
			height:     2160,
			available:  120 << 30, // ceiling max(3, 8/6)=3
			want:       3,
		},
		{
			name:       "hd ignores memory and uses quarter of large machines",
			maxWorkers: 32,
			width:      1920,
			height:     1080,
			available:  60 << 30,
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
			got := initialAdaptiveWorkers(tt.maxWorkers, tt.width, tt.height, tt.available)
			if got != tt.want {
				t.Fatalf("initialAdaptiveWorkers() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestShouldReopenSource(t *testing.T) {
	tests := []struct {
		name       string
		nextFrame  int
		chunkStart int
		want       bool
	}{
		{
			name:       "keeps source for forward chunks",
			nextFrame:  1000,
			chunkStart: 1200,
			want:       false,
		},
		{
			name:       "keeps source for contiguous chunks",
			nextFrame:  1000,
			chunkStart: 1000,
			want:       false,
		},
		{
			name:       "reopens before backward chunks",
			nextFrame:  12759,
			chunkStart: 5498,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldReopenSource(tt.nextFrame, tt.chunkStart)
			if got != tt.want {
				t.Fatalf("shouldReopenSource() = %t, want %t", got, tt.want)
			}
		})
	}
}

// sampleUtilization feeds n monitor ticks at the given active level, then runs
// one ramp decision, mirroring the monitor loop (recordUtilization per tick,
// maybeRampUp once a window has accumulated).
func sampleUtilization(l *adaptiveLimiter, active, ticks int, memoryStable bool) {
	for range ticks {
		l.active = active
		l.recordUtilization()
		l.maybeRampUp(memoryStable)
	}
}

func TestAdaptiveLimiterRampsUpWhenSlotsSaturated(t *testing.T) {
	limiter := newAdaptiveLimiter(8, 2, 8, 0, nil)

	// Slots fully utilized for a full window: expect a ramp.
	sampleUtilization(limiter, 2, rampWindowTicks, true)
	_, target, _ := limiter.stats()
	if target != 3 {
		t.Fatalf("target after saturated window = %d, want 3", target)
	}
}

func TestAdaptiveLimiterDoesNotRampWhenSlotsIdle(t *testing.T) {
	limiter := newAdaptiveLimiter(8, 4, 8, 0, nil)

	// Only half the slots used: utilization 0.5 < threshold, no ramp.
	sampleUtilization(limiter, 2, rampWindowTicks*3, true)
	_, target, _ := limiter.stats()
	if target != 4 {
		t.Fatalf("target with idle slots = %d, want 4 (no ramp)", target)
	}
}

func TestAdaptiveLimiterDoesNotRampUnderMemoryPressure(t *testing.T) {
	limiter := newAdaptiveLimiter(8, 4, 8, 0, nil)

	// Saturated slots but memory not stable: no ramp.
	sampleUtilization(limiter, 4, rampWindowTicks*2, false)
	_, target, _ := limiter.stats()
	if target != 4 {
		t.Fatalf("target with unstable memory = %d, want 4 (no ramp)", target)
	}
}

func TestAdaptiveLimiterRampStepGrowsWithTarget(t *testing.T) {
	limiter := newAdaptiveLimiter(64, 8, 64, 0, nil)

	// At target 8, step is max(1, 8/4) = 2 -> 10.
	sampleUtilization(limiter, 8, rampWindowTicks, true)
	_, target, _ := limiter.stats()
	if target != 10 {
		t.Fatalf("target after one saturated window = %d, want 10", target)
	}
}

func TestAdaptiveLimiterHoldsCooldownAfterPressure(t *testing.T) {
	limiter := newAdaptiveLimiter(8, 6, 8, 0, nil)
	limiter.reduceTarget(0.15, swapPressureGrowthBytes)
	reduced := func() int { _, target, _ := limiter.stats(); return target }()
	if reduced >= 6 {
		t.Fatalf("reduceTarget did not lower target: %d", reduced)
	}

	// During the cooldown, saturated slots must not immediately ramp back up.
	sampleUtilization(limiter, reduced, pressureCooldownTicks-1, true)
	if got := func() int { _, target, _ := limiter.stats(); return target }(); got != reduced {
		t.Fatalf("target ramped during cooldown = %d, want %d", got, reduced)
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
	limiter := newAdaptiveLimiter(4, 2, 4, 100, nil)
	limiter.observeProgress(80) // 80% complete -> late-encode ramp guard

	sampleUtilization(limiter, 2, rampWindowTicks*2, true)

	_, target, _ := limiter.stats()
	if target != 2 {
		t.Fatalf("target after late saturated window = %d, want 2", target)
	}
}
