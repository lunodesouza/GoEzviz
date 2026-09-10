package main

import (
	"context"
	"fmt"
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
	ctx, cancel := context.WithCancel(context.Background())
	grid := newGridManager(ctx)
	running := false

	status := widget.NewLabel("Pronto")
	status.Wrapping = fyne.TextWrapWord
	if loadErr != nil {
		status.SetText("Configuracoes carregadas parcialmente: " + loadErr.Error())
	}

	saveSettings := func() {
		if err := saveAppSettings(settings); err != nil {
			status.SetText("Erro ao salvar configuracoes: " + err.Error())
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
	rebuild := func() {
		settings.normalize()
		saveSettings()
		if running {
			grid.rebuild(settings)
			status.SetText(fmt.Sprintf("%d cameras/perfis em exibicao", len(grid.tiles)))
		}
	}

	devicesButton := widget.NewButton("Devices", func() {
		showDeviceManager(viewer, &settings, rebuild)
	})
	connectButton := widget.NewButton("Conectar", func() {
		running = true
		grid.rebuild(settings)
		status.SetText(fmt.Sprintf("%d cameras/perfis em exibicao", len(grid.tiles)))
	})

	fpsEntry := widget.NewEntry()
	fpsEntry.SetText(strconv.Itoa(settings.FPS))
	fpsEntry.OnSubmitted = func(text string) {
		fps, err := strconv.Atoi(text)
		if err != nil || fps < minStreamFPS || fps > maxStreamFPS {
			status.SetText(fmt.Sprintf("FPS deve estar entre %d e %d", minStreamFPS, maxStreamFPS))
			fpsEntry.SetText(strconv.Itoa(settings.FPS))
			return
		}
		settings.FPS = fps
		rebuild()
		status.SetText(fmt.Sprintf("FPS alterado para %d", fps))
	}

	resolution := widget.NewSelect([]string{"Otimizada", "Original"}, func(value string) {
		settings.ResolutionMode = value
		rebuild()
	})
	resolution.SetSelected(settings.ResolutionMode)

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
		status.SetText("Microfones: " + microphoneErr.Error())
	}

	autoConnect := widget.NewCheck("Reconectar ao abrir", func(enabled bool) {
		settings.AutoConnect = enabled
		saveSettings()
	})
	autoConnect.SetChecked(settings.AutoConnect)

	top := container.NewHBox(
		devicesButton,
		connectButton,
		widget.NewLabel("FPS:"),
		container.NewGridWrap(fyne.NewSize(55, fpsEntry.MinSize().Height), fpsEntry),
		widget.NewLabel("Resolucao:"),
		container.NewGridWrap(fyne.NewSize(125, resolution.MinSize().Height), resolution),
		widget.NewLabel("Microfone:"),
		container.NewGridWrap(fyne.NewSize(230, microphone.MinSize().Height), microphone),
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
		grid.rebuild(settings)
		status.SetText(fmt.Sprintf("%d cameras/perfis em exibicao", len(grid.tiles)))
	} else {
		grid.showIdle()
	}
	viewer.Run()
}
