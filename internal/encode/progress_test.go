package encode

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseSvtProgressFrames(t *testing.T) {
	tests := []struct {
		line string
		want int
		ok   bool
	}{
		{line: "Encoding frame 48 3.48 kbps 7301.49 fps", want: 48, ok: true},
		{line: "Encoding frame 120", want: 120, ok: true},
		{line: "SVT [info]: config", ok: false},
	}

	for _, tt := range tests {
		got, ok := parseSvtProgressFrames(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("parseSvtProgressFrames(%q) = (%d, %v), want (%d, %v)", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSplitProgressLinesSplitsCarriageReturns(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("Encoding frame 1\rEncoding frame 2\nEncoding frame 3"))
	scanner.Split(splitProgressLines)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{"Encoding frame 1", "Encoding frame 2", "Encoding frame 3"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %v, want %v", lines, want)
		}
	}
}

func TestMonitorSvtProgressReportsIncreasingFrames(t *testing.T) {
	var updates []int
	badChunk := false
	done := monitorSvtProgress(
		strings.NewReader("Encoding frame 1\rnoise\rEncoding frame 5\rEncoding frame 999\r"),
		3,
		10,
		func(chunkIdx, frames int) {
			if chunkIdx != 3 {
				badChunk = true
			}
			updates = append(updates, frames)
		},
	)
	output := <-done

	want := []int{1, 5, 10}
	if badChunk {
		t.Fatal("monitorSvtProgress reported wrong chunk index")
	}
	if len(updates) != len(want) {
		t.Fatalf("updates = %v, want %v", updates, want)
	}
	for i := range want {
		if updates[i] != want[i] {
			t.Fatalf("updates = %v, want %v", updates, want)
		}
	}
	if !strings.Contains(output, "noise") {
		t.Fatalf("output = %q, want captured stderr", output)
	}
}
