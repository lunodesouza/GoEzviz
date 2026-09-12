package main

import "testing"

func TestZoomLevelIndexAndStep(t *testing.T) {
	if got := zoomLevelIndex(1); got != 0 {
		t.Fatalf("index 1x = %d", got)
	}
	if got := zoomLevelIndex(2); got != 2 {
		t.Fatalf("index 2x = %d", got)
	}
	if got := stepZoomFactor(1, 1); got != 1.5 {
		t.Fatalf("zoom in from 1x = %v", got)
	}
	if got := stepZoomFactor(1.5, -1); got != 1 {
		t.Fatalf("zoom out back to 1x = %v", got)
	}
	if got := stepZoomFactor(4, 1); got != 4 {
		t.Fatalf("zoom in at max should stay 4x, got %v", got)
	}
	if got := stepZoomFactor(1, -1); got != 1 {
		t.Fatalf("zoom out at min should stay 1x, got %v", got)
	}
}

func TestFormatViewZoom(t *testing.T) {
	if got := formatViewZoom(2); got != "2" {
		t.Fatalf("2x = %q", got)
	}
	if got := formatViewZoom(1.5); got != "1.5" {
		t.Fatalf("1.5x = %q", got)
	}
}
