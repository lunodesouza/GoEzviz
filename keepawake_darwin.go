//go:build darwin

package main

import "os/exec"

var caffeinateCmd *exec.Cmd

func preventDisplaySleep(enabled bool) {
	if caffeinateCmd != nil && caffeinateCmd.Process != nil {
		_ = caffeinateCmd.Process.Kill()
		_, _ = caffeinateCmd.Process.Wait()
		caffeinateCmd = nil
	}
	if !enabled {
		return
	}
	cmd := exec.Command("caffeinate", "-dim")
	if err := cmd.Start(); err != nil {
		return
	}
	caffeinateCmd = cmd
}

func jiggleMouse() {
	// caffeinate already blocks idle sleep; a 1px nudge covers tools that
	// only watch input. cllocation-free relative move via cliclick if present.
	cmd := exec.Command("cliclick", "m:+1,+0")
	if err := cmd.Run(); err == nil {
		_ = exec.Command("cliclick", "m:-1,+0").Run()
	}
}
