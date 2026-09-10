package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
)

// TestSendToneToCamera plays a low test tone through the camera speaker using the
// same back channel path the Falar button uses, so the speaker can be checked
// without talking into the microphone.
func TestSendToneToCamera(t *testing.T) {
	if os.Getenv("EZVIZ_TONE") == "" {
		t.Skip("set EZVIZ_TONE=1 to play a test tone on the camera speaker")
	}
	settings, err := loadAppSettings()
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("EZVIZ_TONE_HOST")
	channel := os.Getenv("EZVIZ_TONE_CHANNEL")
	if channel == "" {
		channel = "102"
	}
	seconds := 5
	if value, convErr := strconv.Atoi(os.Getenv("EZVIZ_TONE_SECONDS")); convErr == nil && value > 0 {
		seconds = value
	}
	for _, device := range settings.Devices {
		if (host != "" && device.Host != host) || device.Password == "" {
			continue
		}
		streamURL, parseErr := base.ParseURL(device.cameraConfig().rtspURL(channel))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+20)*time.Second)
		playTone(ctx, t, ffmpeg, streamURL, device.Host, seconds)
		cancel()
	}
}

func playTone(ctx context.Context, t *testing.T, ffmpeg string, streamURL *base.URL, host string, seconds int) {
	t.Helper()
	client, media, aac, _, err := openTalkSession(ctx, streamURL)
	if err != nil {
		t.Fatalf("%s: %v", host, err)
	}
	defer client.Close()
	if aac == nil {
		t.Fatalf("%s: a camera nao anunciou AAC no backchannel", host)
	}

	// 220 Hz keeps the tone audible without being shrill.
	generator := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=220:sample_rate=%d:duration=%d", aac.ClockRate(), seconds),
		"-ac", "1", "-ar", strconv.Itoa(aac.ClockRate()),
		"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "32k",
		"-f", "adts", "pipe:1")
	hideCommandWindow(generator)
	adts, err := generator.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.Start(); err != nil {
		t.Fatal(err)
	}

	t.Logf("%s: enviando tom de 220 Hz por %ds em %s", host, seconds, media.Control)
	// EOF just means the generator reached the end of the tone.
	if err := streamAACPackets(ctx, client, media, aac, adts, func() bool { return true }, nil, nil); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("%s: %v", host, err)
	}
	_ = generator.Wait()
}
