package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
)

func TestG711HasVoice(t *testing.T) {
	tests := []struct {
		name    string
		samples []byte
		mulaw   bool
		want    bool
	}{
		{"mulaw silence", filledBytes(160, 0xff), true, false},
		{"mulaw voice", filledBytes(160, 0x00), true, true},
		{"alaw silence", filledBytes(160, 0xd5), false, false},
		{"alaw voice", filledBytes(160, 0x2a), false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := g711HasVoice(test.samples, test.mulaw); got != test.want {
				t.Fatalf("g711HasVoice() = %v; want %v", got, test.want)
			}
		})
	}
}

func TestTalkSetupMediasUsesBackchannelOnly(t *testing.T) {
	video := &description.Media{Type: description.MediaTypeVideo}
	incoming := &description.Media{Type: description.MediaTypeAudio}
	back := &description.Media{Type: description.MediaTypeAudio, IsBackChannel: true, Formats: []format.Format{&format.G711{MULaw: true}}}
	session := &description.Session{Medias: []*description.Media{video, incoming, back}}
	medias := talkSetupMedias(session, back)
	if len(medias) != 1 || medias[0] != back {
		t.Fatalf("unexpected talk medias: %+v", medias)
	}
}

func TestTalkPacerSpacesPackets(t *testing.T) {
	pacer := newTalkPacer(20 * time.Millisecond)
	start := time.Now()
	for range 5 {
		if !pacer.wait(context.Background()) {
			t.Fatal("pacer aborted unexpectedly")
		}
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("pacer released 5 packets too quickly: %v", elapsed)
	}
}

func TestTalkPacerResynchronisesAfterStall(t *testing.T) {
	pacer := newTalkPacer(20 * time.Millisecond)
	pacer.next = time.Now().Add(-2 * time.Second)
	start := time.Now()
	if !pacer.wait(context.Background()) {
		t.Fatal("pacer aborted unexpectedly")
	}
	if elapsed := time.Since(start); elapsed > 40*time.Millisecond {
		t.Fatalf("pacer did not resynchronise: %v", elapsed)
	}
}

func TestTalkPacerStopsOnCancelledContext(t *testing.T) {
	pacer := newTalkPacer(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	pacer.next = time.Now().Add(time.Second)
	cancel()
	if pacer.wait(ctx) {
		t.Fatal("pacer kept going after cancellation")
	}
}

func TestMicrophoneErrorMapsPermission(t *testing.T) {
	original := openMicPrivacySettings
	opened := false
	openMicPrivacySettings = func() error {
		opened = true
		return nil
	}
	t.Cleanup(func() { openMicPrivacySettings = original })

	err := microphoneError(errors.New("avfoundation permission denied"))
	if err == nil {
		t.Fatal("expected mapped permission error")
	}
	if !opened {
		t.Fatal("expected microphone privacy settings to open")
	}
	message := strings.ToLower(err.Error())
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(message, "macos") {
			t.Fatalf("permission error was not mapped for macOS: %v", err)
		}
	case "windows":
		if !strings.Contains(message, "windows") {
			t.Fatalf("permission error was not mapped for Windows: %v", err)
		}
	default:
		if !strings.Contains(message, "microfone") {
			t.Fatalf("permission error was not mapped: %v", err)
		}
	}
}

func TestPCMS16leHasVoice(t *testing.T) {
	silence := make([]byte, 256)
	if pcmS16leHasVoice(silence) {
		t.Fatal("silence was detected as voice")
	}
	voice := make([]byte, 256)
	for i := 0; i < len(voice); i += 2 {
		voice[i] = 0x00
		voice[i+1] = 0x20 // 8192
	}
	if !pcmS16leHasVoice(voice) {
		t.Fatal("voice was not detected")
	}
}

func TestPCMS16leToG711(t *testing.T) {
	silence := make([]byte, 8)
	encoded, err := pcmS16leToG711(silence, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 4 {
		t.Fatalf("expected 4 μ-law samples, got %d", len(encoded))
	}
	for _, sample := range encoded {
		if sample != 0xff {
			t.Fatalf("silence did not encode to μ-law idle: %x", encoded)
		}
	}
}

func TestReadADTSAccessUnit(t *testing.T) {
	packets := mpeg4audio.ADTSPackets{{
		Type:          mpeg4audio.ObjectTypeAACLC,
		SampleRate:    16000,
		ChannelConfig: 1,
		ChannelCount:  1,
		AU:            bytes.Repeat([]byte{0x21}, 40),
	}}
	raw, err := packets.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	au, err := readADTSAccessUnit(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(au, packets[0].AU) {
		t.Fatalf("AU mismatch: %v", au)
	}
}

func filledBytes(size int, value byte) []byte {
	result := make([]byte, size)
	for index := range result {
		result[index] = value
	}
	return result
}
