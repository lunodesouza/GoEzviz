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
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, compactXML(data))
	}
	if bytes.Contains(data, []byte(":Fault>")) || bytes.Contains(data, []byte("<Fault>")) {
		return nil, fmt.Errorf("falha SOAP: %s", compactXML(data))
	}
	return data, nil
}
