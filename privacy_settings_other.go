//go:build !windows && !darwin

package main

import "errors"

func openMicrophonePrivacySettings() error {
	return errors.New("abrir a tela de microfone nao e suportado neste sistema")
}
