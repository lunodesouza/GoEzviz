//go:build windows

package main

import "golang.org/x/sys/windows"

const (
	mouseEventMove    = 0x0001
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

var (
	modUser32                   = windows.NewLazySystemDLL("user32.dll")
	modKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procMouseEvent              = modUser32.NewProc("mouse_event")
	procSetThreadExecutionState = modKernel32.NewProc("SetThreadExecutionState")
)

func preventDisplaySleep(enabled bool) {
	if enabled {
		_, _, _ = procSetThreadExecutionState.Call(esContinuous | esSystemRequired | esDisplayRequired)
		return
	}
	_, _, _ = procSetThreadExecutionState.Call(esContinuous)
}

func jiggleMouse() {
	_, _, _ = procMouseEvent.Call(mouseEventMove, 1, 0, 0, 0)
	_, _, _ = procMouseEvent.Call(mouseEventMove, uintptr(^uint32(0)), 0, 0, 0)
}
