//go:build !windows && !darwin

package main

import "os/exec"

func preventDisplaySleep(_ bool) {}

func jiggleMouse() {
	_ = exec.Command("xdg-screensaver", "reset").Run()
	if err := exec.Command("xdotool", "mousemove_relative", "--", "1", "0").Run(); err == nil {
		_ = exec.Command("xdotool", "mousemove_relative", "--", "-1", "0").Run()
	}
}
