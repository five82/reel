package encode

import "testing"

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

func TestAdaptiveLimiterRampIntervals(t *testing.T) {
	limiter := newAdaptiveLimiter(4, 1, nil)

	limiter.maybeIncreaseTarget(abundantRampIntervals)
	_, target, _ := limiter.stats()
	if target != 1 {
		t.Fatalf("target after one abundant tick = %d, want 1", target)
	}

	limiter.maybeIncreaseTarget(abundantRampIntervals)
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target after two abundant ticks = %d, want 2", target)
	}

	for range stableRampIntervals - 1 {
		limiter.maybeIncreaseTarget(stableRampIntervals)
	}
	_, target, _ = limiter.stats()
	if target != 2 {
		t.Fatalf("target before stable ramp completes = %d, want 2", target)
	}

	limiter.maybeIncreaseTarget(stableRampIntervals)
	_, target, _ = limiter.stats()
	if target != 3 {
		t.Fatalf("target after stable ramp completes = %d, want 3", target)
	}
}
