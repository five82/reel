package ffprobe

import "testing"

func TestTagValueCaseInsensitive(t *testing.T) {
	tags := map[string]string{
		"LANGUAGE": "eng",
		"Title":    "Main Audio",
	}

	if got := tagValue(tags, "language"); got != "eng" {
		t.Fatalf("tagValue(language) = %q, want %q", got, "eng")
	}
	if got := tagValue(tags, "title"); got != "Main Audio" {
		t.Fatalf("tagValue(title) = %q, want %q", got, "Main Audio")
	}
}
