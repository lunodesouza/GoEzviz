package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

const getStreamURIAction = "http://www.onvif.org/ver10/media/wsdl/GetStreamUri"

type onvifMediaProfile struct {
	Token       string
	Name        string
	SourceToken string
	RTSPURL     string
	Width       int
	Height      int
	Codec       string
	HasPTZ      bool
	HasAudio    bool
	CanTalk     bool
}

func (c *onvifClient) discoverMediaProfiles(ctx context.Context) ([]onvifMediaProfile, error) {
	response, err := c.request(
		ctx,
		c.config.onvifDeviceURL(),
		deviceAction,
		`<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>`,
	)
	if err != nil {
		return nil, fmt.Errorf("ONVIF indisponivel: %w", err)
	}
	var capabilities struct {
		Body struct {
			Response struct {
				Capabilities struct {
					Media struct {
						XAddr string `xml:"XAddr"`
					} `xml:"Media"`
					PTZ struct {
						XAddr string `xml:"XAddr"`
					} `xml:"PTZ"`
				} `xml:"Capabilities"`
			} `xml:"GetCapabilitiesResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(response, &capabilities); err != nil {
		return nil, fmt.Errorf("resposta ONVIF invalida: %w", err)
	}
	c.mediaURL = capabilities.Body.Response.Capabilities.Media.XAddr
	c.ptzURL = capabilities.Body.Response.Capabilities.PTZ.XAddr
	if c.mediaURL == "" {
		return nil, errors.New("a camera nao anunciou o servico Media")
	}

	response, err = c.request(ctx, c.mediaURL, profilesAction, `<trt:GetProfiles/>`)
	if err != nil {
		return nil, fmt.Errorf("nao foi possivel obter os perfis: %w", err)
	}
	var result struct {
		Body struct {
			Response struct {
				Profiles []struct {
					Token       string `xml:"token,attr"`
					Name        string `xml:"Name"`
					VideoSource struct {
						SourceToken string `xml:"SourceToken"`
						Bounds      struct {
							Width  int `xml:"width,attr"`
							Height int `xml:"height,attr"`
						} `xml:"Bounds"`
					} `xml:"VideoSourceConfiguration"`
					VideoEncoder struct {
						Encoding   string `xml:"Encoding"`
						Resolution struct {
							Width  int `xml:"Width"`
							Height int `xml:"Height"`
						} `xml:"Resolution"`
					} `xml:"VideoEncoderConfiguration"`
					PTZ          *struct{} `xml:"PTZConfiguration"`
					AudioSource  *struct{} `xml:"AudioSourceConfiguration"`
					AudioEncoder *struct{} `xml:"AudioEncoderConfiguration"`
					AudioOutput  *struct{} `xml:"AudioOutputConfiguration"`
					AudioDecoder *struct{} `xml:"AudioDecoderConfiguration"`
				} `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(response, &result); err != nil {
		return nil, fmt.Errorf("perfis ONVIF invalidos: %w", err)
	}

	profiles := make([]onvifMediaProfile, 0, len(result.Body.Response.Profiles))
	for _, source := range result.Body.Response.Profiles {
		if source.Token == "" {
			continue
		}
		profile := onvifMediaProfile{
			Token:       source.Token,
			Name:        strings.TrimSpace(source.Name),
			SourceToken: source.VideoSource.SourceToken,
			Width:       source.VideoEncoder.Resolution.Width,
			Height:      source.VideoEncoder.Resolution.Height,
			Codec:       source.VideoEncoder.Encoding,
			HasPTZ:      source.PTZ != nil && c.ptzURL != "",
			HasAudio:    source.AudioSource != nil || source.AudioEncoder != nil,
			CanTalk: source.AudioSource != nil || source.AudioEncoder != nil ||
				source.AudioOutput != nil || source.AudioDecoder != nil,
		}
		if profile.Width == 0 {
			profile.Width = source.VideoSource.Bounds.Width
			profile.Height = source.VideoSource.Bounds.Height
		}
		if profile.Name == "" {
			profile.Name = profile.Token
		}
		profile.RTSPURL, _ = c.streamURI(ctx, profile.Token)
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		return nil, errors.New("nenhum perfil de midia foi encontrado")
	}
	return profiles, nil
}

func (c *onvifClient) streamURI(ctx context.Context, profileToken string) (string, error) {
	body := fmt.Sprintf(
		`<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>%s</trt:ProfileToken></trt:GetStreamUri>`,
		xmlText(profileToken),
	)
	response, err := c.request(ctx, c.mediaURL, getStreamURIAction, body)
	if err != nil {
		return "", err
	}
	var result struct {
		Body struct {
			Response struct {
				MediaURI struct {
					URI string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetStreamUriResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(response, &result); err != nil {
		return "", err
	}
	if result.Body.Response.MediaURI.URI == "" {
		return "", errors.New("perfil sem URI RTSP")
	}
	return result.Body.Response.MediaURI.URI, nil
}

func preferredPTZProfile(profiles []onvifMediaProfile) string {
	var first string
	for _, profile := range profiles {
		if !profile.HasPTZ {
			continue
		}
		if first == "" {
			first = profile.Token
		}
		source := strings.ToLower(profile.SourceToken)
		if source == "2" || strings.HasSuffix(source, "_2") {
			return profile.Token
		}
	}
	return first
}
