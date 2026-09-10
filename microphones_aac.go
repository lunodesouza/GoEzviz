package main

import "strconv"

// microphoneAACEncodeArgs turns raw PCM (s16le mono) into ADTS AAC for the
// camera talk backchannel. Shared by every capture backend.
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
