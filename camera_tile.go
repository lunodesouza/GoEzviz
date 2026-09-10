package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type tilePreferences struct {
	FPS            int
	ResolutionMode string
	Volume         int
	Microphone     string
	OnVolume       func(int)
	OnProfile      func(deviceID, sourceToken, profileToken string)
}

type doubleTapWidget struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	onDouble func()
}

func newDoubleTapWidget(content fyne.CanvasObject, onDouble func()) *doubleTapWidget {
	result := &doubleTapWidget{content: content, onDouble: onDouble}
	result.ExtendBaseWidget(result)
	return result
}

func (view *doubleTapWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(view.content)
}

func (view *doubleTapWidget) DoubleTapped(*fyne.PointEvent) {
	if view.onDouble != nil {
		view.onDouble()
	}
}

type cameraTile struct {
	id       string
	device   deviceSettings
	profile  profileSettings
	profiles []profileSettings
	prefs    tilePreferences
	parent   context.Context

	root           *doubleTapWidget
	stream         *videoStream
	status         *widget.Label
	title          *widget.Label
	audio          *cameraAudio
	talk           *cameraTalk
	loading        *widget.Activity
	loadingOverlay *fyne.Container

	soundButton   *widget.Button
	talkButton    *widget.Button
	micIndicator  *canvas.Circle
	volume        int
	audioVerified string

	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	generation uint64
	backoff    time.Duration
	client     *onvifClient
	ptzMu      sync.Mutex
}

func newCameraTile(
	parent context.Context,
	device deviceSettings,
	profile profileSettings,
	profiles []profileSettings,
	prefs tilePreferences,
	onDouble func(string),
) *cameraTile {
	tile := &cameraTile{
		id:       cameraTileID(device, profile),
		device:   device,
		profile:  profile,
		profiles: append([]profileSettings(nil), profiles...),
		prefs:    prefs,
		parent:   parent,
		stream:   newVideoStream(),
		status:   widget.NewLabel(T("WaitingConnection")),
		audio:    &cameraAudio{},
		talk:     &cameraTalk{},
		volume:   prefs.Volume,
		backoff:  5 * time.Second,
	}
	tile.stream.view.SetMinSize(fyne.NewSize(200, 112))
	tile.status.Wrapping = fyne.TextWrapWord
	tile.title = widget.NewLabelWithStyle(
		device.Name+" - "+profile.Name,
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	headerControls := tile.buildMediaControls()
	header := container.NewBorder(nil, nil, tile.title, headerControls, tile.buildQualitySelector())
	tile.loading = widget.NewActivity()
	tile.loadingOverlay = container.NewCenter(container.NewVBox(
		container.NewCenter(tile.loading),
		widget.NewLabel(T("LoadingImage")),
	))
	videoObjects := []fyne.CanvasObject{tile.stream.view, tile.loadingOverlay}
	if profile.PTZ {
		videoObjects = append(videoObjects, tile.buildPTZControls())
	}
	video := container.NewStack(videoObjects...)
	content := container.NewBorder(header, tile.status, nil, nil, video)
	tile.root = newDoubleTapWidget(content, func() { onDouble(tile.id) })
	tile.start(parent)
	return tile
}

func cameraTileID(device deviceSettings, profile profileSettings) string {
	return device.ID + "::" + profileGroupKey(profile)
}

func (tile *cameraTile) buildMediaControls() fyne.CanvasObject {
	objects := []fyne.CanvasObject{}
	if tile.profile.Audio {
		tile.soundButton = widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), tile.toggleAudio)
		volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", tile.volume))
		volumeSlider := widget.NewSlider(0, 100)
		volumeSlider.Step = 1
		volumeSlider.SetValue(float64(tile.volume))
		volumeSlider.OnChanged = func(value float64) {
			volumeLabel.SetText(fmt.Sprintf("%d%%", clampVolume(value)))
		}
		volumeSlider.OnChangeEnded = func(value float64) {
			tile.volume = clampVolume(value)
			if tile.prefs.OnVolume != nil {
				tile.prefs.OnVolume(tile.volume)
			}
			if tile.audio.isPlaying() {
				tile.audio.stop()
				tile.startAudio()
			}
		}
		objects = append(objects,
			tile.soundButton,
			container.NewGridWrap(fyne.NewSize(90, volumeSlider.MinSize().Height), volumeSlider),
			volumeLabel,
		)
	}
	if tile.profile.Talk {
		tile.talkButton = widget.NewButtonWithIcon(T("Talk"), theme.MediaRecordIcon(), tile.toggleTalk)
		tile.micIndicator = canvas.NewCircle(microphoneInactiveColor)
		tile.talk.setActivityCallback(tile.setMicrophoneActivity)
		objects = append(objects, container.NewHBox(
			container.NewGridWrap(fyne.NewSize(12, 12), tile.micIndicator),
			tile.talkButton,
		))
	}
	if len(objects) == 0 {
		return layout.NewSpacer()
	}
	return container.NewHBox(objects...)
}

var (
	microphoneInactiveColor = color.NRGBA{R: 105, G: 105, B: 105, A: 255}
	microphoneActiveColor   = color.NRGBA{R: 35, G: 205, B: 90, A: 255}
)

func (tile *cameraTile) setMicrophoneActivity(active bool) {
	fyne.Do(func() {
		if tile.micIndicator == nil {
			return
		}
		if active {
			tile.micIndicator.FillColor = microphoneActiveColor
		} else {
			tile.micIndicator.FillColor = microphoneInactiveColor
		}
		tile.micIndicator.Refresh()
	})
}

func (tile *cameraTile) buildQualitySelector() fyne.CanvasObject {
	if len(tile.profiles) == 0 {
		tile.profiles = []profileSettings{tile.profile}
	}
	options := make([]string, 0, len(tile.profiles))
	profilesByOption := make(map[string]profileSettings, len(tile.profiles))
	selected := ""
	for _, profile := range tile.profiles {
		option := qualityLabel(profileFrameSize(profile))
		if profile.Resolution != "" {
			option += " - " + profile.Resolution
		}
		if _, exists := profilesByOption[option]; exists {
			option += " - " + profile.Name
		}
		options = append(options, option)
		profilesByOption[option] = profile
		if profile.Token == tile.profile.Token {
			selected = option
		}
	}
	quality := widget.NewSelect(options, nil)
	quality.SetSelected(selected)
	quality.OnChanged = func(option string) {
		profile, ok := profilesByOption[option]
		if !ok || profile.Token == tile.profile.Token {
			return
		}
		tile.selectProfile(profile)
	}
	return container.NewHBox(
		widget.NewLabel(T("Quality")),
		container.NewGridWrap(fyne.NewSize(170, quality.MinSize().Height), quality),
	)
}

func (tile *cameraTile) selectProfile(profile profileSettings) {
	tile.stop()
	tile.profile = profile
	tile.audioVerified = ""
	tile.title.SetText(tile.device.Name + " - " + profile.Name)
	if tile.soundButton != nil {
		tile.soundButton.SetIcon(theme.VolumeMuteIcon())
	}
	if tile.talkButton != nil {
		tile.talkButton.SetText(T("Talk"))
	}
	if tile.prefs.OnProfile != nil {
		tile.prefs.OnProfile(tile.device.ID, profile.SourceToken, profile.Token)
	}
	tile.start(tile.parent)
}

func (tile *cameraTile) updatePreferences(preferences tilePreferences) {
	microphoneChanged := tile.prefs.Microphone != preferences.Microphone
	tile.prefs.Microphone = preferences.Microphone
	tile.prefs.OnVolume = preferences.OnVolume
	tile.prefs.OnProfile = preferences.OnProfile
	if microphoneChanged && tile.talk.isActive() {
		tile.talk.stop()
		if tile.talkButton != nil {
			tile.talkButton.SetText(T("Talk"))
		}
		tile.setStatus(T("MicChanged"))
	}
}

func (tile *cameraTile) buildPTZControls() fyne.CanvasObject {
	up := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() { tile.move(0, 0.55) })
	down := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() { tile.move(0, -0.55) })
	left := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { tile.move(-0.55, 0) })
	right := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { tile.move(0.55, 0) })
	stop := widget.NewButtonWithIcon("", theme.MediaStopIcon(), tile.stopPTZ)
	controls := container.NewGridWithColumns(3,
		layout.NewSpacer(), up, layout.NewSpacer(),
		left, stop, right,
		layout.NewSpacer(), down, layout.NewSpacer(),
	)
	return container.NewPadded(container.NewBorder(
		container.NewHBox(layout.NewSpacer(), controls),
		nil, nil, nil, layout.NewSpacer(),
	))
}

func (tile *cameraTile) start(parent context.Context) {
	tile.stop()
	ctx, cancel := context.WithCancel(parent)
	tile.mu.Lock()
	tile.ctx = ctx
	tile.cancel = cancel
	tile.generation++
	generation := tile.generation
	tile.backoff = 5 * time.Second
	tile.mu.Unlock()
	tile.setStatus(T("Connecting"))
	tile.setLoading(true)
	go tile.prepareONVIF(ctx, generation)
	tile.startVideoAttempt(ctx, generation)
}

func (tile *cameraTile) startVideoAttempt(ctx context.Context, generation uint64) {
	if ctx.Err() != nil || !tile.isCurrent(generation) {
		return
	}
	streamURL := tile.device.streamURL(tile.profile)
	if streamURL == "" {
		tile.setStatus(T("NoRTSP"))
		tile.setLoading(false)
		return
	}
	quality := profileFrameSize(tile.profile)
	tile.stream.startURLWithReady(
		ctx,
		streamURL,
		tile.prefs.FPS,
		streamFrameSize(quality),
		normalizeResolutionMode(tile.prefs.ResolutionMode) == "original",
		func() {
			if !tile.isCurrent(generation) {
				return
			}
			tile.mu.Lock()
			tile.backoff = 5 * time.Second
			tile.mu.Unlock()
			tile.setLoading(false)
			tile.setStatus(tile.profile.Resolution + " " + tile.profile.Codec)
		},
		func(err error) {
			if ctx.Err() != nil || !tile.isCurrent(generation) {
				return
			}
			tile.scheduleReconnect(ctx, generation, err)
		},
	)
}

func (tile *cameraTile) scheduleReconnect(ctx context.Context, generation uint64, streamErr error) {
	tile.mu.Lock()
	delay := tile.backoff
	tile.backoff = time.Duration(math.Min(float64(tile.backoff*2), float64(60*time.Second)))
	tile.mu.Unlock()
	tile.setLoading(true)
	tile.setStatus(T("OfflineRetry", map[string]any{"Error": streamErr.Error(), "Delay": delay.String()}))
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			tile.startVideoAttempt(ctx, generation)
		}
	}()
}

func (tile *cameraTile) prepareONVIF(ctx context.Context, generation uint64) {
	client := newONVIFClient(tile.device.cameraConfig())
	profiles, err := client.discoverMediaProfiles(ctx)
	if err != nil || !tile.isCurrent(generation) {
		return
	}
	for _, profile := range profiles {
		if profile.Token == tile.profile.Token ||
			(tile.profile.FallbackChannel != "" && streamChannelFromURL(profile.RTSPURL) == tile.profile.FallbackChannel) {
			client.profileToken = profile.Token
			break
		}
	}
	tile.mu.Lock()
	if tile.generation == generation {
		tile.client = client
	}
	tile.mu.Unlock()
}

func (tile *cameraTile) toggleAudio() {
	if tile.audio.isPlaying() {
		tile.audio.stop()
		tile.soundButton.SetIcon(theme.VolumeMuteIcon())
		tile.setStatus(T("AudioOff"))
		return
	}
	tile.startAudio()
}

func (tile *cameraTile) startAudio() {
	streamURL := tile.device.streamURL(tile.profile)
	if tile.audioVerified == tile.profile.Token {
		tile.playAudio(streamURL)
		return
	}
	tile.mu.Lock()
	ctx := tile.ctx
	generation := tile.generation
	profileToken := tile.profile.Token
	tile.mu.Unlock()
	if ctx == nil {
		return
	}
	tile.soundButton.Disable()
	tile.setStatus(T("CheckingAudio"))
	go func() {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		available, err := audioStreamAvailable(probeCtx, streamURL)
		cancel()
		fyne.Do(func() {
			tile.soundButton.Enable()
			if !tile.isCurrent(generation) || tile.profile.Token != profileToken {
				return
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					tile.status.SetText(T("AudioCheckFailed"))
				}
				return
			}
			if !available {
				tile.soundButton.SetIcon(theme.VolumeMuteIcon())
				tile.status.SetText(T("AudioUnavailable"))
				return
			}
			tile.audioVerified = profileToken
			tile.playAudio(streamURL)
		})
	}()
}

func (tile *cameraTile) playAudio(streamURL string) {
	err := tile.audio.startURL(context.Background(), streamURL, tile.volume, func(err error) {
		fyne.Do(func() {
			tile.soundButton.SetIcon(theme.VolumeMuteIcon())
			tile.status.SetText(T("AudioError", map[string]string{"Error": err.Error()}))
		})
	})
	if err != nil {
		tile.setStatus(T("AudioError", map[string]string{"Error": err.Error()}))
		return
	}
	tile.soundButton.SetIcon(theme.VolumeUpIcon())
	tile.setStatus(T("AudioOn"))
}

func (tile *cameraTile) toggleTalk() {
	if tile.talk.isActive() {
		tile.talk.stop()
		tile.talkButton.SetText(T("Talk"))
		tile.setStatus(T("MicOff"))
		return
	}
	tile.mu.Lock()
	client := tile.client
	ctx := tile.ctx
	tile.mu.Unlock()
	if client == nil {
		tile.setStatus(T("WaitONVIF"))
		return
	}
	if ctx == nil {
		return
	}
	streamURL := tile.device.streamURL(tile.profile)
	err := tile.talk.startURL(
		ctx,
		streamURL,
		tile.prefs.Microphone,
		func(talkCtx context.Context) error {
			_ = client.setAudioOutputLevel(talkCtx, 100)
			return nil
		},
		func() { tile.setStatus(T("MicActive")) },
		func(err error) {
			fyne.Do(func() {
				tile.talkButton.SetText(T("Talk"))
				tile.status.SetText(T("MicError", map[string]string{"Error": err.Error()}))
			})
		},
	)
	if err != nil {
		tile.setStatus(T("MicError", map[string]string{"Error": err.Error()}))
		return
	}
	tile.talkButton.SetText(T("Stop"))
	tile.setStatus(T("PreparingMic"))
}

func (tile *cameraTile) move(x, y float64) {
	tile.mu.Lock()
	client := tile.client
	tile.mu.Unlock()
	if client == nil || client.profileToken == "" {
		tile.setStatus(T("PTZNotReady"))
		return
	}
	go func() {
		tile.ptzMu.Lock()
		defer tile.ptzMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := client.move(ctx, x, y); err != nil {
			tile.setStatus(T("PTZError", map[string]string{"Error": err.Error()}))
			return
		}
		time.Sleep(320 * time.Millisecond)
		if err := client.stop(ctx); err != nil {
			tile.setStatus(T("PTZStopError", map[string]string{"Error": err.Error()}))
		}
	}()
}

func (tile *cameraTile) stopPTZ() {
	tile.mu.Lock()
	client := tile.client
	tile.mu.Unlock()
	if client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.stop(ctx); err != nil {
			tile.setStatus(T("PTZStopError", map[string]string{"Error": err.Error()}))
		}
	}()
}

func (tile *cameraTile) setStatus(text string) {
	fyne.Do(func() { tile.status.SetText(text) })
}

func (tile *cameraTile) setLoading(loading bool) {
	fyne.Do(func() {
		if tile.loading == nil || tile.loadingOverlay == nil {
			return
		}
		if loading {
			tile.loadingOverlay.Show()
			tile.loading.Start()
			return
		}
		tile.loading.Stop()
		tile.loadingOverlay.Hide()
	})
}

func (tile *cameraTile) isCurrent(generation uint64) bool {
	tile.mu.Lock()
	defer tile.mu.Unlock()
	return tile.generation == generation
}

func (tile *cameraTile) stop() {
	tile.mu.Lock()
	tile.generation++
	cancel := tile.cancel
	tile.ctx = nil
	tile.cancel = nil
	tile.client = nil
	tile.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	tile.stream.stop()
	tile.audio.stop()
	tile.talk.stop()
	tile.setLoading(false)
}
