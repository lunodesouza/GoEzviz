package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type cameraAudio struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
	playing    bool
}

func clampVolume(value float64) int {
	volume := int(value + 0.5)
	if volume < 0 {
		return 0
	}
	if volume > 100 {
		return 100
	}
	return volume
}

func audioPlayerArgs(config cameraConfig, channel string, volume int) []string {
	return audioPlayerURLArgs(config.rtspURL(channel), volume)
}

func audioPlayerURLArgs(streamURL string, volume int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-nodisp", "-autoexit",
		"-rtsp_transport", "tcp",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-vn",
		"-volume", fmt.Sprintf("%d", clampVolume(float64(volume))),
		streamURL,
	}
}

func audioProbeArgs(streamURL string) []string {
	return []string{
		"-v", "error",
		"-rtsp_transport", "tcp",
		"-analyzeduration", "1000000",
		"-probesize", "131072",
		"-select_streams", "a:0",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		streamURL,
	}
}

func audioStreamAvailable(ctx context.Context, streamURL string) (bool, error) {
	probe, err := findFFmpegTool("ffprobe")
	if err != nil {
		// ffplay may still be installed separately, so keep the old behavior.
		return true, nil
	}
	cmd := exec.CommandContext(ctx, probe, audioProbeArgs(streamURL)...)
	hideCommandWindow(cmd)
	output, runErr := cmd.CombinedOutput()
	if strings.TrimSpace(string(output)) != "" {
		return true, nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return false, ctx.Err()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false, nil
	}
	if runErr != nil {
		// A reachable RTSP profile without an audio stream often makes ffprobe
		// exit with status 1 and no diagnostic output.
		return false, nil
	}
	return false, nil
}

func (audio *cameraAudio) start(parent context.Context, config cameraConfig, channel string, volume int, report func(error)) error {
	return audio.startURL(parent, config.rtspURL(channel), volume, report)
}

func (audio *cameraAudio) startURL(parent context.Context, streamURL string, volume int, report func(error)) error {
	audio.stop()

	player, err := findFFmpegTool("ffplay")
	if err != nil {
		return errors.New("ffplay nao foi encontrado; coloque ffplay na pasta ffmpeg ao lado do GoEzviz")
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, player, audioPlayerURLArgs(streamURL, volume)...)
	hideCommandWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}

	audio.mu.Lock()
	audio.generation++
	generation := audio.generation
	audio.cancel = cancel
	audio.playing = true
	audio.mu.Unlock()

	go func() {
		waitErr := cmd.Wait()
		audio.mu.Lock()
		current := audio.generation == generation
		if current {
			audio.cancel = nil
			audio.playing = false
		}
		audio.mu.Unlock()
		cancelled := ctx.Err() != nil
		cancel()

		if !current || cancelled {
			return
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			report(errors.New(message))
		} else if waitErr != nil {
			report(fmt.Errorf("ffplay: %w", waitErr))
		} else {
			report(errors.New("o fluxo de audio foi encerrado pela camera"))
		}
	}()
	return nil
}

func (audio *cameraAudio) stop() {
	audio.mu.Lock()
	audio.generation++
	cancel := audio.cancel
	audio.cancel = nil
	audio.playing = false
	audio.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (audio *cameraAudio) isPlaying() bool {
	audio.mu.Lock()
	defer audio.mu.Unlock()
	return audio.playing
}
