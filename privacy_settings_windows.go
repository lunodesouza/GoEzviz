//go:build windows

package main

import "os/exec"

func openMicrophonePrivacySettings() error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", "ms-settings:privacy-microphone")
	hideCommandWindow(cmd)
	return cmd.Start()
}
