package encode

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"codeberg.org/five82/reel/internal/util"
)

const (
	memoryMonitorInterval  = 5 * time.Second
	rampEvaluationTicks    = 18 // 90 seconds
	pressureCooldownTicks  = 12 // 60 seconds
	rampRetryCooldownTicks = 36 // 3 minutes
	plateauCooldownTicks   = 72 // 6 minutes

	memoryCriticalAvailableFraction = 0.08
	memoryPressureAvailableFraction = 0.20
	memoryStableAvailableFraction   = 0.35

	minSpeedGainFraction = 0.06
	speedSmoothing       = 0.50

	swapStableGrowthBytes          = 16 << 20
	swapRampTotalGrowthFloorBytes  = 512 << 20
	swapRampTotalGrowthDenominator = 100 // 1% of configured swap
	swapPressureGrowthBytes        = 64 << 20
	swapCriticalGrowthBytes        = 1 << 30
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

	observedFrames      int
	totalFrames         int
	recentSpeed         float64
	evaluatingRamp      bool
	rampBaselineSpeed   float64
	rampBlocked         bool
	blockedTarget       int
	cooldownTicks       int
	plateauCooldownLeft int
}

// MaxAdaptiveWorkers returns the hardware-derived adaptive concurrency ceiling.
func MaxAdaptiveWorkers() int {
	return max(util.LogicalCores(), 1)
}

func newAdaptiveLimiter(maxWorkers, initialWorkers, totalFrames int, status statusCallback) *adaptiveLimiter {
	maxWorkers = max(maxWorkers, 1)
	initialWorkers = min(max(initialWorkers, 1), maxWorkers)

	l := &adaptiveLimiter{
		min:         1,
		max:         maxWorkers,
		target:      initialWorkers,
		totalFrames: max(totalFrames, 0),
		status:      status,
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
		return min(maxWorkers, 2)
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

func (l *adaptiveLimiter) acquire(ctx context.Context) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for l.active >= l.target {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		l.cond.Wait()
	}
	l.active++
	return l.active, nil
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

func (l *adaptiveLimiter) observeProgress(frames int) {
	l.mu.Lock()
	if frames > l.observedFrames {
		l.observedFrames = frames
	}
	l.mu.Unlock()
}

func (l *adaptiveLimiter) progressFrames() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.observedFrames
}

func (l *adaptiveLimiter) rampDisabledLate() bool {
	return l.totalFrames > 0 && float64(l.observedFrames)/float64(l.totalFrames) >= 0.80
}

func (l *adaptiveLimiter) nextTargetBlocked() bool {
	return l.blockedTarget > 0 && l.target+1 >= l.blockedTarget
}

func (l *adaptiveLimiter) updateRecentSpeed(framesDelta int, elapsedSeconds float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	if framesDelta <= 0 || elapsedSeconds <= 0 {
		return l.recentSpeed
	}

	intervalSpeed := float64(framesDelta) / elapsedSeconds
	if l.recentSpeed == 0 {
		l.recentSpeed = intervalSpeed
	} else {
		l.recentSpeed = l.recentSpeed*(1-speedSmoothing) + intervalSpeed*speedSmoothing
	}
	return l.recentSpeed
}

func (l *adaptiveLimiter) monitor(ctx context.Context, cancel context.CancelFunc, setError func(error)) {
	stats, ok := util.ReadMemoryStats()
	if !ok || stats.MemTotal == 0 {
		return
	}

	baselineSwapUsed := stats.SwapUsed()
	lastSwapUsed := baselineSwapUsed
	lastFrames := l.progressFrames()
	lastSpeedAt := time.Now()
	ticker := time.NewTicker(memoryMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.wake()
			return
		case <-ticker.C:
		}

		now := time.Now()
		currentFrames := l.progressFrames()
		recentSpeed := l.updateRecentSpeed(currentFrames-lastFrames, now.Sub(lastSpeedAt).Seconds())
		lastFrames = currentFrames
		lastSpeedAt = now

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

		swapGrowthStable := swapGrowthStableForRamp(stats, swapGrowthTotal, swapGrowthInterval)
		memoryStable := availableFraction > memoryStableAvailableFraction && swapGrowthStable
		l.maybeAdjustTarget(memoryStable, recentSpeed)
	}
}

func positiveDelta(current, previous uint64) uint64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func swapGrowthStableForRamp(stats util.MemoryStats, totalGrowth, intervalGrowth uint64) bool {
	return totalGrowth <= swapRampTotalGrowthLimit(stats) && intervalGrowth <= swapStableGrowthBytes
}

func swapRampTotalGrowthLimit(stats util.MemoryStats) uint64 {
	limit := uint64(swapRampTotalGrowthFloorBytes)
	if stats.SwapTotal == 0 {
		return limit
	}
	return max(limit, stats.SwapTotal/swapRampTotalGrowthDenominator)
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
	l.evaluatingRamp = false
	l.rampBlocked = false
	l.cooldownTicks = pressureCooldownTicks
	l.plateauCooldownLeft = 0
	active := l.active
	newTarget := l.target
	l.cond.Broadcast()
	l.mu.Unlock()

	if newTarget < old {
		l.statusf("Memory pressure detected; reducing workers %d -> %d (active %d, available %.0f%%, swap +%s)",
			old, newTarget, active, availableFraction*100, util.FormatBytesReadable(swapGrowth))
	}
}

func (l *adaptiveLimiter) maybeAdjustTarget(memoryStable bool, recentSpeed float64) {
	l.mu.Lock()
	if !memoryStable || recentSpeed <= 0 {
		l.stableTicks = 0
		l.mu.Unlock()
		return
	}
	if l.active != l.target {
		l.stableTicks = 0
		l.mu.Unlock()
		return
	}
	if l.cooldownTicks > 0 {
		l.cooldownTicks--
		l.stableTicks = 0
		l.mu.Unlock()
		return
	}
	if l.rampBlocked {
		if l.plateauCooldownLeft > 0 {
			l.plateauCooldownLeft--
			l.stableTicks = 0
			l.mu.Unlock()
			return
		}
		l.rampBlocked = false
	}

	l.stableTicks++
	if l.stableTicks < rampEvaluationTicks {
		l.mu.Unlock()
		return
	}
	l.stableTicks = 0

	if l.evaluatingRamp {
		old := l.target
		baseline := l.rampBaselineSpeed
		clearGainThreshold := baseline * (1 + minSpeedGainFraction)
		switch {
		case recentSpeed <= baseline:
			l.evaluatingRamp = false
			l.rampBlocked = true
			l.blockedTarget = old + 1
			l.plateauCooldownLeft = plateauCooldownTicks
			l.mu.Unlock()

			l.statusf("Throughput did not improve; holding at %d workers and skipping higher worker tests", old)
			return
		case recentSpeed < clearGainThreshold:
			l.evaluatingRamp = false
			l.rampBlocked = true
			l.plateauCooldownLeft = rampRetryCooldownTicks
			l.mu.Unlock()

			l.statusf("Throughput gain was modest; holding at %d workers", old)
			return
		default:
			l.evaluatingRamp = false
			l.mu.Unlock()

			l.statusf("Throughput improved; keeping %d workers", old)
			return
		}
	}

	if l.target >= l.max || l.rampDisabledLate() || l.nextTargetBlocked() {
		l.mu.Unlock()
		return
	}

	old := l.target
	l.rampBaselineSpeed = recentSpeed
	l.target++
	l.evaluatingRamp = true
	newTarget := l.target
	l.cond.Broadcast()
	l.mu.Unlock()

	l.statusf("Throughput stable; testing workers %d -> %d", old, newTarget)
}

func (l *adaptiveLimiter) statusf(format string, args ...any) {
	if l.status != nil {
		l.status(fmt.Sprintf(format, args...))
	}
}
