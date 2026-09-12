package main

import "testing"

func TestNormalizePTZSpeed(t *testing.T) {
	if got := normalizePTZSpeed(""); got != "medium" {
		t.Fatalf("empty = %q", got)
	}
	if got := normalizePTZSpeed("Lenta"); got != "slow" {
		t.Fatalf("Lenta = %q", got)
	}
	if got := normalizePTZSpeed("FAST"); got != "fast" {
		t.Fatalf("FAST = %q", got)
	}
}

func TestPTZSpeedVelocity(t *testing.T) {
	slow := ptzSpeedVelocity("slow")
	medium := ptzSpeedVelocity("medium")
	fast := ptzSpeedVelocity("fast")
	if !(slow < medium && medium < fast) {
		t.Fatalf("expected slow < medium < fast, got %v %v %v", slow, medium, fast)
	}
	if medium != 0.55 {
		t.Fatalf("medium should keep the previous default 0.55, got %v", medium)
	}
}
