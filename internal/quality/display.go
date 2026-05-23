package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/five82/reel/internal/video"
)

type displayModel struct {
	Name                  string  `json:"name"`
	Colorspace            string  `json:"colorspace,omitempty"`
	Resolution            [2]int  `json:"resolution"`
	MaxLuminance          float64 `json:"max_luminance"`
	ViewingDistanceMeters float64 `json:"viewing_distance_meters"`
	DiagonalSizeInches    float64 `json:"diagonal_size_inches"`
	Contrast              float64 `json:"contrast"`
	EAmbient              float64 `json:"E_ambient"`
	KRefl                 float64 `json:"k_refl"`
	Exposure              float64 `json:"exposure,omitempty"`
	Source                string  `json:"source,omitempty"`
}

func EnsureDisplayModel(workDir string, inf *video.Info, overridePath string) (string, error) {
	if overridePath != "" {
		if _, err := os.Stat(overridePath); err != nil {
			return "", fmt.Errorf("CVVDP display JSON is not readable: %w", err)
		}
		return overridePath, nil
	}

	path := filepath.Join(workDir, "cvvdp-display.json")
	model := defaultDisplayModel(inf)
	data, err := json.MarshalIndent(map[string]displayModel{"xav": model}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode default CVVDP display model: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write default CVVDP display model: %w", err)
	}
	return path, nil
}

func defaultDisplayModel(inf *video.Info) displayModel {
	model := displayModel{
		Name:                  "Reel default normal-viewing SDR display",
		Colorspace:            "sRGB/SDR",
		Resolution:            [2]int{3840, 2160},
		MaxLuminance:          200,
		ViewingDistanceMeters: 1.3,
		DiagonalSizeInches:    55,
		Contrast:              1000,
		EAmbient:              100,
		KRefl:                 0.005,
		Source:                "reel default",
	}
	if isHDR(inf) {
		model.Name = "Reel default normal-viewing HDR display"
		model.Colorspace = "HDR"
		if inf != nil && inf.TransferCharacteristics != nil {
			switch *inf.TransferCharacteristics {
			case 16:
				model.Colorspace = "BT.2020-PQ"
			case 18:
				model.Colorspace = "BT.2020-HLG"
			}
		}
		model.MaxLuminance = 1000
		model.Contrast = 1000000
		model.EAmbient = 10
	}
	return model
}

func isHDR(inf *video.Info) bool {
	if inf == nil || inf.TransferCharacteristics == nil {
		return false
	}
	return *inf.TransferCharacteristics == 16 || *inf.TransferCharacteristics == 18
}
