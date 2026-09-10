package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestGridColumns(t *testing.T) {
	tests := map[int]int{0: 1, 1: 1, 2: 2, 3: 2, 4: 2, 5: 3, 9: 3, 10: 4}
	for count, expected := range tests {
		if actual := gridColumns(count); actual != expected {
			t.Fatalf("gridColumns(%d) = %d; want %d", count, actual, expected)
		}
	}
}

func TestDoubleTapWidget(t *testing.T) {
	tapped := 0
	view := newDoubleTapWidget(widget.NewLabel("camera"), func() { tapped++ })
	test.DoubleTap(view)
	if tapped != 1 {
		t.Fatalf("double tap callback ran %d times", tapped)
	}
}

func TestMergeMediaProfilesPreservesSelectionByChannel(t *testing.T) {
	existing := []profileSettings{{
		Token: "102", FallbackChannel: "102", Selected: true,
	}}
	discovered := []onvifMediaProfile{{
		Token: "Profile_1", Name: "Substream", RTSPURL: "rtsp://camera/Streaming/Channels/102",
		Width: 640, Height: 360, Codec: "H264", HasAudio: true,
	}}
	merged := mergeMediaProfiles(existing, discovered)
	if len(merged) != 1 || !merged[0].Selected || merged[0].FallbackChannel != "102" {
		t.Fatalf("legacy selection was not preserved: %+v", merged)
	}
	if merged[0].Resolution != "640x360" || !merged[0].Audio {
		t.Fatalf("profile metadata was not mapped: %+v", merged[0])
	}
}

func TestSettingsDisablePTZForFixedLens(t *testing.T) {
	settings := appSettings{
		FPS: 6, Volume: 100, ResolutionMode: "Otimizada",
		Devices: []deviceSettings{{
			ID: "camera", Host: "camera", Profiles: []profileSettings{
				{Token: "101", SourceToken: "VideoSource_1", PTZ: true},
				{Token: "201", SourceToken: "VideoSource_2", PTZ: true},
			},
		}},
	}
	settings.normalize()
	if settings.Devices[0].Profiles[0].PTZ {
		t.Fatal("fixed wide-angle lens kept PTZ controls")
	}
	if !settings.Devices[0].Profiles[1].PTZ {
		t.Fatal("movable lens lost PTZ controls")
	}
}

func TestProfileGroupKeyGroupsQualityByVideoSource(t *testing.T) {
	hd := profileSettings{Token: "101", SourceToken: "VideoSource_1"}
	fluid := profileSettings{Token: "102", SourceToken: "VideoSource_1"}
	movable := profileSettings{Token: "201", SourceToken: "VideoSource_2"}
	if profileGroupKey(hd) != profileGroupKey(fluid) {
		t.Fatal("qualities from the same lens were split into separate tiles")
	}
	if profileGroupKey(hd) == profileGroupKey(movable) {
		t.Fatal("different lenses were merged into one tile")
	}
}

func TestTileReuseIgnoresSelectionOnlyChanges(t *testing.T) {
	device := deviceSettings{
		ID: "camera", Name: "Camera", Host: "192.0.2.10",
		Username: "admin", Password: "secret",
	}
	profiles := []profileSettings{
		{Token: "101", SourceToken: "VideoSource_1", RTSPURL: "rtsp://camera/101", Selected: true},
		{Token: "102", SourceToken: "VideoSource_1", RTSPURL: "rtsp://camera/102"},
	}
	preferences := tilePreferences{FPS: 6, ResolutionMode: "Otimizada", Microphone: "Mic"}
	tile := &cameraTile{device: device, profiles: append([]profileSettings(nil), profiles...), prefs: preferences}
	profiles[0].Selected = false
	profiles[1].Selected = true
	if !tileCanBeReused(tile, device, profiles, preferences) {
		t.Fatal("changing only the selected quality would reload the whole tile")
	}
	changedMicrophone := preferences
	changedMicrophone.Microphone = "Outro microfone"
	if !tileCanBeReused(tile, device, profiles, changedMicrophone) {
		t.Fatal("changing the microphone would reload the video tile")
	}
	profiles[1].RTSPURL = "rtsp://camera/new-stream"
	if tileCanBeReused(tile, device, profiles, preferences) {
		t.Fatal("tile was reused after its stream catalog changed")
	}
}

func TestDeviceStreamURLAddsCredentials(t *testing.T) {
	device := deviceSettings{Host: "camera.local", Username: "viewer", Password: "p@ss word"}
	raw := "rtsp://camera.local/live"
	streamURL := device.streamURL(profileSettings{RTSPURL: raw})
	if !strings.Contains(streamURL, "viewer:p%40ss%20word@") {
		t.Fatalf("credentials were not safely escaped: %s", streamURL)
	}
}

func TestDiscoverMediaProfilesAndStreamURI(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(request.URL.Path, "device"):
			fmt.Fprintf(writer, `<Envelope><Body><GetCapabilitiesResponse><Capabilities><Media><XAddr>%s/media</XAddr></Media><PTZ><XAddr>%s/ptz</XAddr></PTZ></Capabilities></GetCapabilitiesResponse></Body></Envelope>`, server.URL, server.URL)
		case strings.Contains(request.URL.Path, "media") && strings.Contains(request.Header.Get("Content-Type"), "GetProfiles"):
			fmt.Fprint(writer, `<Envelope><Body><GetProfilesResponse><Profiles token="profile-main"><Name>Principal</Name><VideoSourceConfiguration><SourceToken>video-1</SourceToken><Bounds width="1920" height="1080"/></VideoSourceConfiguration><VideoEncoderConfiguration><Encoding>H264</Encoding><Resolution><Width>1920</Width><Height>1080</Height></Resolution></VideoEncoderConfiguration><PTZConfiguration/><AudioSourceConfiguration/><AudioEncoderConfiguration/><AudioOutputConfiguration/><AudioDecoderConfiguration/></Profiles></GetProfilesResponse></Body></Envelope>`)
		case strings.Contains(request.URL.Path, "media"):
			fmt.Fprint(writer, `<Envelope><Body><GetStreamUriResponse><MediaUri><Uri>rtsp://camera/Streaming/Channels/101</Uri></MediaUri></GetStreamUriResponse></Body></Envelope>`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newONVIFClient(cameraConfig{
		host:             "camera",
		username:         "admin",
		password:         "secret",
		deviceServiceURL: server.URL + "/device",
	})
	profiles, err := client.discoverMediaProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles", len(profiles))
	}
	profile := profiles[0]
	if profile.Token != "profile-main" || profile.RTSPURL == "" || profile.Width != 1920 ||
		!profile.HasPTZ || !profile.HasAudio || !profile.CanTalk {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestConfiguredDevicesOnLAN(t *testing.T) {
	if os.Getenv("EZVIZ_LAN_TEST") != "1" {
		t.Skip("set EZVIZ_LAN_TEST=1 to probe saved ONVIF devices")
	}
	settings, err := loadAppSettings()
	if err != nil {
		t.Fatal(err)
	}
	reachable := 0
	for _, device := range settings.Devices {
		if device.Password == "" {
			t.Logf("%s (%s): sem senha, ignorado", device.Name, device.Host)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		profiles, discoverErr := newONVIFClient(device.cameraConfig()).discoverMediaProfiles(ctx)
		cancel()
		if discoverErr != nil {
			t.Logf("%s (%s): offline ou ONVIF indisponivel: %v", device.Name, device.Host, discoverErr)
			continue
		}
		reachable++
		t.Logf("%s (%s): %d perfis ONVIF", device.Name, device.Host, len(profiles))
		for _, profile := range profiles {
			t.Logf("  %s source=%s channel=%s ptz=%v", profile.Token, profile.SourceToken, streamChannelFromURL(profile.RTSPURL), profile.HasPTZ)
		}
	}
	if reachable == 0 {
		t.Fatal("nenhum dispositivo salvo respondeu na LAN")
	}
}
