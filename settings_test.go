package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsV2MultipleDevicesRoundTrip(t *testing.T) {
	useTemporaryConfigDir(t)

	settings := appSettings{
		FPS:              18,
		ResolutionMode:   "Original",
		Volume:           63,
		Microphone:       "Microfone USB",
		AutoConnect:      true,
		DisplayMode:      "Uma em cima da outra",
		SelectedDeviceID: "front-door",
		Devices: []deviceSettings{
			{
				ID:               "front-door",
				Name:             "Entrada",
				Host:             "192.0.2.10",
				Username:         "admin",
				Password:         "entrada-secret",
				DeviceServiceURL: "http://192.0.2.10/onvif/device_service",
				Profiles: []profileSettings{{
					Token: "main", Name: "Principal", SourceToken: "video-1",
					RTSPURL: "rtsp://192.0.2.10/live", FallbackChannel: "101",
					Resolution: "2560x1440", Codec: "H264", PTZ: true, Audio: true, Selected: true,
				}},
			},
			{
				ID: "garage", Name: "Garagem", Host: "192.0.2.11",
				Username: "viewer", Password: "garage-secret",
				Profiles: []profileSettings{{
					Token: "sub", Name: "Baixa", FallbackChannel: "102",
					Resolution: "640x360", Codec: "H265",
				}},
			},
		},
	}

	if err := saveAppSettings(settings); err != nil {
		t.Fatal(err)
	}
	path, err := settingsPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "entrada-secret") || strings.Contains(string(data), "garage-secret") {
		t.Fatalf("JSON contains a plaintext password:\n%s", data)
	}
	var persisted appSettings
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != 2 || len(persisted.Devices) != 2 {
		t.Fatalf("unexpected persisted settings: %+v", persisted)
	}
	for _, device := range persisted.Devices {
		if device.EncryptedPassword == "" {
			t.Fatalf("device %q has no protected password", device.ID)
		}
	}

	loaded, err := loadAppSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FPS != 18 || loaded.ResolutionMode != "Original" || loaded.Volume != 63 ||
		loaded.Microphone != "Microfone USB" || !loaded.AutoConnect {
		t.Fatalf("global preferences were not preserved: %+v", loaded)
	}
	if loaded.Devices[0].Password != "entrada-secret" || loaded.Devices[1].Password != "garage-secret" {
		t.Fatalf("device passwords were not restored: %+v", loaded.Devices)
	}
	if loaded.Host != "192.0.2.10" || loaded.Password != "entrada-secret" {
		t.Fatalf("selected device was not projected to the compatibility view: %+v", loaded)
	}
}

func TestSettingsV1MigrationCreatesLegacyProfiles(t *testing.T) {
	useTemporaryConfigDir(t)

	protected, err := protectSecret("legacy-secret")
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacySettings{
		Version: 1, Host: "192.0.2.20", Username: "admin",
		EncryptedPassword: protected, Quality1: "HD", Quality2: "Fluido",
		DisplayMode: "Lado a lado", ResolutionMode: "Otimizada",
		FPS: 12, Volume: 44, Microphone: "Mic", AutoConnect: true,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path, err := settingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadAppSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != settingsVersion || len(loaded.Devices) != 1 {
		t.Fatalf("v1 was not migrated: %+v", loaded)
	}
	device := loaded.Devices[0]
	if device.ID == "" || device.Password != "legacy-secret" || len(device.Profiles) != 4 {
		t.Fatalf("unexpected migrated device: %+v", device)
	}
	wantSelected := map[string]bool{"101": true, "102": false, "201": false, "202": true}
	for _, profile := range device.Profiles {
		if selected, exists := wantSelected[profile.Token]; !exists || profile.Selected != selected {
			t.Fatalf("unexpected legacy profile selection: %+v", profile)
		}
		if profile.RTSPURL == "" || profile.FallbackChannel != profile.Token {
			t.Fatalf("legacy profile lacks RTSP fallback data: %+v", profile)
		}
	}
	if loaded.Quality1 != "HD" || loaded.Quality2 != "Fluido" {
		t.Fatalf("legacy qualities were not preserved: %q, %q", loaded.Quality1, loaded.Quality2)
	}
}

func TestSettingsNormalizationKeepsStableDeviceIDs(t *testing.T) {
	settings := appSettings{
		FPS: 6, Volume: 100, ResolutionMode: "Otimizada", DisplayMode: "Lado a lado",
		Devices: []deviceSettings{
			{Host: "camera.local", Username: "admin"},
			{Host: "camera.local", Username: "admin"},
		},
	}
	settings.normalize()
	firstID := settings.Devices[0].ID
	if firstID == "" || settings.Devices[1].ID == firstID {
		t.Fatalf("device IDs are empty or duplicated: %+v", settings.Devices)
	}
	settings.normalize()
	if settings.Devices[0].ID != firstID {
		t.Fatalf("device ID changed after normalization: %q -> %q", firstID, settings.Devices[0].ID)
	}
}

func useTemporaryConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
}
