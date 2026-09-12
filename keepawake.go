package main

import (
	"context"
	"sync"
	"time"
)

var keepAwakeInterval = time.Minute

var (
	jiggleMouseFn         = jiggleMouse
	preventDisplaySleepFn = preventDisplaySleep
)

type displayStayAwake struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (stay *displayStayAwake) Set(enabled bool) {
	stay.mu.Lock()
	defer stay.mu.Unlock()
	if stay.cancel != nil {
		stay.cancel()
		stay.cancel = nil
		preventDisplaySleepFn(false)
	}
	if !enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	stay.cancel = cancel
	preventDisplaySleepFn(true)
	go runKeepAwake(ctx)
}

func runKeepAwake(ctx context.Context) {
	ticker := time.NewTicker(keepAwakeInterval)
	defer ticker.Stop()
	jiggleMouseFn()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jiggleMouseFn()
		}
	}
}
