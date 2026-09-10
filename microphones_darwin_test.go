//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestMicrophoneCaptureArgs(t *testing.T) {
	args := strings.Join(microphoneCaptureArgs("MacBook Pro Microphone", 8000), " ")
	for _, expected := range []string{
		"-f avfoundation",
		"-i :MacBook Pro Microphone",
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
		args := strings.Join(microphoneG711CaptureArgs("MacBook Pro Microphone", 8000, test.mulaw), " ")
		for _, expected := range []string{
			"-f avfoundation",
			"-i :MacBook Pro Microphone",
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
	pcm := strings.Join(microphonePCMCaptureArgs("MacBook Pro Microphone", 16000), " ")
	for _, expected := range []string{
		"-f avfoundation",
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

func TestAVFoundationMicrophoneParser(t *testing.T) {
	output := `[AVFoundation indev @ 0x123] AVFoundation video devices:
[AVFoundation indev @ 0x123] [0] FaceTime HD Camera
[AVFoundation indev @ 0x123] AVFoundation audio devices:
[AVFoundation indev @ 0x123] [0] MacBook Pro Microphone
[AVFoundation indev @ 0x123] [1] MacBook Pro Microphone
[AVFoundation indev @ 0x123] [2] USB Headset
[AVFoundation indev @ 0x123] Error opening input`
	devices := parseAVFoundationAudioDevices(output)
	if len(devices) != 2 || devices[0] != "MacBook Pro Microphone" || devices[1] != "USB Headset" {
		t.Fatalf("unexpected parser devices: %v", devices)
	}
}
