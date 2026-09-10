package main

import (
	"context"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type gridManager struct {
	parent    context.Context
	container *fyne.Container
	tiles     []*cameraTile
	maximized string
	onVolume  func(int)
	onProfile func(deviceID, sourceToken, profileToken string)
}

func newGridManager(parent context.Context) *gridManager {
	return &gridManager{
		parent:    parent,
		container: container.New(layout.NewGridLayoutWithColumns(1), widget.NewLabel(T("OpenDevicesHint"))),
	}
}

func (manager *gridManager) rebuild(settings appSettings) {
	preferences := tilePreferences{
		FPS:            settings.FPS,
		ResolutionMode: settings.ResolutionMode,
		Volume:         settings.Volume,
		Microphone:     settings.Microphone,
		OnVolume:       manager.onVolume,
		OnProfile:      manager.onProfile,
	}
	existing := make(map[string]*cameraTile, len(manager.tiles))
	for _, tile := range manager.tiles {
		existing[tile.id] = tile
	}
	nextTiles := make([]*cameraTile, 0, len(manager.tiles))
	for _, device := range settings.Devices {
		if device.Password == "" {
			continue
		}
		groups := make(map[string][]profileSettings)
		var groupOrder []string
		for _, profile := range device.Profiles {
			key := profileGroupKey(profile)
			if _, exists := groups[key]; !exists {
				groupOrder = append(groupOrder, key)
			}
			groups[key] = append(groups[key], profile)
		}
		for _, key := range groupOrder {
			profiles := groups[key]
			var selected *profileSettings
			for index := range profiles {
				if profiles[index].Selected {
					selected = &profiles[index]
					break
				}
			}
			if selected == nil {
				continue
			}
			id := cameraTileID(device, *selected)
			if tile := existing[id]; tile != nil && tileCanBeReused(tile, device, profiles, preferences) {
				delete(existing, id)
				tile.updatePreferences(preferences)
				if tile.profile.Token != selected.Token {
					tile.selectProfile(*selected)
				}
				nextTiles = append(nextTiles, tile)
				continue
			}
			tile := newCameraTile(manager.parent, device, *selected, profiles, preferences, manager.toggleMaximize)
			nextTiles = append(nextTiles, tile)
		}
	}
	for _, tile := range existing {
		tile.stop()
	}
	manager.tiles = nextTiles
	manager.refresh()
}

func tileCanBeReused(tile *cameraTile, device deviceSettings, profiles []profileSettings, preferences tilePreferences) bool {
	if tile.device.ID != device.ID ||
		tile.device.Name != device.Name ||
		tile.device.Host != device.Host ||
		tile.device.Username != device.Username ||
		tile.device.Password != device.Password ||
		tile.device.DeviceServiceURL != device.DeviceServiceURL {
		return false
	}
	if tile.prefs.FPS != preferences.FPS ||
		tile.prefs.ResolutionMode != preferences.ResolutionMode {
		return false
	}
	return sameProfileCatalog(tile.profiles, profiles)
}

func sameProfileCatalog(first, second []profileSettings) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		a, b := first[index], second[index]
		if a.Token != b.Token ||
			a.Name != b.Name ||
			a.SourceToken != b.SourceToken ||
			a.RTSPURL != b.RTSPURL ||
			a.FallbackChannel != b.FallbackChannel ||
			a.Resolution != b.Resolution ||
			a.Codec != b.Codec ||
			a.PTZ != b.PTZ ||
			a.Audio != b.Audio ||
			a.Talk != b.Talk {
			return false
		}
	}
	return true
}

func (manager *gridManager) showIdle() {
	manager.stopTiles()
	manager.maximized = ""
	manager.container.Layout = layout.NewGridLayoutWithColumns(1)
	manager.container.Objects = []fyne.CanvasObject{
		widget.NewLabel(T("IdleHint")),
	}
	manager.container.Refresh()
}

func (manager *gridManager) refreshIdleTexts(idle bool) {
	if !idle || len(manager.tiles) > 0 {
		if len(manager.tiles) == 0 && manager.maximized == "" {
			manager.refresh()
		}
		return
	}
	manager.showIdle()
}

func (manager *gridManager) toggleMaximize(id string) {
	if manager.maximized == id {
		manager.maximized = ""
	} else {
		manager.maximized = id
	}
	manager.refresh()
}

func (manager *gridManager) refresh() {
	if len(manager.tiles) == 0 {
		manager.container.Layout = layout.NewGridLayoutWithColumns(1)
		manager.container.Objects = []fyne.CanvasObject{
			widget.NewLabel(T("NoProfilesMarked")),
		}
		manager.container.Refresh()
		return
	}
	if manager.maximized != "" {
		for _, tile := range manager.tiles {
			if tile.id == manager.maximized {
				manager.container.Layout = layout.NewGridLayoutWithColumns(1)
				manager.container.Objects = []fyne.CanvasObject{tile.root}
				manager.container.Refresh()
				return
			}
		}
		manager.maximized = ""
	}
	columns := gridColumns(len(manager.tiles))
	manager.container.Layout = layout.NewGridLayoutWithColumns(columns)
	objects := make([]fyne.CanvasObject, 0, len(manager.tiles))
	for _, tile := range manager.tiles {
		objects = append(objects, tile.root)
	}
	manager.container.Objects = objects
	manager.container.Refresh()
}

func (manager *gridManager) stopTiles() {
	for _, tile := range manager.tiles {
		tile.stop()
	}
	manager.tiles = nil
}

func (manager *gridManager) stop() {
	manager.stopTiles()
}

func gridColumns(tileCount int) int {
	if tileCount <= 1 {
		return 1
	}
	return int(math.Ceil(math.Sqrt(float64(tileCount))))
}
