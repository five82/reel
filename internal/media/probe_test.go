package media

import "testing"

func TestDetectHDR(t *testing.T) {
	tests := []struct {
		name      string
		primaries string
		transfer  string
		matrix    string
		want      bool
	}{
		{name: "bt2020 primaries", primaries: "bt2020", want: true},
		{name: "pq transfer", transfer: "smpte2084", want: true},
		{name: "hlg transfer", transfer: "arib-std-b67", want: true},
		{name: "bt2020 matrix", matrix: "bt2020nc", want: true},
		{name: "sdr", primaries: "bt709", transfer: "bt709", matrix: "bt709", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectHDR(tt.primaries, tt.transfer, tt.matrix); got != tt.want {
				t.Fatalf("detectHDR() = %v, want %v", got, tt.want)
			}
		})
	}
}
