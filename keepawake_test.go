package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDisplayStayAwakeStartsAndStops(t *testing.T) {
	originalInterval := keepAwakeInterval
	keepAwakeInterval = 20 * time.Millisecond
	t.Cleanup(func() { keepAwakeInterval = originalInterval })

	var jiggles atomic.Int32
	originalJiggle := jiggleMouseFn
	originalPrevent := preventDisplaySleepFn
	jiggleMouseFn = func() { jiggles.Add(1) }
	preventDisplaySleepFn = func(bool) {}
	t.Cleanup(func() {
		jiggleMouseFn = originalJiggle
		preventDisplaySleepFn = originalPrevent
	})

	stay := &displayStayAwake{}
	stay.Set(true)
	time.Sleep(70 * time.Millisecond)
	stay.Set(false)
	count := jiggles.Load()
	if count < 2 {
		t.Fatalf("expected immediate jiggle plus ticker, got %d", count)
	}
	time.Sleep(50 * time.Millisecond)
	if jiggles.Load() != count {
		t.Fatal("jiggle continued after stay-awake was disabled")
	}
}
