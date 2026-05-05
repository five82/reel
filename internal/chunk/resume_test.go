package chunk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkDirNameUsesCanonicalPathHash(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	otherDir := filepath.Join(dir, "other")
	if err := os.Mkdir(otherDir, 0755); err != nil {
		t.Fatal(err)
	}
	sameBase := filepath.Join(otherDir, "movie.mkv")
	if err := os.WriteFile(sameBase, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}

	name := WorkDirName(input)
	otherName := WorkDirName(sameBase)
	if name == otherName {
		t.Fatalf("WorkDirName collided for distinct canonical paths: %q", name)
	}
	if !strings.HasPrefix(name, ".reel-movie-") {
		t.Fatalf("WorkDirName() = %q, want .reel-movie-<hash>", name)
	}
}

func TestResumeValidateRequiresExistingMatchingChunkFile(t *testing.T) {
	workDir := t.TempDir()
	if err := EnsureEncodeDir(workDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IVFPath(workDir, 0), []byte("chunk0"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IVFPath(workDir, 1), []byte("chunk1"), 0644); err != nil {
		t.Fatal(err)
	}

	resume := &ResumeInf{ChunksDone: []ChunkComp{
		{Idx: 0, Frames: 10, Size: 6},   // valid
		{Idx: 1, Frames: 10, Size: 999}, // size mismatch
		{Idx: 2, Frames: 10, Size: 6},   // missing file
		{Idx: 3, Frames: 99, Size: 6},   // frame mismatch
	}}
	chunks := []Chunk{
		{Idx: 0, Start: 0, End: 10},
		{Idx: 1, Start: 10, End: 20},
		{Idx: 2, Start: 20, End: 30},
		{Idx: 3, Start: 30, End: 40},
	}

	validated := resume.Validate(workDir, chunks)
	if len(validated.ChunksDone) != 1 || validated.ChunksDone[0].Idx != 0 {
		t.Fatalf("validated chunks = %+v, want only chunk 0", validated.ChunksDone)
	}
}

func TestEnsureResumeManifestRejectsMismatch(t *testing.T) {
	workDir := t.TempDir()
	manifest := ResumeManifest{InputPath: "/media/movie.mkv", Quality: 27}
	if err := EnsureResumeManifest(workDir, manifest); err != nil {
		t.Fatal(err)
	}

	if err := EnsureResumeManifest(workDir, manifest); err != nil {
		t.Fatalf("matching manifest rejected: %v", err)
	}

	mismatch := manifest
	mismatch.Quality = 28
	err := EnsureResumeManifest(workDir, mismatch)
	if err == nil {
		t.Fatal("mismatched manifest accepted")
	}
	if !strings.Contains(err.Error(), "resume metadata does not match current encode settings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChunkFingerprintChangesWithBoundaries(t *testing.T) {
	chunks := []Chunk{{Idx: 0, Start: 0, End: 10}, {Idx: 1, Start: 10, End: 20}}
	changed := []Chunk{{Idx: 0, Start: 0, End: 12}, {Idx: 1, Start: 12, End: 20}}

	if ChunkFingerprint(chunks) == ChunkFingerprint(changed) {
		t.Fatal("ChunkFingerprint did not change when chunk boundaries changed")
	}
}
