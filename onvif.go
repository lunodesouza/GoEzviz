package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deviceAction          = "http://www.onvif.org/ver10/device/wsdl/GetCapabilities"
	profilesAction        = "http://www.onvif.org/ver10/media/wsdl/GetProfiles"
	moveAction            = "http://www.onvif.org/ver20/ptz/wsdl/ContinuousMove"
	stopAction            = "http://www.onvif.org/ver20/ptz/wsdl/Stop"
	getStatusAction       = "http://www.onvif.org/ver20/ptz/wsdl/GetStatus"
	absoluteMoveAction    = "http://www.onvif.org/ver20/ptz/wsdl/AbsoluteMove"
	setPresetAction       = "http://www.onvif.org/ver20/ptz/wsdl/SetPreset"
	gotoPresetAction      = "http://www.onvif.org/ver20/ptz/wsdl/GotoPreset"
	removePresetAction    = "http://www.onvif.org/ver20/ptz/wsdl/RemovePreset"
	getAudioOutputsAction = "http://www.onvif.org/ver10/media/wsdl/GetAudioOutputConfigurations"
	setAudioOutputAction  = "http://www.onvif.org/ver10/media/wsdl/SetAudioOutputConfiguration"
)

type cameraConfig struct {
	host             string
	username         string
	password         string
	deviceServiceURL string
}

func (c cameraConfig) rtspURL(channel string) string {
	u := &url.URL{
		Scheme: "rtsp",
		User:   url.UserPassword(c.username, c.password),
		Host:   net.JoinHostPort(c.host, "554"),
		Path:   "/Streaming/Channels/" + channel,
	}
	return u.String()
}

func (c cameraConfig) withCredentials(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.User = url.UserPassword(c.username, c.password)
	return parsed.String()
}

func (c cameraConfig) onvifDeviceURL() string {
	if c.deviceServiceURL != "" {
		return c.deviceServiceURL
	}
	return "http://" + net.JoinHostPort(c.host, "80") + "/onvif/device_service"
}

type onvifClient struct {
	config       cameraConfig
	httpClient   *http.Client
	mediaURL     string
	ptzURL       string
	profileToken string
}

func newONVIFClient(config cameraConfig) *onvifClient {
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // Camera uses a local self-signed certificate.
		DisableKeepAlives: true,
	}
	return &onvifClient{
		config: config,
		httpClient: &http.Client{
			Timeout:   8 * time.Second,
			Transport: transport,
		},
	}
}

func (c *onvifClient) discover(ctx context.Context) error {
	profiles, err := c.discoverMediaProfiles(ctx)
	if err != nil {
		return err
	}
	c.profileToken = preferredPTZProfile(profiles)
	if c.profileToken == "" {
		return errors.New("nenhum perfil PTZ foi encontrado")
	}
	return nil
}

func (c *onvifClient) move(ctx context.Context, x, y float64) error {
	if c.profileToken == "" {
		return errors.New("perfil PTZ ainda nao foi carregado")
	}
	body := fmt.Sprintf(
		`<tptz:ContinuousMove><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:Velocity><tt:PanTilt x="%.2f" y="%.2f" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace"/></tptz:Velocity></tptz:ContinuousMove>`,
		xmlText(c.profileToken), x, y,
	)
	_, err := c.request(ctx, c.ptzURL, moveAction, body)
	return err
}

func (c *onvifClient) stop(ctx context.Context) error {
	if c.profileToken == "" {
		return errors.New("perfil PTZ ainda nao foi carregado")
	}
	body := fmt.Sprintf(
		`<tptz:Stop><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:PanTilt>true</tptz:PanTilt><tptz:Zoom>true</tptz:Zoom></tptz:Stop>`,
		xmlText(c.profileToken),
	)
	_, err := c.request(ctx, c.ptzURL, stopAction, body)
	return err
}

const (
	defaultPanTiltSpace = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace"
	defaultZoomSpace    = "http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace"
)

type ptzPosition struct {
	Pan          float64
	Tilt         float64
	Zoom         float64
	PanTiltSpace string
	ZoomSpace    string
}

func (c *onvifClient) getPTZStatus(ctx context.Context) (ptzPosition, error) {
	if c.profileToken == "" {
		return ptzPosition{}, errors.New("perfil PTZ ainda nao foi carregado")
	}
	body := fmt.Sprintf(
		`<tptz:GetStatus><tptz:ProfileToken>%s</tptz:ProfileToken></tptz:GetStatus>`,
		xmlText(c.profileToken),
	)
	response, err := c.request(ctx, c.ptzURL, getStatusAction, body)
	if err != nil {
		return ptzPosition{}, err
	}
	return parsePTZStatus(response)
}

func parsePTZStatus(data []byte) (ptzPosition, error) {
	var payload struct {
		Body struct {
			Response struct {
				Status struct {
					Position struct {
						PanTilt struct {
							X     float64 `xml:"x,attr"`
							Y     float64 `xml:"y,attr"`
							Space string  `xml:"space,attr"`
						} `xml:"PanTilt"`
						Zoom struct {
							X     float64 `xml:"x,attr"`
							Space string  `xml:"space,attr"`
						} `xml:"Zoom"`
					} `xml:"Position"`
				} `xml:"PTZStatus"`
			} `xml:"GetStatusResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &payload); err != nil {
		return ptzPosition{}, fmt.Errorf("status PTZ invalido: %w", err)
	}
	position := payload.Body.Response.Status.Position
	result := ptzPosition{
		Pan:          position.PanTilt.X,
		Tilt:         position.PanTilt.Y,
		Zoom:         position.Zoom.X,
		PanTiltSpace: position.PanTilt.Space,
		ZoomSpace:    position.Zoom.Space,
	}
	if result.PanTiltSpace == "" {
		result.PanTiltSpace = defaultPanTiltSpace
	}
	if result.ZoomSpace == "" {
		result.ZoomSpace = defaultZoomSpace
	}
	return result, nil
}

func (c *onvifClient) absoluteMove(ctx context.Context, position ptzPosition) error {
	if c.profileToken == "" {
		return errors.New("perfil PTZ ainda nao foi carregado")
	}
	panSpace := position.PanTiltSpace
	if panSpace == "" {
		panSpace = defaultPanTiltSpace
	}
	zoomSpace := position.ZoomSpace
	if zoomSpace == "" {
		zoomSpace = defaultZoomSpace
	}
	body := fmt.Sprintf(
		`<tptz:AbsoluteMove><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:Position><tt:PanTilt x="%.6f" y="%.6f" space="%s"/><tt:Zoom x="%.6f" space="%s"/></tptz:Position></tptz:AbsoluteMove>`,
		xmlText(c.profileToken),
		position.Pan, position.Tilt, xmlText(panSpace),
		position.Zoom, xmlText(zoomSpace),
	)
	_, err := c.request(ctx, c.ptzURL, absoluteMoveAction, body)
	return err
}

func (c *onvifClient) setPreset(ctx context.Context, name, presetToken string) (string, error) {
	if c.profileToken == "" {
		return "", errors.New("perfil PTZ ainda nao foi carregado")
	}
	tokenXML := ""
	if presetToken != "" {
		tokenXML = "<tptz:PresetToken>" + xmlText(presetToken) + "</tptz:PresetToken>"
	}
	body := fmt.Sprintf(
		`<tptz:SetPreset><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:PresetName>%s</tptz:PresetName>%s</tptz:SetPreset>`,
		xmlText(c.profileToken), xmlText(name), tokenXML,
	)
	response, err := c.request(ctx, c.ptzURL, setPresetAction, body)
	if err != nil {
		return "", err
	}
	token := parsePresetToken(response)
	if token == "" {
		token = presetToken
	}
	if token == "" {
		return "", errors.New("a camera nao devolveu o token do preset")
	}
	return token, nil
}

func parsePresetToken(data []byte) string {
	var payload struct {
		Body struct {
			Response struct {
				PresetToken string `xml:"PresetToken"`
			} `xml:"SetPresetResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Body.Response.PresetToken)
}

func (c *onvifClient) gotoPreset(ctx context.Context, presetToken string) error {
	if c.profileToken == "" {
		return errors.New("perfil PTZ ainda nao foi carregado")
	}
	if presetToken == "" {
		return errors.New("preset sem token")
	}
	body := fmt.Sprintf(
		`<tptz:GotoPreset><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:PresetToken>%s</tptz:PresetToken><tptz:Speed><tt:PanTilt x="0.5" y="0.5"/><tt:Zoom x="0.5"/></tptz:Speed></tptz:GotoPreset>`,
		xmlText(c.profileToken), xmlText(presetToken),
	)
	_, err := c.request(ctx, c.ptzURL, gotoPresetAction, body)
	return err
}

func (c *onvifClient) removePreset(ctx context.Context, presetToken string) error {
	if c.profileToken == "" {
		return errors.New("perfil PTZ ainda nao foi carregado")
	}
	if presetToken == "" {
		return nil
	}
	body := fmt.Sprintf(
		`<tptz:RemovePreset><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:PresetToken>%s</tptz:PresetToken></tptz:RemovePreset>`,
		xmlText(c.profileToken), xmlText(presetToken),
	)
	_, err := c.request(ctx, c.ptzURL, removePresetAction, body)
	return err
}

func soapFaultText(data []byte) string {
	var payload struct {
		Body struct {
			Fault struct {
				FaultString string `xml:"faultstring"`
				Reason      struct {
					Text string `xml:"Text"`
				} `xml:"Reason"`
				Code struct {
					Value   string `xml:"Value"`
					Subcode struct {
						Value string `xml:"Value"`
					} `xml:"Subcode"`
				} `xml:"Code"`
			} `xml:"Fault"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &payload); err != nil {
		return ""
	}
	fault := payload.Body.Fault
	for _, value := range []string{fault.Reason.Text, fault.FaultString, fault.Code.Subcode.Value, fault.Code.Value} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *onvifClient) setAudioOutputLevel(ctx context.Context, level int) error {
	response, err := c.request(ctx, c.mediaURL, getAudioOutputsAction, `<trt:GetAudioOutputConfigurations/>`)
	if err != nil {
		return fmt.Errorf("consultar volume do alto-falante: %w", err)
	}
	var configurations struct {
		Body struct {
			Response struct {
				Configurations []struct {
					Token       string `xml:"token,attr"`
					Name        string `xml:"Name"`
					UseCount    int    `xml:"UseCount"`
					OutputToken string `xml:"OutputToken"`
					SendPrimacy string `xml:"SendPrimacy"`
				} `xml:"Configurations"`
			} `xml:"GetAudioOutputConfigurationsResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(response, &configurations); err != nil {
		return fmt.Errorf("configuracao de audio invalida: %w", err)
	}
	if len(configurations.Body.Response.Configurations) == 0 {
		return errors.New("a camera nao forneceu configuracao do alto-falante")
	}
	configuration := configurations.Body.Response.Configurations[0]
	if level < 0 {
		level = 0
	} else if level > 100 {
		level = 100
	}
	sendPrimacy := ""
	if configuration.SendPrimacy != "" {
		sendPrimacy = "<tt:SendPrimacy>" + xmlText(configuration.SendPrimacy) + "</tt:SendPrimacy>"
	}
	body := fmt.Sprintf(
		`<trt:SetAudioOutputConfiguration><trt:Configuration token="%s"><tt:Name>%s</tt:Name><tt:UseCount>%d</tt:UseCount><tt:OutputToken>%s</tt:OutputToken>%s<tt:OutputLevel>%d</tt:OutputLevel></trt:Configuration><trt:ForcePersistence>true</trt:ForcePersistence></trt:SetAudioOutputConfiguration>`,
		xmlText(configuration.Token),
		xmlText(configuration.Name),
		configuration.UseCount,
		xmlText(configuration.OutputToken),
		sendPrimacy,
		level,
	)
	if _, err := c.request(ctx, c.mediaURL, setAudioOutputAction, body); err != nil {
		return fmt.Errorf("ajustar volume do alto-falante: %w", err)
	}
	return nil
}

func (c *onvifClient) request(ctx context.Context, endpoint, action, body string) ([]byte, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	hash := sha1.New()
	hash.Write(nonce)
	hash.Write([]byte(created))
	hash.Write([]byte(c.config.password))
	digest := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	envelope := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
<s:Header><wsse:Security s:mustUnderstand="1"><wsse:UsernameToken><wsse:Username>%s</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>
<s:Body>%s</s:Body></s:Envelope>`, xmlText(c.config.username), digest, base64.StdEncoding.EncodeToString(nonce), created, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	req.Close = true
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if text := soapFaultText(data); text != "" {
			return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, text)
		}
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, compactXML(data))
	}
	if bytes.Contains(data, []byte(":Fault>")) || bytes.Contains(data, []byte("<Fault>")) {
		if text := soapFaultText(data); text != "" {
			return nil, errors.New(text)
		}
		return nil, fmt.Errorf("falha SOAP: %s", compactXML(data))
	}
	return data, nil
}
