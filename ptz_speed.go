package main

import "strings"

const defaultPTZSpeed = "medium"

func normalizePTZSpeed(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "slow", "lenta":
		return "slow"
	case "fast", "rapida", "rápida":
		return "fast"
	default:
		return defaultPTZSpeed
	}
}

func ptzSpeedVelocity(value string) float64 {
	switch normalizePTZSpeed(value) {
	case "slow":
		return 0.22
	case "fast":
		return 0.95
	default:
		return 0.55
	}
}

func ptzSpeedLabel(value string) string {
	switch normalizePTZSpeed(value) {
	case "slow":
		return T("PTZSpeedSlow")
	case "fast":
		return T("PTZSpeedFast")
	default:
		return T("PTZSpeedMedium")
	}
}

func ptzSpeedFromLabel(label string) string {
	switch label {
	case T("PTZSpeedSlow"):
		return "slow"
	case T("PTZSpeedFast"):
		return "fast"
	default:
		return defaultPTZSpeed
	}
}
