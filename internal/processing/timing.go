package processing

import (
	"fmt"
	"time"

	"codeberg.org/five82/reel/internal/perf"
	"codeberg.org/five82/reel/internal/reporter"
)

func startVerboseStep(rep reporter.Reporter, name string) func() {
	start := time.Now()
	rep.Verbose(fmt.Sprintf("%s started at %s", name, start.Format(time.RFC3339)))
	return func() {
		stop := time.Now()
		rep.Verbose(fmt.Sprintf("%s stopped at %s (duration %s)", name, stop.Format(time.RFC3339), stop.Sub(start).Round(time.Millisecond)))
	}
}

// startPhase times a pipeline phase: it emits the same verbose start/stop lines
// as startVerboseStep and, when the returned stop func runs, records the phase
// wall window into the perf collector for perf.json. The collector is nil-safe,
// so callers without one still get verbose logging.
func startPhase(c *perf.Collector, rep reporter.Reporter, name string) func() {
	start := time.Now()
	rep.Verbose(fmt.Sprintf("%s started at %s", name, start.Format(time.RFC3339)))
	return func() {
		stop := time.Now()
		rep.Verbose(fmt.Sprintf("%s stopped at %s (duration %s)", name, stop.Format(time.RFC3339), stop.Sub(start).Round(time.Millisecond)))
		c.RecordPhase(name, start, stop)
	}
}
