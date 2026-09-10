//go:build !windows

package main

import "errors"

func listMicrophones() ([]string, error) {
	return nil, errors.New("a captura de microfone esta disponivel apenas no Windows")
}

func microphoneCaptureArgs(_ string, _ int) []string {
	return nil
}

func microphoneG711CaptureArgs(_ string, _ int, _ bool) []string {
	return nil
}

func microphoneAACCaptureArgs(_ string, _ int) []string {
	return nil
}

func microphonePCMCaptureArgs(_ string, _ int) []string {
	return nil
}

func microphoneAACEncodeArgs(_ int) []string {
	return nil
}
