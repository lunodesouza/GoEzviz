//go:build darwin

package main

import "os/exec"

func openMicrophonePrivacySettings() error {
	// Works on modern System Settings and older System Preferences builds.
	cmd := exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")
	return cmd.Start()
}
