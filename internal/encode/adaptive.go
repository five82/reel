package encode

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/five82/reel/internal/util"
)

const (
	memoryMonitorInterval = 5 * time.Second
	abundantRampIntervals = 2 // 10 seconds
	stableRampIntervals   = 6 // 30 seconds

	memoryCriticalAvailableFraction = 0.08
	memoryPressureAvailableFraction = 0.20
	memoryStableAvailableFraction   = 0.35
	memoryAbundantAvailableFraction = 0.50

	swapStableGrowthBytes   = 16 << 20
	swapPressureGrowthBytes = 64 << 20
	swapCriticalGrowthBytes = 1 << 30
)

// ErrMemoryPressure is returned when Reel cancels encoding to avoid system OOM.
var ErrMemoryPressure = errors.New("memory pressure critical; canceled before swap exhaustion")

type statusCallback func(string)

type adaptiveLimiter struct {
	mu     sync.Mutex
	cond   *sync.Cond
	min    int
	max    int
	target int
	active int

	stableTicks int
	status      statusCallback
}

// MaxAdaptiveWorkers returns the hardware-derived adaptive concurrency ceiling.
func MaxAdaptiveWorkers() int {
	return max(util.LogicalCores(), 1)
}

func newAdaptiveLimiter(maxWorkers, initialWorkers int, status statusCallback) *adaptiveLimiter {
	maxWorkers = max(maxWorkers, 1)
	initialWorkers = min(max(initialWorkers, 1), maxWorkers)

	l := &adaptiveLimiter{
		min:    1,
		max:    maxWorkers,
		target: initialWorkers,
		status: status,
	}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func initialAdaptiveWorkers(maxWorkers int, width, height uint32) int {
	if maxWorkers <= 1 {
		return 1
	}
	switch {
	case width >= 3840 || height >= 2160:
		return 1
	case width >= 1920 || height >= 1080:
		return min(maxWorkers, max(3, maxWorkers/4))
	default:
		return min(maxWorkers, 4)
	}
}

func levelOfParallelismForWorkers(workers int) uint32 {
	switch {
	case workers <= 2:
		return 4
	case workers <= 5:
		return 3
	default:
		return 2
	}
}

func (l *adaptiveLimiter) acquire(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for l.active >= l.target {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.cond.Wait()
	}
	l.active++
	return nil
}

func (l *adaptiveLimiter) release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	l.cond.Broadcast()
	l.mu.Unlock()
}

func (l *adaptiveLimiter) wake() {
	l.mu.Lock()
	l.cond.Broadcast()
	l.mu.Unlock()
}

func (l *adaptiveLimiter) stats() (active, target, maxWorkers int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active, l.target, l.max
}

func (l *adaptiveLimiter) monitor(ctx context.Context, cancel context.CancelFunc, setError func(error)) {
	stats, ok := util.ReadMemoryStats()
	if !ok || stats.MemTotal == 0 {
		return
	}

	baselineSwapUsed := stats.SwapUsed()
	lastSwapUsed := baselineSwapUsed
	ticker := time.NewTicker(memoryMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.wake()
			return
		case <-ticker.C:
		}

		stats, ok := util.ReadMemoryStats()
		if !ok || stats.MemTotal == 0 {
			continue
		}

		availableFraction := float64(stats.MemAvailable) / float64(stats.MemTotal)
		currentSwapUsed := stats.SwapUsed()
		swapGrowthTotal := positiveDelta(currentSwapUsed, baselineSwapUsed)
		swapGrowthInterval := positiveDelta(currentSwapUsed, lastSwapUsed)
		lastSwapUsed = currentSwapUsed

		if l.criticalPressure(availableFraction, swapGrowthTotal, stats) {
			setError(ErrMemoryPressure)
			l.statusf("Memory pressure is critical; canceling encode before swap exhaustion")
			cancel()
			l.wake()
			return
		}

		if l.hasPressure(availableFraction, swapGrowthInterval) {
			l.reduceTarget(availableFraction, swapGrowthInterval)
			continue
		}

		switch {
		case availableFraction > memoryAbundantAvailableFraction && swapGrowthTotal == 0:
			l.maybeIncreaseTarget(abundantRampIntervals)
		case availableFraction > memoryStableAvailableFraction && swapGrowthTotal <= swapStableGrowthBytes:
			l.maybeIncreaseTarget(stableRampIntervals)
		default:
			l.resetStability()
		}
	}
}

func positiveDelta(current, previous uint64) uint64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func (l *adaptiveLimiter) criticalPressure(availableFraction float64, swapGrowth uint64, stats util.MemoryStats) bool {
	if availableFraction < memoryCriticalAvailableFraction {
		return true
	}
	if swapGrowth >= swapCriticalGrowthBytes {
		return true
	}
	// Swap is a performance cliff, not normal operating headroom. If Reel has
	// already consumed a meaningful fraction of swap during this encode, stop
	// before the machine spends minutes thrashing or reaches OOM.
	if stats.SwapTotal > 0 && swapGrowth > stats.SwapTotal/20 {
		return true
	}
	return false
}

func (l *adaptiveLimiter) hasPressure(availableFraction float64, swapGrowth uint64) bool {
	return availableFraction < memoryPressureAvailableFraction || swapGrowth >= swapPressureGrowthBytes
}

func (l *adaptiveLimiter) reduceTarget(availableFraction float64, swapGrowth uint64) {
	l.mu.Lock()
	old := l.target
	step := max(old/3, 1)
	if swapGrowth >= swapPressureGrowthBytes {
		step = max(step, old/2)
	}
	l.target = max(l.min, old-step)
	l.stableTicks = 0
	active := l.active
	newTarget := l.target
	l.cond.Broadcast()
	l.mu.Unlock()

	if newTarget < old {
		l.statusf("Memory pressure detected; reducing workers %d -> %d (active %d, available %.0f%%, swap +%s)",
			old, newTarget, active, availableFraction*100, util.FormatBytesReadable(swapGrowth))
	}
}

func (l *adaptiveLimiter) maybeIncreaseTarget(requiredStableTicks int) {
	l.mu.Lock()
	if l.target >= l.max {
		l.stableTicks = 0
		l.mu.Unlock()
		return
	}

	l.stableTicks++
	if l.stableTicks < requiredStableTicks {
		l.mu.Unlock()
		return
	}

	old := l.target
	l.target++
	l.stableTicks = 0
	newTarget := l.target
	l.cond.Broadcast()
	l.mu.Unlock()

	l.statusf("Memory stable; increasing workers %d -> %d", old, newTarget)
}

func (l *adaptiveLimiter) resetStability() {
	l.mu.Lock()
	l.stableTicks = 0
	l.mu.Unlock()
}

func (l *adaptiveLimiter) statusf(format string, args ...any) {
	if l.status != nil {
		l.status(fmt.Sprintf(format, args...))
	}
}
