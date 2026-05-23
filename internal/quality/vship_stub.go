//go:build no_vship || !cgo

package quality

import (
	"fmt"

	"codeberg.org/five82/reel/internal/video"
)

// VshipProcessor is a CVVDP scorer backed by VSHIP. This stub is used for
// builds that explicitly disable VSHIP with -tags no_vship, or when cgo is off.
type VshipProcessor struct{}

func VshipBuildEnabled() bool { return false }

func NewVshipProcessor(_, _ uint32, _ *video.Info, _ string) (*VshipProcessor, error) {
	return nil, fmt.Errorf("VSHIP/CVVDP support is not enabled in this build; rebuild Reel without -tags no_vship and with libvship installed")
}

func (p *VshipProcessor) Close() error { return nil }

func (p *VshipProcessor) ResetCVVDP() error {
	return fmt.Errorf("VSHIP/CVVDP support is not enabled")
}

func (p *VshipProcessor) ComputeCVVDP(_, _ FramePlanes) (float32, error) {
	return 0, fmt.Errorf("VSHIP/CVVDP support is not enabled")
}
