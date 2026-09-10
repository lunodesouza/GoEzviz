//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestDPAPISecretRoundTrip(t *testing.T) {
	const password = "camera-password-#123"
	encrypted, err := protectSecret(password)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, password) {
		t.Fatalf("password was not protected: %q", encrypted)
	}
	decrypted, err := unprotectSecret(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != password {
		t.Fatalf("decrypted password = %q", decrypted)
	}
}
