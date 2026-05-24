package quality

import (
	"encoding/json"
	"os"
	"testing"

	"codeberg.org/five82/reel/internal/video"
)

func TestEnsureDisplayModelWritesVshipCompatibleSDRColorspace(t *testing.T) {
	path, err := EnsureDisplayModel(t.TempDir(), &video.Info{}, "")
	if err != nil {
		t.Fatalf("EnsureDisplayModel() error = %v", err)
	}

	model := readGeneratedDisplayModel(t, path)
	if model.Colorspace != "SDR" {
		t.Fatalf("generated SDR colorspace = %q, want %q", model.Colorspace, "SDR")
	}
}

func TestEnsureDisplayModelWritesVshipCompatibleHDRColorspaces(t *testing.T) {
	tests := []struct {
		name     string
		transfer int32
		want     string
	}{
		{name: "pq", transfer: 16, want: "BT.2020-PQ"},
		{name: "hlg", transfer: 18, want: "BT.2020-HLG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := EnsureDisplayModel(t.TempDir(), &video.Info{TransferCharacteristics: &tt.transfer}, "")
			if err != nil {
				t.Fatalf("EnsureDisplayModel() error = %v", err)
			}

			model := readGeneratedDisplayModel(t, path)
			if model.Colorspace != tt.want {
				t.Fatalf("generated HDR colorspace = %q, want %q", model.Colorspace, tt.want)
			}
		})
	}
}

func readGeneratedDisplayModel(t *testing.T, path string) displayModel {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read generated display model: %v", err)
	}

	var models map[string]displayModel
	if err := json.Unmarshal(data, &models); err != nil {
		t.Fatalf("failed to decode generated display model: %v", err)
	}

	model, ok := models["xav"]
	if !ok {
		t.Fatalf("generated display model missing xav key")
	}
	return model
}
