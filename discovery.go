package main

import (
	"context"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type discoveredCamera struct {
	host             string
	name             string
	deviceServiceURL string
}

func discoverLocalCameras(ctx context.Context) ([]discoveredCamera, error) {
	deadline := time.Now().Add(6 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	target, err := net.ResolveUDPAddr("udp4", "239.255.255.250:3702")
	if err != nil {
		return nil, err
	}

	type discoveryResult struct {
		data []byte
		err  error
	}
	results := make(chan discoveryResult, 64)
	var readers sync.WaitGroup
	var listenErr error
	addresses := discoveryIPv4Addresses()
	for _, address := range addresses {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: address})
		if err != nil {
			listenErr = err
			continue
		}
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			listenErr = err
			continue
		}
		readers.Add(1)
		go func(conn *net.UDPConn) {
			defer readers.Done()
			defer conn.Close()
			go sendDiscoveryProbes(ctx, conn, target)
			buffer := make([]byte, 65535)
			for {
				n, _, readErr := conn.ReadFromUDP(buffer)
				if readErr != nil {
					if networkError, ok := readErr.(net.Error); ok && networkError.Timeout() {
						return
					}
					if ctx.Err() == nil {
						results <- discoveryResult{err: readErr}
					}
					return
				}
				packet := append([]byte(nil), buffer[:n]...)
				results <- discoveryResult{data: packet}
			}
		}(conn)
	}
	if readersCount := len(addresses); readersCount == 0 {
		return nil, errors.New("nenhuma interface IPv4 ativa foi encontrada")
	}
	go func() {
		readers.Wait()
		close(results)
	}()

	found := make(map[string]discoveredCamera)
	for result := range results {
		if result.err != nil {
			if listenErr == nil {
				listenErr = result.err
			}
			continue
		}
		cameras, err := parseDiscoveryResponse(result.data)
		if err != nil {
			continue
		}
		for _, camera := range cameras {
			previous, exists := found[camera.host]
			if !exists || previous.name == "Camera ONVIF" {
				found[camera.host] = camera
			}
		}
	}
	if len(found) == 0 && listenErr != nil {
		return nil, listenErr
	}

	cameras := make([]discoveredCamera, 0, len(found))
	for _, camera := range found {
		cameras = append(cameras, camera)
	}
	sort.Slice(cameras, func(i, j int) bool { return cameras[i].host < cameras[j].host })
	return cameras, nil
}

func discoveryIPv4Addresses() []net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []net.IP{net.IPv4zero}
	}
	var addresses []net.IP
	seen := make(map[string]bool)
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 ||
			networkInterface.Flags&net.FlagLoopback != 0 ||
			networkInterface.Flags&net.FlagMulticast == 0 {
			continue
		}
		interfaceAddresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range interfaceAddresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			ip = ip.To4()
			if !seen[ip.String()] {
				seen[ip.String()] = true
				addresses = append(addresses, ip)
			}
		}
	}
	if len(addresses) == 0 {
		return []net.IP{net.IPv4zero}
	}
	return addresses
}

func sendDiscoveryProbes(ctx context.Context, conn *net.UDPConn, target *net.UDPAddr) {
	for attempt := 0; attempt < 3; attempt++ {
		for _, typed := range []bool{true, false} {
			probe, err := discoveryProbe(typed)
			if err == nil {
				_, _ = conn.WriteToUDP(probe, target)
			}
		}
		if attempt == 2 {
			return
		}
		timer := time.NewTimer(700 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func discoveryProbe(typed bool) ([]byte, error) {
	messageID := make([]byte, 16)
	if _, err := rand.Read(messageID); err != nil {
		return nil, err
	}
	messageID[6] = (messageID[6] & 0x0f) | 0x40
	messageID[8] = (messageID[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", messageID[0:4], messageID[4:6], messageID[6:8], messageID[8:10], messageID[10:16])
	probeBody := `<d:Probe/>`
	if typed {
		probeBody = `<d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe>`
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
<e:Header><a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action><a:MessageID>uuid:%s</a:MessageID><a:ReplyTo><a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address></a:ReplyTo><a:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To></e:Header>
<e:Body>%s</e:Body></e:Envelope>`, uuid, probeBody)), nil
}

func parseDiscoveryResponse(data []byte) ([]discoveredCamera, error) {
	var response struct {
		Body struct {
			ProbeMatches struct {
				Matches []struct {
					XAddrs string `xml:"XAddrs"`
					Scopes string `xml:"Scopes"`
				} `xml:"ProbeMatch"`
			} `xml:"ProbeMatches"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	var cameras []discoveredCamera
	for _, match := range response.Body.ProbeMatches.Matches {
		name := discoveryCameraName(match.Scopes)
		for _, address := range strings.Fields(match.XAddrs) {
			endpoint, err := url.Parse(address)
			if err != nil || endpoint.Hostname() == "" {
				continue
			}
			cameras = append(cameras, discoveredCamera{
				host:             endpoint.Hostname(),
				name:             name,
				deviceServiceURL: address,
			})
		}
	}
	return cameras, nil
}

func discoveryCameraName(scopes string) string {
	for _, category := range []string{"/name/", "/hardware/"} {
		for _, scope := range strings.Fields(scopes) {
			decoded, err := url.PathUnescape(scope)
			if err != nil {
				decoded = scope
			}
			index := strings.Index(strings.ToLower(decoded), category)
			if index >= 0 {
				if name := strings.TrimSpace(decoded[index+len(category):]); name != "" {
					return name
				}
			}
		}
	}
	return "Camera ONVIF"
}
