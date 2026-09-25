package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

const (
	minStreamFPS     = 1
	maxStreamFPS     = 30
	defaultStreamFPS = 6

	fluidFrameWidth  = 640
	fluidFrameHeight = 360
	hdFrameWidth     = 1280
	hdFrameHeight    = 720
	// Original mode used to pipe native 2K as uncompressed PPM; FFmpeg then
	// occasionally failed to allocate ~9MB packets. Cap at 1080p raw RGBA.
	originalFrameWidth  = 1920
	originalFrameHeight = 1080

	// Camera reboots often leave the RTSP TCP socket half-open. ffmpeg then
	// sits on the last frame forever unless the decode loop times out.
	streamStallTimeout   = 8 * time.Second
	streamConnectTimeout = 15 * time.Second
)

var (
	errStreamStalled        = errors.New("stream stalled")
	errStreamConnectTimeout = errors.New("stream connect timeout")
)

func streamStallReason(lastFrameNano, lastChangeNano int64, started, now time.Time, stall, connect time.Duration) error {
	if lastFrameNano == 0 {
		if now.Sub(started) >= connect {
			return errStreamConnectTimeout
		}
		return nil
	}
	if now.Sub(time.Unix(0, lastFrameNano)) >= stall {
		return errStreamStalled
	}
	// ffmpeg's fps filter keeps emitting the last picture when the camera
	// freezes, so "frames arriving" is not enough — the picture must change.
	if lastChangeNano > 0 && now.Sub(time.Unix(0, lastChangeNano)) >= stall {
		return errStreamStalled
	}
	return nil
}

func frameFingerprint(frame *image.RGBA) uint64 {
	if frame == nil || len(frame.Pix) == 0 {
		return 0
	}
	pix := frame.Pix
	step := 64
	if len(pix) < step {
		step = 4
	}
	var hash uint64 = 1469598103934665603
	for i := 0; i+3 < len(pix); i += step {
		hash ^= uint64(pix[i]) | uint64(pix[i+1])<<8 | uint64(pix[i+2])<<16 | uint64(pix[i+3])<<24
		hash *= 1099511628211
	}
	hash ^= uint64(len(pix))
	return hash
}

func liveStreamArgs(streamURL string, fps int, size image.Rectangle, extra []string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-threads", "1",
	}
	args = append(args, extra...)
	args = append(args,
		"-i", streamURL, "-map", "0:v:0", "-an",
		"-vf", videoFilter(fps, size), "-threads", "1",
		"-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1",
	)
	return args
}

func clampFPS(value float64) int {
	fps := int(value + 0.5)
	if fps < minStreamFPS {
		return minStreamFPS
	}
	if fps > maxStreamFPS {
		return maxStreamFPS
	}
	return fps
}

// streamFrameSize keeps the decoded resolution near what the window shows, so the
// buffers stay small and the GPU handles whatever scaling is left.
func streamFrameSize(quality string) image.Rectangle {
	if normalizeQuality(quality) == "hd" {
		return image.Rect(0, 0, hdFrameWidth, hdFrameHeight)
	}
	return image.Rect(0, 0, fluidFrameWidth, fluidFrameHeight)
}

func decodeFrameSize(resolutionMode, quality string) image.Rectangle {
	if normalizeResolutionMode(resolutionMode) == "original" {
		return image.Rect(0, 0, originalFrameWidth, originalFrameHeight)
	}
	return streamFrameSize(quality)
}

func fpsFilter(fps int) string {
	return fmt.Sprintf("fps=%d", clampFPS(float64(fps)))
}

// videoFilter pads after scaling so every frame is exactly the size we allocate,
// which lets the reader refill one buffer instead of looking for frame boundaries.
func videoFilter(fps int, size image.Rectangle) string {
	width, height := size.Dx(), size.Dy()
	return fmt.Sprintf("%s,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		fpsFilter(fps), width, height, width, height)
}

type videoStream struct {
	view     *canvas.Image
	zoomView *videoZoom
	cancel   context.CancelFunc

	mu         sync.Mutex
	shown      *image.RGBA // buffer the view is pointing at
	pending    *image.RGBA // newest frame waiting to be drawn
	spare      *image.RGBA // buffer the reader may refill
	updating   bool        // a present callback is already queued
	generation uint64
}

func newVideoStream() *videoStream {
	placeholder := image.NewRGBA(streamFrameSize("fluid"))
	view := canvas.NewImageFromImage(placeholder)
	view.FillMode = canvas.ImageFillContain
	// Anything else makes Fyne resample every frame on the CPU into a fresh buffer.
	view.ScaleMode = canvas.ImageScaleFastest
	view.SetMinSize(fyne.NewSize(480, 270))
	zoomView := newVideoZoom(view)
	return &videoStream{view: view, zoomView: zoomView, shown: placeholder}
}

// buffer hands the reader somewhere to write, recycling a retired frame when possible.
func (s *videoStream) buffer(size image.Rectangle) *image.RGBA {
	s.mu.Lock()
	reused := s.spare
	s.spare = nil
	s.mu.Unlock()

	if reused != nil && reused.Rect.Eq(size) {
		return reused
	}
	return image.NewRGBA(size)
}

// offer stores frame as the next one to draw and discards any frame the window never
// got to, so a slow window costs frames rather than unbounded memory. It reports
// whether a present callback still has to be queued.
func (s *videoStream) offer(frame *image.RGBA) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending != nil && s.spare == nil {
		s.spare = s.pending
	}
	s.pending = frame
	queue := !s.updating
	s.updating = true
	return queue
}

// take claims the newest frame for the view and retires the one being replaced.
func (s *videoStream) take() *image.RGBA {
	s.mu.Lock()
	defer s.mu.Unlock()

	frame := s.pending
	s.pending = nil
	s.updating = false
	if frame != nil {
		retired := s.shown
		s.shown = frame
		if s.spare == nil {
			s.spare = retired
		}
	}
	return frame
}

func (s *videoStream) publish(frame *image.RGBA) {
	if s.offer(frame) {
		fyne.Do(s.present)
	}
}

func (s *videoStream) publishGeneration(frame *image.RGBA, generation uint64) {
	s.mu.Lock()
	if s.generation != generation {
		s.mu.Unlock()
		return
	}
	if s.pending != nil && s.spare == nil {
		s.spare = s.pending
	}
	s.pending = frame
	queue := !s.updating
	s.updating = true
	s.mu.Unlock()
	if queue {
		fyne.Do(s.present)
	}
}

func (s *videoStream) present() {
	frame := s.take()
	if frame == nil {
		return
	}
	s.view.Image = frame
	s.view.Refresh()
	if s.zoomView != nil {
		s.zoomView.Refresh()
	}
}

func readPPMToken(reader *bufio.Reader) (string, error) {
	var token strings.Builder
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		if value == '#' && token.Len() == 0 {
			if _, err := reader.ReadString('\n'); err != nil {
				return "", err
			}
			continue
		}
		if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
			if token.Len() > 0 {
				return token.String(), nil
			}
			continue
		}
		token.WriteByte(value)
		if token.Len() > 32 {
			return "", errors.New("cabecalho PPM invalido")
		}
	}
}

func readPPMHeader(reader *bufio.Reader) (image.Rectangle, error) {
	magic, err := readPPMToken(reader)
	if err != nil {
		return image.Rectangle{}, err
	}
	if magic != "P6" {
		return image.Rectangle{}, fmt.Errorf("formato PPM inesperado: %q", magic)
	}
	widthText, err := readPPMToken(reader)
	if err != nil {
		return image.Rectangle{}, err
	}
	heightText, err := readPPMToken(reader)
	if err != nil {
		return image.Rectangle{}, err
	}
	maxValue, err := readPPMToken(reader)
	if err != nil {
		return image.Rectangle{}, err
	}
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 ||
		width > 8192 || height > 8192 || int64(width)*int64(height) > 32<<20 {
		return image.Rectangle{}, fmt.Errorf("dimensoes PPM invalidas: %sx%s", widthText, heightText)
	}
	if maxValue != "255" {
		return image.Rectangle{}, fmt.Errorf("profundidade PPM nao suportada: %s", maxValue)
	}
	return image.Rect(0, 0, width, height), nil
}

// readPPMPixels reads RGB into the RGBA allocation and expands it backwards in
// place. This preserves the original resolution without allocating per frame.
func readPPMPixels(reader io.Reader, frame *image.RGBA) error {
	pixels := frame.Rect.Dx() * frame.Rect.Dy()
	rgbBytes := pixels * 3
	if _, err := io.ReadFull(reader, frame.Pix[:rgbBytes]); err != nil {
		return err
	}
	for index := pixels - 1; index >= 0; index-- {
		source := index * 3
		target := index * 4
		red, green, blue := frame.Pix[source], frame.Pix[source+1], frame.Pix[source+2]
		frame.Pix[target] = red
		frame.Pix[target+1] = green
		frame.Pix[target+2] = blue
		frame.Pix[target+3] = 0xff
	}
	return nil
}

func (s *videoStream) start(parent context.Context, config cameraConfig, channel string, fps int, size image.Rectangle, report func(error)) {
	s.startURL(parent, config.rtspURL(channel), fps, size, report)
}

func (s *videoStream) startURL(parent context.Context, streamURL string, fps int, size image.Rectangle, report func(error)) {
	s.startURLWithReady(parent, streamURL, fps, size, nil, report)
}

func (s *videoStream) startURLWithReady(parent context.Context, streamURL string, fps int, size image.Rectangle, ready func(), report func(error)) {
	s.startURLWithStallWatch(parent, streamURL, fps, size, nil, ready, report, streamStallTimeout, streamConnectTimeout)
}

func (s *videoStream) startURLWithOptions(parent context.Context, streamURL string, fps int, size image.Rectangle, extra []string, ready func(), report func(error)) {
	s.startURLWithStallWatch(parent, streamURL, fps, size, extra, ready, report, 0, 0)
}

func (s *videoStream) startURLWithStallWatch(parent context.Context, streamURL string, fps int, size image.Rectangle, extra []string, ready func(), report func(error), stall, connect time.Duration) {
	s.stop()
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.generation++
	generation := s.generation
	s.mu.Unlock()

	go func() {
		var reported sync.Once
		doReport := func(err error) {
			if report == nil || err == nil {
				return
			}
			s.mu.Lock()
			current := s.generation == generation
			s.mu.Unlock()
			if !current {
				return
			}
			reported.Do(func() { report(err) })
		}

		var lastFrame atomic.Int64
		var lastChange atomic.Int64
		var lastHash atomic.Uint64
		started := time.Now()
		if stall > 0 || connect > 0 {
			go func() {
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if reason := streamStallReason(lastFrame.Load(), lastChange.Load(), started, time.Now(), stall, connect); reason != nil {
							cancel()
							doReport(reason)
							return
						}
					}
				}
			}()
		}

		readySent := false
		publish := func(frame *image.RGBA) {
			now := time.Now().UnixNano()
			lastFrame.Store(now)
			hash := frameFingerprint(frame)
			if lastChange.Load() == 0 || hash != lastHash.Load() {
				lastHash.Store(hash)
				lastChange.Store(now)
			}
			if !readySent {
				readySent = true
				if ready != nil {
					ready()
				}
			}
			s.publishGeneration(frame, generation)
		}
		cmd := exec.CommandContext(ctx, ffmpegBinary(), liveStreamArgs(streamURL, fps, size, extra)...)
		hideCommandWindow(cmd)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			doReport(err)
			return
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			doReport(err)
			return
		}

		var readErr error
		for {
			frame := s.buffer(size)
			if _, readErr = io.ReadFull(stdout, frame.Pix); readErr != nil {
				break
			}
			publish(frame)
		}
		_ = cmd.Wait() // Wait finishes the stderr copy, so the buffer is only safe to read afterwards.

		if ctx.Err() != nil {
			return
		}
		if readErr == nil {
			return
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = readErr.Error()
		}
		doReport(errors.New(message))
	}()
}

func (s *videoStream) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.updating = false
	if s.pending != nil && s.spare == nil {
		s.spare = s.pending
	}
	s.pending = nil
}
