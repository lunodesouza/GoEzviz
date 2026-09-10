//go:build windows

package main

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
)

var directShowAudioDevice = regexp.MustCompile(`"([^"\r\n]+)" \(audio\)`)

func listMicrophones() ([]string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg nao foi encontrado")
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	hideCommandWindow(cmd)
	var output bytes.Buffer
	cmd.Stderr = &output
	_ = cmd.Run() // FFmpeg exits with an error after listing the devices.

	matches := directShowAudioDevice.FindAllStringSubmatch(output.String(), -1)
	devices := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		name := match[1]
		if !seen[name] {
			seen[name] = true
			devices = append(devices, name)
		}
	}
	if len(devices) == 0 {
		return nil, errors.New("nenhum microfone DirectShow foi encontrado")
	}
	return devices, nil
}

func microphoneCaptureArgs(device string, sampleRate int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "dshow", "-audio_buffer_size", "20",
		"-i", "audio=" + device,
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", "pcm_s16be", "-f", "s16be", "pipe:1",
	}
}

func microphonePCMCaptureArgs(device string, sampleRate int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "dshow", "-audio_buffer_size", "20",
		"-i", "audio=" + device,
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", "pcm_s16le", "-f", "s16le", "pipe:1",
	}
}

func microphoneAACEncodeArgs(sampleRate int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-f", "s16le", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-i", "pipe:0",
		"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "32k",
		"-f", "adts", "pipe:1",
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
		"-f", "dshow", "-audio_buffer_size", "20",
		"-i", "audio=" + device,
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-acodec", codec, "-f", format, "pipe:1",
	}
}
