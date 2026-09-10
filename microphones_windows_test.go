//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestMicrophoneCaptureArgs(t *testing.T) {
	args := strings.Join(microphoneCaptureArgs("Microfone de teste", 8000), " ")
	for _, expected := range []string{
		"-f dshow",
		"-audio_buffer_size 20",
		"-i audio=Microfone de teste",
		"-ac 1",
		"-ar 8000",
		"-acodec pcm_s16be",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("microphone arguments do not contain %q: %s", expected, args)
		}
	}
}

func TestMicrophoneG711CaptureArgs(t *testing.T) {
	for _, test := range []struct {
		mulaw  bool
		codec  string
		format string
	}{
		{true, "pcm_mulaw", "mulaw"},
		{false, "pcm_alaw", "alaw"},
	} {
		args := strings.Join(microphoneG711CaptureArgs("Microfone de teste", 8000, test.mulaw), " ")
		for _, expected := range []string{
			"-f dshow",
			"-audio_buffer_size 20",
			"-ac 1",
			"-ar 8000",
			"-acodec " + test.codec,
			"-f " + test.format + " pipe:1",
		} {
			if !strings.Contains(args, expected) {
				t.Fatalf("G.711 microphone arguments do not contain %q: %s", expected, args)
			}
		}
	}
}

func TestMicrophoneAACCaptureArgs(t *testing.T) {
	pcm := strings.Join(microphonePCMCaptureArgs("Microfone de teste", 16000), " ")
	for _, expected := range []string{
		"-f dshow",
		"-ar 16000",
		"-acodec pcm_s16le",
		"-f s16le pipe:1",
	} {
		if !strings.Contains(pcm, expected) {
			t.Fatalf("PCM microphone arguments do not contain %q: %s", expected, pcm)
		}
	}
	encode := strings.Join(microphoneAACEncodeArgs(16000), " ")
	for _, expected := range []string{
		"-f s16le",
		"-i pipe:0",
		"-c:a aac",
		"-b:a 32k",
		"-f adts pipe:1",
	} {
		if !strings.Contains(encode, expected) {
			t.Fatalf("AAC encode arguments do not contain %q: %s", expected, encode)
		}
	}
}

func TestDirectShowMicrophoneParser(t *testing.T) {
	output := `"Camera" (video)
"Microfone A" (audio)
"Microfone A" (audio)
"Microfone B" (audio)`
	matches := directShowAudioDevice.FindAllStringSubmatch(output, -1)
	if len(matches) != 3 || matches[0][1] != "Microfone A" || matches[2][1] != "Microfone B" {
		t.Fatalf("unexpected parser matches: %v", matches)
	}
}
