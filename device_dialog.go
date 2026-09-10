package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type deviceManager struct {
	window   fyne.Window
	settings *appSettings
	onChange func()

	list       *widget.List
	selected   int
	online     map[string]bool
	name       *widget.Entry
	host       *widget.Entry
	username   *widget.Entry
	password   *widget.Entry
	serviceURL *widget.Entry
	profiles   *fyne.Container
	status     *widget.Label
	refresh    *widget.Button
}

func showDeviceManager(app fyne.App, settings *appSettings, onChange func()) {
	manager := &deviceManager{
		window:     app.NewWindow(T("DevicesONVIF")),
		settings:   settings,
		onChange:   onChange,
		selected:   -1,
		online:     make(map[string]bool),
		name:       widget.NewEntry(),
		host:       widget.NewEntry(),
		username:   widget.NewEntry(),
		password:   widget.NewPasswordEntry(),
		serviceURL: widget.NewEntry(),
		profiles:   container.NewVBox(),
		status:     widget.NewLabel(T("SelectOrDiscover")),
	}
	manager.status.Wrapping = fyne.TextWrapWord
	manager.serviceURL.SetPlaceHolder("http://IP/onvif/device_service")
	manager.list = widget.NewList(
		func() int { return len(manager.settings.Devices) },
		func() fyne.CanvasObject { return widget.NewLabel(T("Device")) },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			device := manager.settings.Devices[id]
			state := T("Saved")
			if manager.online[device.Host] {
				state = T("Online")
			}
			object.(*widget.Label).SetText(fmt.Sprintf("%s (%s) - %s", device.Name, device.Host, state))
		},
	)
	manager.list.OnSelected = func(id widget.ListItemID) {
		manager.selected = id
		manager.loadSelected()
	}

	add := widget.NewButton(T("Add"), manager.addDevice)
	remove := widget.NewButton(T("Remove"), manager.removeDevice)
	searchProgress := widget.NewProgressBarInfinite()
	searchProgress.Hide()
	searchLabel := widget.NewLabel(T("SearchingONVIF"))
	searchLabel.Hide()
	var discover *widget.Button
	discover = widget.NewButton(T("DiscoverLAN"), func() {
		discover.Disable()
		discover.SetText(T("Searching"))
		searchProgress.Show()
		searchLabel.Show()
		manager.status.SetText(T("LookingForONVIF"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			cameras, err := discoverLocalCameras(ctx)
			fyne.Do(func() {
				discover.Enable()
				discover.SetText(T("DiscoverLAN"))
				searchProgress.Hide()
				searchLabel.Hide()
				if err != nil {
					manager.status.SetText(T("DiscoveryError", map[string]string{"Error": err.Error()}))
					return
				}
				manager.mergeDiscovery(cameras)
			})
		}()
	})
	manager.refresh = widget.NewButton(T("RefreshProfiles"), manager.refreshProfiles)
	save := widget.NewButton(T("SaveDevice"), manager.saveSelected)
	closeButton := widget.NewButton(T("Close"), manager.window.Close)

	form := widget.NewForm(
		widget.NewFormItem(T("Name"), manager.name),
		widget.NewFormItem(T("Host"), manager.host),
		widget.NewFormItem(T("Username"), manager.username),
		widget.NewFormItem(T("Password"), manager.password),
		widget.NewFormItem(T("ONVIFEndpoint"), manager.serviceURL),
	)
	left := container.NewBorder(
		container.NewVBox(
			discover,
			container.NewBorder(nil, nil, searchLabel, nil, searchProgress),
			container.NewGridWithColumns(2, add, remove),
		),
		nil, nil, nil,
		manager.list,
	)
	right := container.NewBorder(
		container.NewVBox(form, container.NewGridWithColumns(2, save, manager.refresh), widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), manager.status, closeButton),
		nil, nil,
		container.NewVScroll(manager.profiles),
	)
	split := container.NewHSplit(left, right)
	split.Offset = 0.33
	manager.window.SetContent(container.NewPadded(split))
	manager.window.Resize(fyne.NewSize(940, 620))
	if len(settings.Devices) > 0 {
		manager.list.Select(0)
	}
	manager.window.Show()
}

func (manager *deviceManager) loadSelected() {
	if manager.selected < 0 || manager.selected >= len(manager.settings.Devices) {
		return
	}
	device := manager.settings.Devices[manager.selected]
	manager.name.SetText(device.Name)
	manager.host.SetText(device.Host)
	manager.username.SetText(device.Username)
	manager.password.SetText(device.Password)
	manager.serviceURL.SetText(device.DeviceServiceURL)
	manager.renderProfiles()
}

func (manager *deviceManager) addDevice() {
	device := deviceSettings{
		ID:       fmt.Sprintf("device-%d", time.Now().UnixNano()),
		Name:     T("NewCamera"),
		Username: "admin",
		Profiles: []profileSettings{},
	}
	manager.settings.Devices = append(manager.settings.Devices, device)
	manager.list.Refresh()
	manager.list.Select(len(manager.settings.Devices) - 1)
}

func (manager *deviceManager) removeDevice() {
	if manager.selected < 0 || manager.selected >= len(manager.settings.Devices) {
		return
	}
	device := manager.settings.Devices[manager.selected]
	dialog.ShowConfirm(
		T("RemoveDeviceTitle"),
		T("RemoveDeviceBody", map[string]string{"Name": device.Name}),
		func(remove bool) {
			if !remove {
				return
			}
			index := manager.selected
			manager.settings.Devices = append(manager.settings.Devices[:index], manager.settings.Devices[index+1:]...)
			manager.selected = -1
			manager.profiles.RemoveAll()
			manager.list.Refresh()
			manager.settings.normalize()
			_ = saveAppSettings(*manager.settings)
			manager.onChange()
			if len(manager.settings.Devices) > 0 {
				manager.list.Select(0)
			}
		}, manager.window)
}

func (manager *deviceManager) saveSelected() {
	if manager.selected < 0 || manager.selected >= len(manager.settings.Devices) {
		return
	}
	host := normalizeHost(manager.host.Text)
	if host == "" || strings.TrimSpace(manager.username.Text) == "" {
		manager.status.SetText(T("FillHostUser"))
		return
	}
	device := &manager.settings.Devices[manager.selected]
	device.Name = strings.TrimSpace(manager.name.Text)
	if device.Name == "" {
		device.Name = host
	}
	device.Host = host
	device.Username = strings.TrimSpace(manager.username.Text)
	device.Password = manager.password.Text
	device.DeviceServiceURL = strings.TrimSpace(manager.serviceURL.Text)
	if device.DeviceServiceURL == "" {
		device.DeviceServiceURL = deviceServiceURL(host)
		manager.serviceURL.SetText(device.DeviceServiceURL)
	}
	manager.settings.normalize()
	if err := saveAppSettings(*manager.settings); err != nil {
		manager.status.SetText(T("SaveError", map[string]string{"Error": err.Error()}))
		return
	}
	manager.list.Refresh()
	manager.status.SetText(T("DeviceSaved"))
	manager.onChange()
}

func (manager *deviceManager) refreshProfiles() {
	manager.saveSelected()
	if manager.selected < 0 || manager.selected >= len(manager.settings.Devices) {
		return
	}
	index := manager.selected
	device := manager.settings.Devices[index]
	if device.Password == "" {
		manager.status.SetText(T("EnterPasswordFirst"))
		return
	}
	manager.refresh.Disable()
	manager.status.SetText(T("QueryingProfiles", map[string]string{"Name": device.Name}))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client := newONVIFClient(device.cameraConfig())
		profiles, err := client.discoverMediaProfiles(ctx)
		fyne.Do(func() {
			manager.refresh.Enable()
			if err != nil {
				manager.status.SetText(T("ONVIFError", map[string]string{"Error": err.Error()}))
				return
			}
			if index >= len(manager.settings.Devices) || manager.settings.Devices[index].ID != device.ID {
				return
			}
			current := &manager.settings.Devices[index]
			current.Profiles = mergeMediaProfiles(current.Profiles, profiles)
			current.DeviceServiceURL = device.cameraConfig().onvifDeviceURL()
			_ = saveAppSettings(*manager.settings)
			manager.renderProfiles()
			manager.status.SetText(T("ProfilesFound", map[string]any{"Count": len(profiles)}))
			manager.onChange()
		})
	}()
}

func (manager *deviceManager) renderProfiles() {
	manager.profiles.RemoveAll()
	if manager.selected < 0 || manager.selected >= len(manager.settings.Devices) {
		return
	}
	deviceIndex := manager.selected
	profiles := manager.settings.Devices[deviceIndex].Profiles
	if len(profiles) == 0 {
		manager.profiles.Add(widget.NewLabel(T("NoProfilesLoaded")))
		return
	}
	manager.profiles.Add(widget.NewLabelWithStyle(T("DisplayedProfiles"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	for profileIndex := range profiles {
		index := profileIndex
		profile := profiles[index]
		capabilities := make([]string, 0, 3)
		if profile.PTZ {
			capabilities = append(capabilities, "PTZ")
		}
		if profile.Audio {
			capabilities = append(capabilities, T("CapabilityAudio"))
		}
		if profile.Talk {
			capabilities = append(capabilities, T("CapabilityTalk"))
		}
		details := strings.TrimSpace(strings.Join([]string{profile.Resolution, profile.Codec}, " "))
		if len(capabilities) > 0 {
			details += " [" + strings.Join(capabilities, ", ") + "]"
		}
		check := widget.NewCheck(profile.Name+" - "+details, nil)
		check.SetChecked(profile.Selected)
		check.OnChanged = func(selected bool) {
			if deviceIndex >= len(manager.settings.Devices) || index >= len(manager.settings.Devices[deviceIndex].Profiles) {
				return
			}
			manager.settings.Devices[deviceIndex].Profiles[index].Selected = selected
			_ = saveAppSettings(*manager.settings)
			manager.onChange()
		}
		manager.profiles.Add(check)
	}
	manager.profiles.Add(layout.NewSpacer())
}

func (manager *deviceManager) mergeDiscovery(cameras []discoveredCamera) {
	for _, camera := range cameras {
		manager.online[camera.host] = true
		found := false
		for index := range manager.settings.Devices {
			device := &manager.settings.Devices[index]
			if device.Host != camera.host {
				continue
			}
			found = true
			device.DeviceServiceURL = camera.deviceServiceURL
			if device.Name == "" || device.Name == device.Host {
				device.Name = camera.name
			}
			break
		}
		if !found {
			manager.settings.Devices = append(manager.settings.Devices, deviceSettings{
				ID:               stableDeviceID(camera.host, "admin", camera.deviceServiceURL),
				Name:             camera.name,
				Host:             camera.host,
				Username:         "admin",
				DeviceServiceURL: camera.deviceServiceURL,
				Profiles:         []profileSettings{},
			})
		}
	}
	manager.settings.normalize()
	manager.list.Refresh()
	_ = saveAppSettings(*manager.settings)
	manager.status.SetText(T("DevicesFound", map[string]any{"Count": len(cameras)}))
	if manager.selected < 0 && len(manager.settings.Devices) > 0 {
		manager.list.Select(0)
	}
}
