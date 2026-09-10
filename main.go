package main

import (
	"context"
	"runtime/debug"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	debug.SetGCPercent(35)

	viewer := app.NewWithID("local.goezviz")
	viewer.SetIcon(appIcon())
	window := viewer.NewWindow("GoEzviz")
	window.SetIcon(appIcon())
	window.Resize(fyne.NewSize(1280, 820))

	settings, loadErr := loadAppSettings()
	settings.normalize()
	if settings.Language == "" {
		showLanguagePrompt(window, &settings, func() {
			buildMainUI(viewer, window, &settings, loadErr)
		})
	} else {
		SetLanguage(settings.Language)
		buildMainUI(viewer, window, &settings, loadErr)
	}
	viewer.Run()
}

func showLanguagePrompt(window fyne.Window, settings *appSettings, onDone func()) {
	suggested := suggestedLanguage()
	title := widget.NewLabelWithStyle(
		"Escolha o idioma / Choose language",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	hint := widget.NewLabel("Voce pode trocar depois na barra / You can change it later in the toolbar")
	hint.Wrapping = fyne.TextWrapWord
	hint.Alignment = fyne.TextAlignCenter

	choose := func(code string) {
		settings.Language = normalizeLanguage(code)
		SetLanguage(settings.Language)
		_ = saveAppSettings(*settings)
		onDone()
	}

	ptButton := widget.NewButton("Português", func() { choose(langPortuguese) })
	enButton := widget.NewButton("English", func() { choose(langEnglish) })
	if suggested == langPortuguese {
		ptButton.Importance = widget.HighImportance
	} else {
		enButton.Importance = widget.HighImportance
	}

	window.SetContent(container.NewCenter(container.NewVBox(
		title,
		hint,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, ptButton, enButton),
	)))
	window.SetCloseIntercept(nil)
	window.Show()
}

func buildMainUI(viewer fyne.App, window fyne.Window, settings *appSettings, loadErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	grid := newGridManager(ctx)
	running := false

	status := widget.NewLabel(T("Ready"))
	status.Wrapping = fyne.TextWrapWord
	if loadErr != nil {
		status.SetText(T("SettingsPartialLoad", map[string]string{"Error": loadErr.Error()}))
	}

	saveSettings := func() {
		if err := saveAppSettings(*settings); err != nil {
			status.SetText(T("SettingsSaveError", map[string]string{"Error": err.Error()}))
		}
	}
	grid.onVolume = func(volume int) {
		settings.Volume = volume
		saveSettings()
	}
	grid.onProfile = func(deviceID, _ string, profileToken string) {
		for deviceIndex := range settings.Devices {
			device := &settings.Devices[deviceIndex]
			if device.ID != deviceID {
				continue
			}
			groupKey := ""
			for _, profile := range device.Profiles {
				if profile.Token == profileToken {
					groupKey = profileGroupKey(profile)
					break
				}
			}
			for profileIndex := range device.Profiles {
				profile := &device.Profiles[profileIndex]
				if profileGroupKey(*profile) == groupKey {
					profile.Selected = profile.Token == profileToken
				}
			}
			break
		}
		saveSettings()
	}

	var (
		devicesButton   *widget.Button
		connectButton   *widget.Button
		fpsLabel        *widget.Label
		resolutionLabel *widget.Label
		microphoneLabel *widget.Label
		languageLabel   *widget.Label
		autoConnect     *widget.Check
		resolution      *widget.Select
		language        *widget.Select
		rebuild         func()
	)

	var applyChromeLanguage func()
	applyChromeLanguage = func() {
		devicesButton.SetText(T("Devices"))
		connectButton.SetText(T("Connect"))
		fpsLabel.SetText(T("FPS"))
		resolutionLabel.SetText(T("Resolution"))
		microphoneLabel.SetText(T("Microphone"))
		languageLabel.SetText(T("Language"))
		autoConnect.Text = T("ReconnectOnOpen")
		autoConnect.Refresh()
		selectedMode := settings.ResolutionMode
		resolution.OnChanged = nil
		resolution.Options = []string{T("Optimized"), T("Original")}
		resolution.SetSelected(resolutionModeLabel(selectedMode))
		resolution.OnChanged = func(value string) {
			settings.ResolutionMode = resolutionModeFromLabel(value)
			rebuild()
		}
		language.OnChanged = nil
		language.Options = []string{T("Portuguese"), T("English")}
		language.SetSelected(languageDisplayName(settings.Language))
		language.OnChanged = func(value string) {
			code := languageFromDisplay(value)
			if code == settings.Language {
				return
			}
			settings.Language = code
			SetLanguage(code)
			saveSettings()
			applyChromeLanguage()
			if running {
				grid.rebuild(*settings)
			}
		}
		if running {
			status.SetText(T("CamerasShowing", map[string]any{"Count": len(grid.tiles)}))
		}
		grid.refreshIdleTexts(!running)
	}

	rebuild = func() {
		settings.normalize()
		saveSettings()
		if running {
			grid.rebuild(*settings)
			status.SetText(T("CamerasShowing", map[string]any{"Count": len(grid.tiles)}))
		}
	}

	devicesButton = widget.NewButton(T("Devices"), func() {
		showDeviceManager(viewer, settings, rebuild)
	})
	connectButton = widget.NewButton(T("Connect"), func() {
		running = true
		grid.rebuild(*settings)
		status.SetText(T("CamerasShowing", map[string]any{"Count": len(grid.tiles)}))
	})

	fpsEntry := widget.NewEntry()
	fpsEntry.SetText(strconv.Itoa(settings.FPS))
	fpsEntry.OnSubmitted = func(text string) {
		fps, err := strconv.Atoi(text)
		if err != nil || fps < minStreamFPS || fps > maxStreamFPS {
			status.SetText(T("FPSRange", map[string]any{"Min": minStreamFPS, "Max": maxStreamFPS}))
			fpsEntry.SetText(strconv.Itoa(settings.FPS))
			return
		}
		settings.FPS = fps
		rebuild()
		status.SetText(T("FPSChanged", map[string]any{"FPS": fps}))
	}

	resolution = widget.NewSelect([]string{T("Optimized"), T("Original")}, nil)
	resolution.SetSelected(resolutionModeLabel(settings.ResolutionMode))

	microphones, microphoneErr := listMicrophones()
	microphone := widget.NewSelect(microphones, func(value string) {
		settings.Microphone = value
		rebuild()
	})
	if settings.Microphone != "" && !containsString(microphones, settings.Microphone) {
		microphones = append([]string{settings.Microphone}, microphones...)
		microphone.Options = microphones
		microphone.Refresh()
	}
	if settings.Microphone != "" {
		microphone.SetSelected(settings.Microphone)
	} else if len(microphones) > 0 {
		microphone.SetSelected(microphones[0])
		settings.Microphone = microphones[0]
	}
	if microphoneErr != nil {
		status.SetText(T("MicrophonesError", map[string]string{"Error": microphoneErr.Error()}))
	}

	autoConnect = widget.NewCheck(T("ReconnectOnOpen"), func(enabled bool) {
		settings.AutoConnect = enabled
		saveSettings()
	})
	autoConnect.SetChecked(settings.AutoConnect)

	language = widget.NewSelect([]string{T("Portuguese"), T("English")}, nil)
	language.SetSelected(languageDisplayName(settings.Language))

	fpsLabel = widget.NewLabel(T("FPS"))
	resolutionLabel = widget.NewLabel(T("Resolution"))
	microphoneLabel = widget.NewLabel(T("Microphone"))
	languageLabel = widget.NewLabel(T("Language"))
	applyChromeLanguage()

	top := container.NewHBox(
		devicesButton,
		connectButton,
		fpsLabel,
		container.NewGridWrap(fyne.NewSize(55, fpsEntry.MinSize().Height), fpsEntry),
		resolutionLabel,
		container.NewGridWrap(fyne.NewSize(125, resolution.MinSize().Height), resolution),
		microphoneLabel,
		container.NewGridWrap(fyne.NewSize(230, microphone.MinSize().Height), microphone),
		languageLabel,
		container.NewGridWrap(fyne.NewSize(120, language.MinSize().Height), language),
		autoConnect,
	)
	window.SetContent(container.NewBorder(
		container.NewVBox(top, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), status),
		nil, nil,
		grid.container,
	))
	window.SetCloseIntercept(func() {
		running = false
		grid.stop()
		cancel()
		saveSettings()
		window.SetCloseIntercept(nil)
		window.Close()
	})
	window.Show()
	if settings.AutoConnect {
		running = true
		grid.rebuild(*settings)
		status.SetText(T("CamerasShowing", map[string]any{"Count": len(grid.tiles)}))
	} else {
		grid.showIdle()
	}
}
