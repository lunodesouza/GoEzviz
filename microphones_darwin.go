//go:build darwin

package main

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var avfoundationAudioDevice = regexp.MustCompile(`\[(\d+)\]\s+(.+?)\s*$`)

func listMicrophones() ([]string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg nao foi encontrado")
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-list_devices", "true", "-f", "avfoundation", "-i", "")
	var output bytes.Buffer
	cmd.Stderr = &output
	_ = cmd.Run() // FFmpeg exits with an error after listing the devices.

	devices := parseAVFoundationAudioDevices(output.String())
	if len(devices) == 0 {
		return nil, errors.New("nenhum microfone AVFoundation foi encontrado")
	}
	return devices, nil
}

func parseAVFoundationAudioDevices(output string) []string {
	lines := strings.Split(output, "\n")
	inAudio := false
	devices := make([]string, 0)
	seen := make(map[string]bool)
	for _, line := range lines {
		if strings.Contains(line, "AVFoundation audio devices") {
			inAudio = true
			continue
		}
		if !inAudio {
			continue
		}
		if strings.Contains(line, "AVFoundation video devices") {
			break
		}
		match := avfoundationAudioDevice.FindStringSubmatch(line)
		if match == nil {
			if strings.Contains(strings.ToLower(line), "error") {
				break
			}
			continue
		}
		name := strings.TrimSpace(match[2])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		devices = append(devices, name)
	}
	return devices
}

func avfoundationAudioInput(device string) string {
	return ":" + device
}

func microphoneCaptureArgs(device string, sampleRate int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "avfoundation",
		"-i", avfoundationAudioInput(device),
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", "pcm_s16be", "-f", "s16be", "pipe:1",
	}
}

func microphonePCMCaptureArgs(device string, sampleRate int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "avfoundation",
		"-i", avfoundationAudioInput(device),
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", "pcm_s16le", "-f", "s16le", "pipe:1",
	}
}

func microphoneG711CaptureArgs(device string, sampleRate int, mulaw bool) []string {
	codec := "pcm_alaw"
	format := "alaw"
	if mulaw {
		codec = "pcm_mulaw"
		format = "mulaw"
	}
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "avfoundation",
		"-i", avfoundationAudioInput(device),
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", codec, "-f", format, "pipe:1",
	}
}
