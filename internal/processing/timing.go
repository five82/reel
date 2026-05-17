package processing

import (
	"fmt"
	"time"

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
