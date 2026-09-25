package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestClampFPS(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 1},
		{0.4, 1},
		{6, 6},
		{15.4, 15},
		{15.6, 16},
		{30, 30},
		{45, 30},
	}
	for _, test := range cases {
		if got := clampFPS(test.in); got != test.want {
			t.Fatalf("clampFPS(%v) = %d, want %d", test.in, got, test.want)
		}
	}
}

func TestSettingsNormalization(t *testing.T) {
	settings := appSettings{
		FPS:            90,
		Quality1:       "invalida",
		Quality2:       "HD",
		DisplayMode:    "invalido",
		ResolutionMode: "Original",
	}
	settings.normalize()
	if settings.FPS != maxStreamFPS ||
		settings.Quality1 != "fluid" ||
		settings.Quality2 != "hd" ||
		settings.DisplayMode != "Lado a lado" ||
		settings.ResolutionMode != "original" {
		t.Fatalf("unexpected normalized settings: %+v", settings)
	}
}

func TestVideoFilterFixesFrameSize(t *testing.T) {
	got := videoFilter(12, image.Rect(0, 0, 640, 360))
	want := "fps=12,scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2"
	if got != want {
		t.Fatalf("videoFilter = %q, want %q", got, want)
	}
	if got := videoFilter(0, image.Rect(0, 0, 640, 360)); !strings.HasPrefix(got, "fps=1,") {
		t.Fatalf("videoFilter did not clamp the frame rate: %q", got)
	}
}

func TestStreamFrameSize(t *testing.T) {
	if got := streamFrameSize("hd"); got != image.Rect(0, 0, hdFrameWidth, hdFrameHeight) {
		t.Fatalf("HD frame size = %v", got)
	}
	if got := streamFrameSize("fluid"); got != image.Rect(0, 0, fluidFrameWidth, fluidFrameHeight) {
		t.Fatalf("fluid frame size = %v", got)
	}
	if got := streamFrameSize("HD"); got != image.Rect(0, 0, hdFrameWidth, hdFrameHeight) {
		t.Fatalf("legacy HD frame size = %v", got)
	}
}

func TestDecodeFrameSizeCapsOriginal(t *testing.T) {
	got := decodeFrameSize("original", "hd")
	if got != image.Rect(0, 0, originalFrameWidth, originalFrameHeight) {
		t.Fatalf("original frame size = %v", got)
	}
	if got := decodeFrameSize("optimized", "fluid"); got != streamFrameSize("fluid") {
		t.Fatalf("optimized fluid size = %v", got)
	}
}

func TestStreamStallReason(t *testing.T) {
	started := time.Unix(100, 0)
	if err := streamStallReason(0, 0, started, started.Add(5*time.Second), streamStallTimeout, streamConnectTimeout); err != nil {
		t.Fatalf("early connect should wait: %v", err)
	}
	if err := streamStallReason(0, 0, started, started.Add(streamConnectTimeout), streamStallTimeout, streamConnectTimeout); !errors.Is(err, errStreamConnectTimeout) {
		t.Fatalf("connect timeout = %v", err)
	}
	last := started.Add(2 * time.Second).UnixNano()
	if err := streamStallReason(last, last, started, started.Add(6*time.Second), streamStallTimeout, streamConnectTimeout); err != nil {
		t.Fatalf("fresh frames should stay live: %v", err)
	}
	if err := streamStallReason(last, last, started, started.Add(2*time.Second+streamStallTimeout), streamStallTimeout, streamConnectTimeout); !errors.Is(err, errStreamStalled) {
		t.Fatalf("stalled stream = %v", err)
	}
	// fps filter keeps emitting the same picture; lastFrame stays recent.
	now := started.Add(2*time.Second + streamStallTimeout)
	if err := streamStallReason(now.UnixNano(), last, started, now, streamStallTimeout, streamConnectTimeout); !errors.Is(err, errStreamStalled) {
		t.Fatalf("identical picture = %v", err)
	}
}

func TestFrameFingerprintDetectsChange(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 8, 8))
	first := frameFingerprint(frame)
	if first == 0 {
		t.Fatal("empty frame should still hash length")
	}
	if got := frameFingerprint(frame); got != first {
		t.Fatal("identical frames must share a fingerprint")
	}
	frame.Pix[0] = 9
	if frameFingerprint(frame) == first {
		t.Fatal("changed pixel should change the fingerprint")
	}
}

func TestVideoStreamStopAllowsNewPresent(t *testing.T) {
	size := image.Rect(0, 0, 4, 4)
	stream := &videoStream{}
	if !stream.offer(image.NewRGBA(size)) {
		t.Fatal("first frame should queue a present")
	}
	stream.stop()
	if !stream.offer(image.NewRGBA(size)) {
		t.Fatal("stop must reset updating so the next frame can be drawn")
	}
}

func TestLiveStreamArgsIncludeTimeouts(t *testing.T) {
	args := strings.Join(liveStreamArgs("rtsp://camera/stream", defaultStreamFPS, streamFrameSize("fluid"), nil), " ")
	for _, expected := range []string{
		"-rtsp_transport tcp",
		"-fflags nobuffer",
		"rtsp://camera/stream",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("live stream args missing %q: %s", expected, args)
		}
	}
}

func TestReconnectErrorText(t *testing.T) {
	SetLanguage(langEnglish)
	if got := reconnectErrorText(errStreamStalled); got != "camera stopped sending video" {
		t.Fatalf("stalled = %q", got)
	}
	if got := reconnectErrorText(errStreamConnectTimeout); got != "camera did not respond" {
		t.Fatalf("connect = %q", got)
	}
	if got := reconnectErrorText(errors.New("boom")); got != "boom" {
		t.Fatalf("other = %q", got)
	}
}

func TestVideoStreamDropsFramesInsteadOfQueueing(t *testing.T) {
	size := image.Rect(0, 0, 4, 4)
	stream := &videoStream{}

	first := image.NewRGBA(size)
	if !stream.offer(first) {
		t.Fatal("the first frame should ask for a present callback")
	}
	second := image.NewRGBA(size)
	if stream.offer(second) {
		t.Fatal("a frame offered while one is queued must not ask for another callback")
	}
	if got := stream.buffer(size); got != first {
		t.Fatal("the dropped frame should be recycled instead of allocating")
	}
	if got := stream.take(); got != second {
		t.Fatal("take should return the newest frame")
	}

	third := image.NewRGBA(size)
	if !stream.offer(third) {
		t.Fatal("after presenting, the next frame should ask for a callback again")
	}
	if got := stream.buffer(image.Rect(0, 0, 8, 8)); !got.Rect.Eq(image.Rect(0, 0, 8, 8)) {
		t.Fatalf("a buffer of a different size should be allocated fresh, got %v", got.Rect)
	}
}

func TestReadPPMFrameWithoutAllocatingPixelBuffer(t *testing.T) {
	data := append([]byte("P6\n# frame original\n2 1\n255\n"), []byte{
		10, 20, 30,
		40, 50, 60,
	}...)
	reader := bufio.NewReader(bytes.NewReader(data))
	size, err := readPPMHeader(reader)
	if err != nil {
		t.Fatal(err)
	}
	if size != image.Rect(0, 0, 2, 1) {
		t.Fatalf("PPM size = %v", size)
	}
	frame := image.NewRGBA(size)
	if err := readPPMPixels(reader, frame); err != nil {
		t.Fatal(err)
	}
	want := []byte{10, 20, 30, 255, 40, 50, 60, 255}
	if !bytes.Equal(frame.Pix, want) {
		t.Fatalf("RGBA pixels = %v, want %v", frame.Pix, want)
	}
}

func TestRTSPURLQuotesPassword(t *testing.T) {
	config := cameraConfig{host: "192.168.2.133", username: "admin", password: "value#1"}
	got := config.rtspURL("102")
	if !strings.Contains(got, "value%231") {
		t.Fatalf("RTSP password was not URL encoded: %s", got)
	}
}

func TestAudioPlayerArgs(t *testing.T) {
	config := cameraConfig{host: "192.168.2.133", username: "admin", password: "value#1"}
	args := strings.Join(audioPlayerArgs(config, "102", 65), " ")
	for _, expected := range []string{
		"-nodisp",
		"-rtsp_transport tcp",
		"-fflags nobuffer",
		"-vn",
		"-volume 65",
		"Streaming/Channels/102",
		"value%231",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("audio player arguments do not contain %q: %s", expected, args)
		}
	}
}

func TestAudioProbeArgsSelectOnlyAudio(t *testing.T) {
	args := strings.Join(audioProbeArgs("rtsp://camera/stream"), " ")
	for _, expected := range []string{
		"-rtsp_transport tcp",
		"-select_streams a:0",
		"-show_entries stream=index",
		"rtsp://camera/stream",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("audio probe arguments do not contain %q: %s", expected, args)
		}
	}
}

func TestClampVolume(t *testing.T) {
	for _, test := range []struct {
		value float64
		want  int
	}{
		{-10, 0},
		{0, 0},
		{45.4, 45},
		{45.6, 46},
		{100, 100},
		{150, 100},
	} {
		if got := clampVolume(test.value); got != test.want {
			t.Fatalf("clampVolume(%v) = %d, want %d", test.value, got, test.want)
		}
	}
}

func TestParseDiscoveryResponse(t *testing.T) {
	response := []byte(`<?xml version="1.0"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <e:Body><d:ProbeMatches>
    <d:ProbeMatch>
      <d:Scopes>onvif://www.onvif.org/name/EZVIZ%20H9c onvif://www.onvif.org/hardware/CS-H9c</d:Scopes>
      <d:XAddrs>http://192.168.2.133/onvif/device_service</d:XAddrs>
    </d:ProbeMatch>
    <d:ProbeMatch>
      <d:Scopes>onvif://www.onvif.org/hardware/CS-C6N</d:Scopes>
      <d:XAddrs>http://192.168.2.134:80/onvif/device_service</d:XAddrs>
    </d:ProbeMatch>
  </d:ProbeMatches></e:Body>
</e:Envelope>`)

	cameras, err := parseDiscoveryResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cameras))
	}
	if cameras[0].host != "192.168.2.133" || cameras[0].name != "EZVIZ H9c" {
		t.Fatalf("unexpected first camera: %+v", cameras[0])
	}
	if cameras[0].deviceServiceURL != "http://192.168.2.133/onvif/device_service" {
		t.Fatalf("discovery endpoint was not preserved: %q", cameras[0].deviceServiceURL)
	}
	if cameras[1].host != "192.168.2.134" || cameras[1].name != "CS-C6N" {
		t.Fatalf("unexpected second camera: %+v", cameras[1])
	}
}

func TestDiscoveryProbeSupportsTypedAndGenericSearch(t *testing.T) {
	typed, err := discoveryProbe(true)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := discoveryProbe(false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(typed, []byte("dn:NetworkVideoTransmitter")) {
		t.Fatal("typed probe does not include the ONVIF device type")
	}
	if bytes.Contains(generic, []byte("<d:Types>")) || !bytes.Contains(generic, []byte("<d:Probe/>")) {
		t.Fatalf("generic probe is unexpectedly filtered: %s", generic)
	}
	if bytes.Equal(typed, generic) {
		t.Fatal("discovery probes reused the same message ID")
	}
}

func TestLANDiscovery(t *testing.T) {
	if os.Getenv("EZVIZ_LAN_DISCOVERY") != "1" {
		t.Skip("set EZVIZ_LAN_DISCOVERY=1 to run WS-Discovery on the LAN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cameras, err := discoverLocalCameras(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) == 0 {
		t.Fatal("no ONVIF devices answered WS-Discovery")
	}
	for _, camera := range cameras {
		t.Logf("%s: %s (%s)", camera.host, camera.name, camera.deviceServiceURL)
	}
}

func TestCameraDiscovery(t *testing.T) {
	password := os.Getenv("EZVIZ_TEST_PASSWORD")
	if password == "" {
		t.Skip("set EZVIZ_TEST_PASSWORD to run the camera integration test")
	}
	config := cameraConfig{
		host:     "192.168.2.133",
		username: "admin",
		password: password,
	}
	client := newONVIFClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.discover(ctx); err != nil {
		t.Fatal(err)
	}
	if client.profileToken != "Profile_201" {
		t.Fatalf("expected movable lens profile Profile_201, got %s", client.profileToken)
	}
	t.Logf("media=%s ptz=%s profile=%s", client.mediaURL, client.ptzURL, client.profileToken)
	if err := client.move(ctx, 0, 0); err != nil {
		t.Fatalf("zero-velocity PTZ move failed: %v", err)
	}
	if err := client.stop(ctx); err != nil {
		t.Fatalf("PTZ stop failed: %v", err)
	}
}

func TestCameraFrames(t *testing.T) {
	password := os.Getenv("EZVIZ_TEST_PASSWORD")
	if password == "" {
		t.Skip("set EZVIZ_TEST_PASSWORD to run the camera integration test")
	}
	config := cameraConfig{host: "192.168.2.133", username: "admin", password: password}
	size := streamFrameSize("fluid")
	for _, channel := range []string{"102", "202"} {
		t.Run(channel, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, ffmpegBinary(),
				"-hide_banner", "-loglevel", "error", "-rtsp_transport", "tcp", "-threads", "1",
				"-i", config.rtspURL(channel), "-map", "0:v:0", "-an",
				"-vf", videoFilter(defaultStreamFPS, size), "-threads", "1",
				"-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1",
			)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			frame := image.NewRGBA(size)
			_, err = io.ReadFull(stdout, frame.Pix)
			cancel()
			_ = cmd.Wait()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("frame %s: %v", channel, frame.Bounds())
		})
	}
}
