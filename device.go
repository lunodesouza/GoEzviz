package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func (device deviceSettings) cameraConfig() cameraConfig {
	return cameraConfig{
		host:             device.Host,
		username:         device.Username,
		password:         device.Password,
		deviceServiceURL: device.DeviceServiceURL,
	}
}

func (device deviceSettings) streamURL(profile profileSettings) string {
	config := device.cameraConfig()
	if profile.RTSPURL != "" {
		return config.withCredentials(profile.RTSPURL)
	}
	if profile.FallbackChannel != "" {
		return config.rtspURL(profile.FallbackChannel)
	}
	return ""
}

func mergeMediaProfiles(existing []profileSettings, discovered []onvifMediaProfile) []profileSettings {
	selected := make(map[string]bool, len(existing))
	selectedChannels := make(map[string]bool, len(existing))
	sources := make(map[string]bool, len(discovered))
	for _, profile := range discovered {
		if profile.SourceToken != "" {
			sources[strings.ToLower(profile.SourceToken)] = true
		}
	}
	for _, profile := range existing {
		selected[profile.Token] = profile.Selected
		if profile.FallbackChannel != "" {
			selectedChannels[profile.FallbackChannel] = profile.Selected
		}
	}
	result := make([]profileSettings, 0, len(discovered))
	for _, profile := range discovered {
		channel := streamChannelFromURL(profile.RTSPURL)
		isSelected, known := selected[profile.Token]
		if !known && channel != "" {
			isSelected, known = selectedChannels[channel]
		}
		if !known {
			isSelected = false
		}
		hasPTZ := profile.HasPTZ
		if len(sources) > 1 {
			hasPTZ = hasPTZ && movableSourceToken(profile.SourceToken)
		}
		result = append(result, profileSettings{
			Token:           profile.Token,
			Name:            profile.Name,
			SourceToken:     profile.SourceToken,
			RTSPURL:         profile.RTSPURL,
			FallbackChannel: channel,
			Resolution:      formatResolution(profile.Width, profile.Height),
			Codec:           profile.Codec,
			PTZ:             hasPTZ,
			Audio:           profile.HasAudio,
			Talk:            profile.CanTalk,
			Selected:        isSelected,
		})
	}
	return result
}

func movableSourceToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	return token == "2" || strings.HasSuffix(token, "_2") ||
		strings.HasSuffix(token, "-2") || strings.Contains(token, "pt")
}

func profileGroupKey(profile profileSettings) string {
	if source := strings.TrimSpace(profile.SourceToken); source != "" {
		return "source:" + strings.ToLower(source)
	}
	channel := profile.FallbackChannel
	if len(channel) >= 3 {
		return "channel-source:" + channel[:1]
	}
	return "profile:" + profile.Token
}

func formatResolution(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func profileFrameSize(profile profileSettings) string {
	parts := strings.Split(strings.ToLower(profile.Resolution), "x")
	if len(parts) != 2 {
		return "Fluido"
	}
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	if width*height > fluidFrameWidth*fluidFrameHeight {
		return "HD"
	}
	return "Fluido"
}

func streamChannelFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if _, err := strconv.Atoi(last); err == nil {
		return last
	}
	return ""
}
