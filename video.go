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
)

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
	view   *canvas.Image
	cancel context.CancelFunc

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
	return &videoStream{view: view, shown: placeholder}
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

func (s *videoStream) start(parent context.Context, config cameraConfig, channel string, fps int, size image.Rectangle, original bool, report func(error)) {
	s.startURL(parent, config.rtspURL(channel), fps, size, original, report)
}

func (s *videoStream) startURL(parent context.Context, streamURL string, fps int, size image.Rectangle, original bool, report func(error)) {
	s.startURLWithReady(parent, streamURL, fps, size, original, nil, report)
}

func (s *videoStream) startURLWithReady(parent context.Context, streamURL string, fps int, size image.Rectangle, original bool, ready func(), report func(error)) {
	s.stop()
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.generation++
	generation := s.generation
	s.mu.Unlock()

	go func() {
		args := []string{
			"-hide_banner", "-loglevel", "error",
			"-rtsp_transport", "tcp", "-threads", "1",
			"-i", streamURL, "-map", "0:v:0", "-an",
		}
		readySent := false
		publish := func(frame *image.RGBA) {
			if !readySent {
				readySent = true
				if ready != nil {
					ready()
				}
			}
			s.publishGeneration(frame, generation)
		}
		if original {
			// PPM carries dimensions in every frame, so the source can stay at its
			// native resolution without requiring a separate ffprobe executable.
			args = append(args, "-vf", fpsFilter(fps), "-threads", "1",
				"-f", "image2pipe", "-vcodec", "ppm", "pipe:1")
		} else {
			args = append(args, "-vf", videoFilter(fps, size), "-threads", "1",
				"-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1")
		}
		cmd := exec.CommandContext(ctx, ffmpegBinary(), args...)
		hideCommandWindow(cmd)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			report(err)
			return
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			report(err)
			return
		}

		var readErr error
		if original {
			reader := bufio.NewReaderSize(stdout, 1<<20)
			for {
				frameSize, err := readPPMHeader(reader)
				if err != nil {
					readErr = err
					break
				}
				frame := s.buffer(frameSize)
				if readErr = readPPMPixels(reader, frame); readErr != nil {
					break
				}
				publish(frame)
			}
		} else {
			for {
				frame := s.buffer(size)
				if _, readErr = io.ReadFull(stdout, frame.Pix); readErr != nil {
					break
				}
				publish(frame)
			}
		}
		_ = cmd.Wait() // Wait finishes the stderr copy, so the buffer is only safe to read afterwards.

		if readErr == nil || ctx.Err() != nil {
			return
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = readErr.Error()
		}
		report(errors.New(message))
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
	if s.pending != nil && s.spare == nil {
		s.spare = s.pending
	}
	s.pending = nil
}
