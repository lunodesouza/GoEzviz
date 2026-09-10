package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const settingsVersion = 2

type appSettings struct {
	Version          int              `json:"version"`
	FPS              int              `json:"fps"`
	ResolutionMode   string           `json:"resolution"`
	Volume           int              `json:"volume"`
	Microphone       string           `json:"microphone,omitempty"`
	AutoConnect      bool             `json:"auto_connect"`
	DisplayMode      string           `json:"display_mode"`
	SelectedDeviceID string           `json:"selected_device_id,omitempty"`
	Devices          []deviceSettings `json:"devices"`

	// Compatibility view used by the current UI. These fields are projected
	// from the selected device and are never written to the v2 JSON.
	Host              string `json:"-"`
	Username          string `json:"-"`
	EncryptedPassword string `json:"-"`
	Password          string `json:"-"`
	Quality1          string `json:"-"`
	Quality2          string `json:"-"`
}

type deviceSettings struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Host              string            `json:"host"`
	Username          string            `json:"username"`
	EncryptedPassword string            `json:"encrypted_password,omitempty"`
	Password          string            `json:"-"`
	DeviceServiceURL  string            `json:"device_service_url,omitempty"`
	Profiles          []profileSettings `json:"profiles"`
}

type profileSettings struct {
	Token           string `json:"token"`
	Name            string `json:"name"`
	SourceToken     string `json:"source_token,omitempty"`
	RTSPURL         string `json:"rtsp_url,omitempty"`
	FallbackChannel string `json:"fallback_channel,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	Codec           string `json:"codec,omitempty"`
	PTZ             bool   `json:"ptz"`
	Audio           bool   `json:"audio"`
	Talk            bool   `json:"talk"`
	Selected        bool   `json:"selected"`
}

type legacySettings struct {
	Version           int    `json:"version"`
	Host              string `json:"ip"`
	Username          string `json:"username"`
	EncryptedPassword string `json:"encrypted_password,omitempty"`
	Quality1          string `json:"quality_lens_1"`
	Quality2          string `json:"quality_lens_2"`
	DisplayMode       string `json:"display_mode"`
	ResolutionMode    string `json:"resolution"`
	FPS               int    `json:"fps"`
	Volume            int    `json:"volume"`
	Microphone        string `json:"microphone,omitempty"`
	AutoConnect       bool   `json:"auto_connect"`
}

func defaultSettings() appSettings {
	return appSettings{
		Version:        settingsVersion,
		Username:       "admin",
		Quality1:       "Fluido",
		Quality2:       "Fluido",
		DisplayMode:    "Lado a lado",
		ResolutionMode: "Otimizada",
		FPS:            defaultStreamFPS,
		Volume:         100,
		Devices:        []deviceSettings{},
	}
}

func settingsPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "goezviz", "config.json"), nil
}

func legacySettingsPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ezviz-viewer", "config.json"), nil
}

func migrateSettingsDir() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacyPath, err := legacySettingsPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(legacyPath); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := copyFile(legacyPath, path); err != nil {
		return err
	}
	legacyKey := filepath.Join(filepath.Dir(legacyPath), "secret.key")
	newKey := filepath.Join(filepath.Dir(path), "secret.key")
	if _, err := os.Stat(legacyKey); err == nil {
		_ = copyFile(legacyKey, newKey)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

func loadAppSettings() (appSettings, error) {
	settings := defaultSettings()
	if err := migrateSettingsDir(); err != nil {
		return settings, err
	}
	path, err := settingsPath()
	if err != nil {
		return settings, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return defaultSettings(), err
	}
	if header.Version < settingsVersion {
		var legacy legacySettings
		if err := json.Unmarshal(data, &legacy); err != nil {
			return defaultSettings(), err
		}
		settings = migrateLegacySettings(legacy)
	} else if err := json.Unmarshal(data, &settings); err != nil {
		return defaultSettings(), err
	}
	settings.normalize()
	var passwordErrors []error
	for index := range settings.Devices {
		device := &settings.Devices[index]
		if device.EncryptedPassword == "" {
			continue
		}
		device.Password, err = unprotectSecret(device.EncryptedPassword)
		if err != nil {
			device.Password = ""
			passwordErrors = append(passwordErrors, fmt.Errorf("dispositivo %q: %w", device.ID, err))
		}
	}
	settings.projectSelectedDevice()
	return settings, errors.Join(passwordErrors...)
}

func saveAppSettings(settings appSettings) error {
	settings.normalize()
	settings = settings.clone()
	for index := range settings.Devices {
		device := &settings.Devices[index]
		if device.Password == "" && device.EncryptedPassword != "" {
			continue
		}
		encryptedPassword, err := protectSecret(device.Password)
		if err != nil {
			return fmt.Errorf("proteger senha do dispositivo %q: %w", device.ID, err)
		}
		device.EncryptedPassword = encryptedPassword
		device.Password = ""
	}
	settings.clearCompatibilityView()

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (settings *appSettings) normalize() {
	settings.Version = settingsVersion
	settings.FPS = clampFPS(float64(settings.FPS))
	settings.Volume = clampVolume(float64(settings.Volume))
	if settings.Quality1 != "HD" && settings.Quality1 != "Fluido" {
		settings.Quality1 = "Fluido"
	}
	if settings.Quality2 != "HD" && settings.Quality2 != "Fluido" {
		settings.Quality2 = "Fluido"
	}
	if settings.DisplayMode != "Uma em cima da outra" && settings.DisplayMode != "Lado a lado" {
		settings.DisplayMode = "Lado a lado"
	}
	if settings.ResolutionMode != "Original" && settings.ResolutionMode != "Otimizada" {
		settings.ResolutionMode = "Otimizada"
	}
	if len(settings.Devices) == 0 && (settings.Host != "" || settings.Username != "" || settings.Password != "" || settings.EncryptedPassword != "") {
		settings.Devices = []deviceSettings{legacyDevice(
			settings.Host,
			settings.Username,
			settings.Password,
			settings.EncryptedPassword,
			settings.Quality1,
			settings.Quality2,
		)}
	}
	seenIDs := make(map[string]struct{}, len(settings.Devices))
	for index := range settings.Devices {
		device := &settings.Devices[index]
		device.Host = strings.TrimSpace(device.Host)
		device.Username = strings.TrimSpace(device.Username)
		if device.ID == "" {
			device.ID = stableDeviceID(device.Host, device.Username, device.DeviceServiceURL)
		}
		baseID := device.ID
		for suffix := 2; ; suffix++ {
			if _, exists := seenIDs[device.ID]; !exists {
				break
			}
			device.ID = fmt.Sprintf("%s-%d", baseID, suffix)
		}
		seenIDs[device.ID] = struct{}{}
		if device.Name == "" {
			device.Name = device.Host
		}
		if device.DeviceServiceURL == "" && device.Host != "" {
			device.DeviceServiceURL = deviceServiceURL(device.Host)
		}
		if device.Profiles == nil {
			device.Profiles = []profileSettings{}
		}
		sources := make(map[string]bool)
		hasMovableSource := false
		for _, profile := range device.Profiles {
			if profile.SourceToken != "" {
				sources[strings.ToLower(profile.SourceToken)] = true
				hasMovableSource = hasMovableSource || movableSourceToken(profile.SourceToken)
			}
		}
		if len(sources) > 1 && hasMovableSource {
			for profileIndex := range device.Profiles {
				profile := &device.Profiles[profileIndex]
				profile.PTZ = profile.PTZ && movableSourceToken(profile.SourceToken)
			}
		}
	}
	if len(settings.Devices) > 0 && settings.deviceIndex(settings.SelectedDeviceID) < 0 {
		settings.SelectedDeviceID = settings.Devices[0].ID
	}
	if len(settings.Devices) > 0 {
		settings.projectSelectedDevice()
	}
}

func migrateLegacySettings(legacy legacySettings) appSettings {
	settings := appSettings{
		Version:           settingsVersion,
		FPS:               legacy.FPS,
		ResolutionMode:    legacy.ResolutionMode,
		Volume:            legacy.Volume,
		Microphone:        legacy.Microphone,
		AutoConnect:       legacy.AutoConnect,
		DisplayMode:       legacy.DisplayMode,
		Host:              legacy.Host,
		Username:          legacy.Username,
		EncryptedPassword: legacy.EncryptedPassword,
		Quality1:          legacy.Quality1,
		Quality2:          legacy.Quality2,
	}
	settings.Devices = []deviceSettings{legacyDevice(
		legacy.Host,
		legacy.Username,
		"",
		legacy.EncryptedPassword,
		legacy.Quality1,
		legacy.Quality2,
	)}
	settings.SelectedDeviceID = settings.Devices[0].ID
	settings.normalize()
	return settings
}

func legacyDevice(host, username, password, encryptedPassword, quality1, quality2 string) deviceSettings {
	if host == "" {
		host = defaultSettings().Host
	}
	if username == "" {
		username = defaultSettings().Username
	}
	if quality1 != "HD" {
		quality1 = "Fluido"
	}
	if quality2 != "HD" {
		quality2 = "Fluido"
	}
	device := deviceSettings{
		Name:              host,
		Host:              host,
		Username:          username,
		Password:          password,
		EncryptedPassword: encryptedPassword,
		DeviceServiceURL:  deviceServiceURL(host),
	}
	device.ID = stableDeviceID(device.Host, device.Username, device.DeviceServiceURL)
	device.Profiles = []profileSettings{
		legacyProfile(host, "101", "Lente 1 HD", "legacy-lens-1", "HD", quality1 == "HD", false),
		legacyProfile(host, "102", "Lente 1 Fluido", "legacy-lens-1", "Fluido", quality1 == "Fluido", false),
		legacyProfile(host, "201", "Lente 2 HD", "legacy-lens-2", "HD", quality2 == "HD", true),
		legacyProfile(host, "202", "Lente 2 Fluido", "legacy-lens-2", "Fluido", quality2 == "Fluido", true),
	}
	return device
}

func legacyProfile(host, channel, name, sourceToken, resolution string, selected, ptz bool) profileSettings {
	return profileSettings{
		Token:           channel,
		Name:            name,
		SourceToken:     sourceToken,
		RTSPURL:         legacyRTSPURL(host, channel),
		FallbackChannel: channel,
		Resolution:      resolution,
		PTZ:             ptz,
		Audio:           true,
		Talk:            true,
		Selected:        selected,
	}
}

func stableDeviceID(host, username, serviceURL string) string {
	identity := strings.ToLower(strings.TrimSpace(serviceURL))
	if identity == "" {
		identity = strings.ToLower(strings.TrimSpace(host)) + "\x00" + strings.ToLower(strings.TrimSpace(username))
	}
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("device-%x", sum[:8])
}

func deviceServiceURL(host string) string {
	return "http://" + hostForURL(host) + "/onvif/device_service"
}

func legacyRTSPURL(host, channel string) string {
	return (&url.URL{
		Scheme: "rtsp",
		Host:   net.JoinHostPort(strings.Trim(hostForURL(host), "[]"), "554"),
		Path:   "/Streaming/Channels/" + channel,
	}).String()
}

func hostForURL(host string) string {
	host = strings.TrimSpace(host)
	if parsed, err := url.Parse("http://" + host); err == nil && parsed.Hostname() != "" {
		if port := parsed.Port(); port != "" {
			return net.JoinHostPort(parsed.Hostname(), port)
		}
		return parsed.Hostname()
	}
	return host
}

func (settings *appSettings) deviceIndex(id string) int {
	for index := range settings.Devices {
		if settings.Devices[index].ID == id {
			return index
		}
	}
	return -1
}

func (settings *appSettings) projectSelectedDevice() {
	index := settings.deviceIndex(settings.SelectedDeviceID)
	if index < 0 {
		settings.clearCompatibilityView()
		return
	}
	device := &settings.Devices[index]
	settings.Host = device.Host
	settings.Username = device.Username
	settings.Password = device.Password
	settings.EncryptedPassword = device.EncryptedPassword
	settings.Quality1 = selectedLegacyQuality(device.Profiles, "101", "102")
	settings.Quality2 = selectedLegacyQuality(device.Profiles, "201", "202")
}

func selectedLegacyQuality(profiles []profileSettings, hdToken, fluidToken string) string {
	for _, profile := range profiles {
		if profile.Selected && (profile.Token == hdToken || profile.FallbackChannel == hdToken) {
			return "HD"
		}
	}
	for _, profile := range profiles {
		if profile.Selected && (profile.Token == fluidToken || profile.FallbackChannel == fluidToken) {
			return "Fluido"
		}
	}
	return "Fluido"
}

func (settings *appSettings) clearCompatibilityView() {
	settings.Host = ""
	settings.Username = ""
	settings.Password = ""
	settings.EncryptedPassword = ""
	settings.Quality1 = ""
	settings.Quality2 = ""
}

func (settings appSettings) clone() appSettings {
	settings.Devices = append([]deviceSettings(nil), settings.Devices...)
	for index := range settings.Devices {
		settings.Devices[index].Profiles = append([]profileSettings(nil), settings.Devices[index].Profiles...)
	}
	return settings
}
