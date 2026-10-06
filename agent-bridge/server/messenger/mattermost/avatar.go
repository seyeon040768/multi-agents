package mattermost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/seyeon/agent-bridge/server/agent"
)

// Fetch only public HTTPS images, checking resolved addresses at dial time.
func publicImageClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			ip := address.IP
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				return nil, fmt.Errorf("avatar address must be public")
			}
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("avatar hostname has no addresses")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" {
			return fmt.Errorf("avatar redirect rejected")
		}
		return nil
	}}
}
func (p *BotProvisioner) syncAvatar(a *agent.Agent, userID string) error {
	avatarURL := a.Avatar.URL
	if a.Messenger.Profile.AvatarURL != nil && *a.Messenger.Profile.AvatarURL != "" {
		avatarURL = a.Messenger.Profile.AvatarURL
	}
	if avatarURL == nil || *avatarURL == "" {
		// No custom URL: use a deterministic neutral Agent avatar.
		sum := sha256.Sum256([]byte(a.ID))
		img := image.NewRGBA(image.Rect(0, 0, 128, 128))
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				img.Set(x, y, color.RGBA{sum[0], sum[1], sum[2], 255})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			return err
		}
		if err := p.api.SetProfileImage(userID, encoded.Bytes()); err != nil {
			return err
		}
		return nil
	}
	parsed, err := url.Parse(*avatarURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("avatar URL must be public HTTPS")
	}
	client := publicImageClient()
	defer client.CloseIdleConnections()
	response, err := client.Get(parsed.String())
	if err != nil {
		return fmt.Errorf("avatar download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("avatar download returned %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return fmt.Errorf("avatar image exceeds 2 MiB or cannot be read")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 4096 || config.Height > 4096 {
		return fmt.Errorf("avatar must be PNG, JPEG or GIF up to 4096 pixels")
	}
	if err := p.api.SetProfileImage(userID, data); err != nil {
		return err
	}
	return nil
}
