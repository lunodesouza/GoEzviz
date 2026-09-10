package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFFmpegToolPrefersBundledBinary(t *testing.T) {
	dir := t.TempDir()
	name := "ffmpeg" + ffmpegToolExt()
	bundled := filepath.Join(dir, name)
	if err := os.WriteFile(bundled, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpegTestDirs = []string{dir}
	t.Cleanup(func() { ffmpegTestDirs = nil })

	got, err := findFFmpegTool("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if got != bundled {
		t.Fatalf("got %q, want bundled %q", got, bundled)
	}
}
