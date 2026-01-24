package encode

import "github.com/five82/reel/internal/util"

// Estimated memory per worker by resolution (bytes).
// Based on real-world SVT-AV1 measurements.
const (
	MemPerWorker4K    = 5 << 30   // 5 GB
	MemPerWorker1080p = 2 << 30   // 2 GB
	MemPerWorkerSD    = 512 << 20 // 512 MB
)

// MemoryFraction is the fraction of available memory to use for workers.
// 70% leaves headroom for OS, file cache, and other processes.
const MemoryFraction = 0.7

// CapWorkers returns the safe number of workers based on available memory.
// Returns (actualWorkers, wasCapped).
func CapWorkers(requested int, width, height uint32) (int, bool) {
	memPerWorker := memoryPerWorker(width, height)

	maxByMemory := requested // default if we can't determine memory
	if available := util.AvailableMemoryBytes(); available > 0 {
		usable := uint64(float64(available) * MemoryFraction)
		maxByMemory = max(int(usable/memPerWorker), 1)
	}

	if requested > maxByMemory {
		return maxByMemory, true
	}
	return requested, false
}

// memoryPerWorker returns estimated memory usage per worker based on resolution.
func memoryPerWorker(width, height uint32) uint64 {
	switch {
	case width >= 3840 || height >= 2160:
		return MemPerWorker4K
	case width >= 1920 || height >= 1080:
		return MemPerWorker1080p
	default:
		return MemPerWorkerSD
	}
}

// CalculatePermits returns the number of in-flight chunk permits.
// Permits = workers + buffer to allow prefetching chunks.
// Returns at least 1.
func CalculatePermits(workers, buffer int) int {
	return max(workers+buffer, 1)
}
