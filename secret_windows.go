//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectSecret(plainText string) (string, error) {
	if plainText == "" {
		return "", nil
	}
	input := []byte(plainText)
	inputBlob := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var outputBlob windows.DataBlob
	if err := windows.CryptProtectData(
		&inputBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outputBlob,
	); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outputBlob.Data))) //nolint:errcheck
	protected := unsafe.Slice(outputBlob.Data, int(outputBlob.Size))
	return base64.StdEncoding.EncodeToString(protected), nil
}

func unprotectSecret(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	protected, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(protected) == 0 {
		return "", errors.New("senha protegida vazia")
	}
	inputBlob := windows.DataBlob{Size: uint32(len(protected)), Data: &protected[0]}
	var outputBlob windows.DataBlob
	if err := windows.CryptUnprotectData(
		&inputBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outputBlob,
	); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outputBlob.Data))) //nolint:errcheck
	plainText := unsafe.Slice(outputBlob.Data, int(outputBlob.Size))
	return string(plainText), nil
}
