package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/g711"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/pion/rtp"
)

const microphonePacketDuration = 20 * time.Millisecond

// openMicPrivacySettings opens the OS microphone privacy page. Tests may replace it.
var openMicPrivacySettings = openMicrophonePrivacySettings

type cameraTalk struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
	active     bool
	onActivity func(bool)
}

func (talk *cameraTalk) setActivityCallback(callback func(bool)) {
	talk.mu.Lock()
	talk.onActivity = callback
	talk.mu.Unlock()
}

func findG711BackChannel(session *description.Session) (*description.Media, *format.G711) {
	media, _, g711 := findTalkBackChannel(session)
	return media, g711
}

func findTalkBackChannel(session *description.Session) (*description.Media, *format.MPEG4Audio, *format.G711) {
	for _, media := range session.Medias {
		if !media.IsBackChannel {
			continue
		}
		var aac *format.MPEG4Audio
		var g711 *format.G711
		for _, streamFormat := range media.Formats {
			switch typed := streamFormat.(type) {
			case *format.MPEG4Audio:
				aac = typed
			case *format.G711:
				g711 = typed
			}
		}
		if aac != nil || g711 != nil {
			return media, aac, g711
		}
	}
	return nil, nil, nil
}

func ensureAACTalkFormat(aac *format.MPEG4Audio) {
	if aac.SizeLength == 0 {
		aac.SizeLength = 13
	}
	if aac.IndexLength == 0 {
		aac.IndexLength = 3
	}
	if aac.IndexDeltaLength == 0 {
		aac.IndexDeltaLength = 3
	}
	if aac.Config == nil {
		aac.Config = &mpeg4audio.AudioSpecificConfig{
			Type:          mpeg4audio.ObjectTypeAACLC,
			SampleRate:    16000,
			ChannelConfig: 1,
			ChannelCount:  1,
		}
	}
	if aac.Config.SampleRate == 0 {
		aac.Config.SampleRate = 16000
	}
}

// talkSetupMedias keeps the talk session minimal: only the send-only media is set up,
// which is what the camera accepts without opening a second video session.
func talkSetupMedias(_ *description.Session, backChannel *description.Media) []*description.Media {
	if backChannel == nil {
		return nil
	}
	return []*description.Media{backChannel}
}

func (talk *cameraTalk) start(
	parent context.Context,
	config cameraConfig,
	channel string,
	microphone string,
	prepare func(context.Context) error,
	ready func(),
	report func(error),
) error {
	return talk.startURL(parent, config.rtspURL(channel), microphone, prepare, ready, report)
}

func (talk *cameraTalk) startURL(
	parent context.Context,
	rawStreamURL string,
	microphone string,
	prepare func(context.Context) error,
	ready func(),
	report func(error),
) error {
	if microphone == "" {
		return errors.New("selecione um microfone")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("ffmpeg nao foi encontrado")
	}
	streamURL, err := base.ParseURL(rawStreamURL)
	if err != nil {
		return err
	}

	talk.stop()
	ctx, cancel := context.WithCancel(parent)
	talk.mu.Lock()
	talk.generation++
	generation := talk.generation
	talk.cancel = cancel
	talk.active = true
	activity := talk.onActivity
	talk.mu.Unlock()

	go func() {
		var runErr error
		if prepare != nil {
			runErr = prepare(ctx)
		}
		if runErr == nil {
			runErr = runMicrophoneBackchannel(ctx, ffmpeg, streamURL, microphone, ready, activity)
		}
		cancelled := ctx.Err() != nil
		cancel()

		talk.mu.Lock()
		current := talk.generation == generation
		if current {
			talk.cancel = nil
			talk.active = false
		}
		talk.mu.Unlock()
		if current && !cancelled && runErr != nil {
			report(microphoneError(runErr))
		}
	}()
	return nil
}

func runMicrophoneBackchannel(
	ctx context.Context,
	ffmpeg string,
	streamURL *base.URL,
	microphone string,
	ready func(),
	activity func(bool),
) error {
	client, media, aac, g711, err := openTalkSession(ctx, streamURL)
	if err != nil {
		return err
	}
	defer client.Close()
	// The EZVIZ firmware only plays AAC on the back channel: it accepts G.711 packets,
	// mutes its own microphone, and never drives the speaker with them.
	if aac != nil {
		return sendAACTalk(ctx, client, media, aac, ffmpeg, microphone, ready, activity)
	}
	return sendG711Talk(ctx, client, media, g711, ffmpeg, microphone, ready, activity)
}

func sendAACTalk(
	ctx context.Context,
	client *gortsplib.Client,
	media *description.Media,
	aac *format.MPEG4Audio,
	ffmpeg, microphone string,
	ready func(),
	activity func(bool),
) error {
	capture := exec.CommandContext(ctx, ffmpeg, microphonePCMCaptureArgs(microphone, aac.ClockRate())...)
	encoder := exec.CommandContext(ctx, ffmpeg, microphoneAACEncodeArgs(aac.ClockRate())...)
	hideCommandWindow(capture)
	hideCommandWindow(encoder)
	pcmOut, err := capture.StdoutPipe()
	if err != nil {
		return err
	}
	encIn, err := encoder.StdinPipe()
	if err != nil {
		return err
	}
	encOut, err := encoder.StdoutPipe()
	if err != nil {
		return err
	}
	var captureErr, encodeErr bytes.Buffer
	capture.Stderr = &captureErr
	encoder.Stderr = &encodeErr
	if err := capture.Start(); err != nil {
		return fmt.Errorf("abrir microfone: %w", err)
	}
	if err := encoder.Start(); err != nil {
		return fmt.Errorf("codificar microfone: %w", err)
	}

	var voiced atomic.Bool
	go func() {
		defer encIn.Close()
		buf := make([]byte, 2048)
		for {
			n, readErr := pcmOut.Read(buf)
			if n > 0 {
				voiced.Store(pcmS16leHasVoice(buf[:n]))
				if _, writeErr := encIn.Write(buf[:n]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	err = streamAACPackets(ctx, client, media, aac, encOut, voiced.Load, ready, activity)
	_ = capture.Wait()
	_ = encoder.Wait()
	if ctx.Err() != nil {
		return nil
	}
	message := strings.TrimSpace(captureErr.String() + "\n" + encodeErr.String())
	if message != "" && err != nil {
		return errors.New(message)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// streamAACPackets pushes an ADTS stream to the camera speaker. This is the only
// codec the EZVIZ firmware actually plays on the back channel.
func streamAACPackets(
	ctx context.Context,
	client *gortsplib.Client,
	media *description.Media,
	aac *format.MPEG4Audio,
	adts io.Reader,
	voiced func() bool,
	ready func(),
	activity func(bool),
) error {
	rtpEncoder, err := aac.CreateEncoder()
	if err != nil {
		return err
	}
	// One access unit covers 1024 samples, so the camera expects a packet every
	// 1024/sampleRate seconds. Sending them in bursts leaves the speaker silent.
	interval := time.Duration(mpeg4audio.SamplesPerAccessUnit) * time.Second / time.Duration(aac.ClockRate())
	reader := bufio.NewReaderSize(adts, 4096)
	return writeTalkPackets(ctx, client, media, interval, ready, activity, func() ([]*rtp.Packet, uint32, bool, error) {
		au, readErr := readADTSAccessUnit(reader)
		if readErr != nil {
			return nil, 0, false, readErr
		}
		packets, readErr := rtpEncoder.Encode([][]byte{au})
		return packets, mpeg4audio.SamplesPerAccessUnit, voiced(), readErr
	})
}

func sendG711Talk(
	ctx context.Context,
	client *gortsplib.Client,
	media *description.Media,
	audioFormat *format.G711,
	ffmpeg, microphone string,
	ready func(),
	activity func(bool),
) error {
	if audioFormat == nil {
		return errors.New("a camera nao anunciou um backchannel de audio no RTSP")
	}
	rtpEncoder, err := audioFormat.CreateEncoder()
	if err != nil {
		return err
	}
	clockRate := audioFormat.ClockRate()
	if clockRate <= 0 {
		clockRate = 8000
	}
	cmd := exec.CommandContext(ctx, ffmpeg, microphoneCaptureArgs(microphone, clockRate)...)
	hideCommandWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("abrir microfone: %w", err)
	}
	sampleCount := clockRate * int(microphonePacketDuration) / int(time.Second)
	if sampleCount <= 0 {
		sampleCount = 160
	}
	channels := audioFormat.ChannelCount
	if channels < 1 {
		channels = 1
	}
	pcm := make([]byte, sampleCount*channels*2)
	err = writeTalkPackets(ctx, client, media, microphonePacketDuration, ready, activity, func() ([]*rtp.Packet, uint32, bool, error) {
		if _, readErr := io.ReadFull(stdout, pcm); readErr != nil {
			return nil, 0, false, readErr
		}
		encoded, encodeErr := pcmS16beToG711(pcm, audioFormat.MULaw)
		if encodeErr != nil {
			return nil, 0, false, encodeErr
		}
		packets, packErr := rtpEncoder.Encode(encoded)
		return packets, uint32(len(encoded) / channels), pcmS16HasVoice(pcm, binary.BigEndian), packErr
	})
	_ = cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if message := strings.TrimSpace(stderr.String()); message != "" {
		return errors.New(message)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeTalkPackets(
	ctx context.Context,
	client *gortsplib.Client,
	media *description.Media,
	interval time.Duration,
	ready func(),
	activity func(bool),
	next func() ([]*rtp.Packet, uint32, bool, error),
) error {
	randomStart := randomRTPTimestamp()
	var pts uint32
	readySent := false
	voiceActive := false
	silentFrames := 0
	pacer := newTalkPacer(interval)
	defer func() {
		if voiceActive && activity != nil {
			activity(false)
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		packets, delta, voiced, err := next()
		if err != nil {
			return err
		}
		if voiced {
			silentFrames = 0
			if !voiceActive {
				voiceActive = true
				if activity != nil {
					activity(true)
				}
			}
		} else if voiceActive {
			silentFrames++
			if silentFrames >= 8 {
				voiceActive = false
				if activity != nil {
					activity(false)
				}
			}
		}
		if !pacer.wait(ctx) {
			return nil
		}
		for _, packet := range packets {
			packet.Timestamp += randomStart + pts
			if err := client.WritePacketRTP(media, packet); err != nil {
				return err
			}
			if !readySent && ctx.Err() == nil {
				readySent = true
				if ready != nil {
					ready()
				}
			}
		}
		pts += delta
	}
}

// talkPacer releases one packet per interval using a fixed schedule, so the camera
// receives audio at the rate its decoder expects instead of in bursts.
type talkPacer struct {
	interval time.Duration
	next     time.Time
}

func newTalkPacer(interval time.Duration) *talkPacer {
	return &talkPacer{interval: interval}
}

func (pacer *talkPacer) wait(ctx context.Context) bool {
	if pacer.interval <= 0 {
		return ctx.Err() == nil
	}
	now := time.Now()
	if pacer.next.IsZero() {
		pacer.next = now
	}
	// Resynchronise after a long stall instead of trying to catch up in one burst.
	if pacer.next.Before(now.Add(-4 * pacer.interval)) {
		pacer.next = now
	}
	if wait := time.Until(pacer.next); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return false
		}
	}
	pacer.next = pacer.next.Add(pacer.interval)
	return ctx.Err() == nil
}

func readADTSAccessUnit(reader *bufio.Reader) ([]byte, error) {
	for {
		first, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if first != 0xff {
			continue
		}
		second, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if second&0xf0 != 0xf0 {
			_ = reader.UnreadByte()
			continue
		}
		header := []byte{first, second, 0, 0, 0, 0, 0}
		if _, err := io.ReadFull(reader, header[2:]); err != nil {
			return nil, err
		}
		frameLen := int(((uint16(header[3]) & 0x03) << 11) | (uint16(header[4]) << 3) | ((uint16(header[5]) >> 5) & 0x07))
		if frameLen < 7 {
			continue
		}
		payload := make([]byte, frameLen-7)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
}

func openTalkSession(ctx context.Context, streamURL *base.URL) (*gortsplib.Client, *description.Media, *format.MPEG4Audio, *format.G711, error) {
	if ctx.Err() != nil {
		return nil, nil, nil, nil, ctx.Err()
	}
	return connectTalkSession(streamURL, gortsplib.ProtocolTCP)
}

func connectTalkSession(streamURL *base.URL, protocol gortsplib.Protocol) (*gortsplib.Client, *description.Media, *format.MPEG4Audio, *format.G711, error) {
	client := &gortsplib.Client{
		Scheme:              streamURL.Scheme,
		Host:                streamURL.Host,
		Protocol:            &protocol,
		ReadTimeout:         8 * time.Second,
		WriteTimeout:        8 * time.Second,
		RequestBackChannels: true,
	}
	if err := client.Start(); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("conexao RTSP (%s): %w", protocol, err)
	}

	session, _, err := client.Describe(streamURL)
	if err != nil {
		client.Close()
		return nil, nil, nil, nil, fmt.Errorf("backchannel indisponivel (%s): %w", protocol, err)
	}
	media, aac, g711 := findTalkBackChannel(session)
	if media == nil {
		client.Close()
		return nil, nil, nil, nil, errors.New("a camera nao anunciou um backchannel de audio no RTSP")
	}
	if aac != nil {
		ensureAACTalkFormat(aac)
	}
	for _, setupMedia := range talkSetupMedias(session, media) {
		if _, setupErr := client.Setup(session.BaseURL, setupMedia, 0, 0); setupErr != nil {
			client.Close()
			return nil, nil, nil, nil, fmt.Errorf("preparar backchannel (%s): %w", protocol, setupErr)
		}
	}
	if _, err := client.Play(nil); err != nil {
		client.Close()
		return nil, nil, nil, nil, fmt.Errorf("iniciar backchannel (%s): %w", protocol, err)
	}
	return client, media, aac, g711, nil
}

func randomRTPTimestamp() uint32 {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return uint32(value[0])<<24 | uint32(value[1])<<16 | uint32(value[2])<<8 | uint32(value[3])
}

func pcmS16beToG711(samples []byte, mulaw bool) ([]byte, error) {
	if mulaw {
		return g711.Mulaw(samples).Marshal()
	}
	return g711.Alaw(samples).Marshal()
}

func pcmS16leToG711(samples []byte, mulaw bool) ([]byte, error) {
	return pcmS16beToG711(pcmS16SwapEndian(samples), mulaw)
}

func pcmS16SwapEndian(samples []byte) []byte {
	swapped := make([]byte, len(samples))
	for index := 0; index+1 < len(samples); index += 2 {
		swapped[index] = samples[index+1]
		swapped[index+1] = samples[index]
	}
	return swapped
}

func pcmS16leHasVoice(samples []byte) bool {
	return pcmS16HasVoice(samples, binary.LittleEndian)
}

func pcmS16HasVoice(samples []byte, order binary.ByteOrder) bool {
	if len(samples) < 4 {
		return false
	}
	var total int64
	count := 0
	for index := 0; index+1 < len(samples); index += 2 {
		value := int(int16(order.Uint16(samples[index:])))
		if value < 0 {
			value = -value
		}
		total += int64(value)
		count++
	}
	return count > 0 && total/int64(count) >= 400
}

func g711HasVoice(samples []byte, mulaw bool) bool {
	if len(samples) == 0 {
		return false
	}
	var total int64
	for _, sample := range samples {
		var value int
		if mulaw {
			decoded := ^sample
			value = (int(decoded&0x0f)<<3 + 0x84) << ((decoded & 0x70) >> 4)
			value -= 0x84
		} else {
			decoded := sample ^ 0x55
			value = int(decoded&0x0f) << 4
			segment := int((decoded & 0x70) >> 4)
			switch segment {
			case 0:
				value += 8
			case 1:
				value += 0x108
			default:
				value += 0x108
				value <<= segment - 1
			}
		}
		total += int64(value)
	}
	return total/int64(len(samples)) >= 250
}

func microphoneError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "permission") ||
		strings.Contains(message, "access is denied") ||
		strings.Contains(message, "denied") ||
		strings.Contains(message, "not permitted") {
		_ = openMicPrivacySettings()
		switch runtime.GOOS {
		case "darwin":
			return errors.New("macOS bloqueou o microfone. Abrindo Ajustes > Privacidade e Seguranca > Microfone — permita o GoEzviz (e o Terminal, se usar go run)")
		case "windows":
			return errors.New("Windows bloqueou o microfone. Abrindo Configuracoes > Privacidade > Microfone — permita aplicativos desktop")
		default:
			return errors.New("acesso ao microfone foi negado pelo sistema")
		}
	}
	if strings.Contains(message, "i/o error") ||
		strings.Contains(message, "could not find") ||
		strings.Contains(message, "failed to fill") ||
		strings.Contains(message, "error opening") ||
		strings.Contains(message, "input/output error") {
		switch runtime.GOOS {
		case "darwin":
			return fmt.Errorf("nao foi possivel abrir o microfone. Verifique o dispositivo ou use Liberar mic: %w", err)
		case "windows":
			return fmt.Errorf("nao foi possivel abrir o microfone. Verifique o dispositivo ou use Liberar mic: %w", err)
		default:
			return fmt.Errorf("nao foi possivel abrir o microfone. Verifique o dispositivo selecionado: %w", err)
		}
	}
	return err
}

func (talk *cameraTalk) stop() {
	talk.mu.Lock()
	talk.generation++
	cancel := talk.cancel
	talk.cancel = nil
	talk.active = false
	activity := talk.onActivity
	talk.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if activity != nil {
		activity(false)
	}
}

func (talk *cameraTalk) isActive() bool {
	talk.mu.Lock()
	defer talk.mu.Unlock()
	return talk.active
}
