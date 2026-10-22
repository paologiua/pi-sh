package webauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"golang.org/x/net/html"
)

// ErrAnswerTimeout means Connect accepted the offer but supplied no answer
// within the polling budget. No remote command has been sent at that point.
var ErrAnswerTimeout = errors.New("Connect WebRTC answer timed out")

type reply struct {
	http.Header
	status int
}

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Connection struct {
	Device              Device
	ICE                 webrtc.Configuration
	client              *Client
	prefix, csrf        string
	ctx                 context.Context
	cancel              context.CancelFunc
	failures            chan string
	interval, timeCheck time.Duration
	mu                  sync.Mutex
	attempted           bool
}

func (s *Connection) Close() error            { s.cancel(); return nil }
func (s *Connection) Failures() <-chan string { return s.failures }

// Prepare uses only observed Connect web requests and this client's own jar.
func (c *Client) Prepare(ctx context.Context, destination string) (*Connection, error) {
	return c.PrepareWithContext(ctx, ctx, destination)
}

func (c *Client) PrepareWithContext(lifetime, ctx context.Context, destination string) (*Connection, error) {
	if c.stage != Authenticated {
		return nil, errors.New("direct login must finish before device discovery")
	}
	owned, cancel := context.WithCancel(lifetime)
	s := &Connection{client: c, ctx: owned, cancel: cancel, failures: make(chan string, 1), interval: time.Second, timeCheck: time.Minute}
	ready := false
	defer func() {
		if !ready {
			cancel()
		}
	}()
	_, b, err := s.request(ctx, "GET", c.root+"/devices", nil, "text/html", "")
	if err != nil {
		return nil, err
	}
	n, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, ErrFlow
	}
	devices := map[string]Device{}
	walk(n, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "meta" && attr(n, "name") == "csrf-token" {
			s.csrf = attr(n, "content")
		}
		if n.Data != "a" {
			return
		}
		u, e := url.Parse(c.root)
		if e != nil {
			return
		}
		u, e = u.Parse(attr(n, "href"))
		if e != nil || !c.permitted(u) || origin(u) != c.root || !devicePath.MatchString(u.Path) || u.RawQuery != "" || u.Fragment != "" {
			return
		}
		id := strings.TrimPrefix(u.Path, "/devices/")
		if _, exists := devices[id]; !exists {
			devices[id] = Device{ID: id, Name: strings.TrimSpace(nodeText(n))}
		}
	})
	var matches []Device
	for _, d := range devices {
		if destination == "" || destination == d.ID || destination == d.Name {
			matches = append(matches, d)
		}
	}
	if len(matches) != 1 {
		return nil, errors.New("no unique authorized device matches the destination")
	}
	if s.csrf == "" || len(s.csrf) > 4096 {
		return nil, errors.New("Connect page has no usable CSRF control")
	}
	s.Device = matches[0]
	s.prefix = "/devices/" + s.Device.ID
	_, b, err = s.request(ctx, "GET", c.root+s.prefix+"/remote-shell-session", nil, "text/html", "")
	if err != nil {
		return nil, err
	}
	n, err = html.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, ErrFlow
	}
	controllers := 0
	var device Device
	walk(n, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		for _, controller := range strings.Fields(attr(n, "data-controller")) {
			if controller == "shell" {
				controllers++
				_ = json.Unmarshal([]byte(attr(n, "data-shell-device-value")), &device)
			}
		}
	})
	if controllers != 1 || device.ID != s.Device.ID || (destination != "" && destination != device.ID && destination != device.Name) {
		return nil, errors.New("Connect shell authorization or device identity was not confirmed; sign in again")
	}
	_, b, err = s.request(ctx, "GET", c.root+s.prefix+"/ice-configuration", nil, "application/json", "")
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &s.ICE) != nil {
		return nil, errors.New("invalid Connect ICE configuration")
	}
	ready = true
	return s, nil
}

func (s *Connection) request(ctx context.Context, method, target string, body []byte, accept, csrf string) (*reply, []byte, error) {
	u, err := url.Parse(target)
	if err != nil || !s.client.permitted(u) || origin(u) != s.client.root {
		return nil, nil, ErrBoundary
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, ErrRequest
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "pi-sh/experimental-direct-login")
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Origin", s.client.root)
		req.Header.Set("X-CSRF-Token", csrf)
	}
	h := *s.client.http
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrBoundary }
	resp, err := h.Do(req)
	if err != nil {
		if errors.Is(err, ErrBoundary) {
			return nil, nil, ErrBoundary
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, ErrRequest
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("Connect request rejected (HTTP %d)", resp.StatusCode)
	}
	meta := &reply{Header: resp.Header, status: resp.StatusCode}
	if resp.StatusCode == http.StatusNoContent || method == http.MethodPost {
		return meta, nil, nil
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), accept) {
		return nil, nil, errors.New("unexpected Connect response type")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(b) > 2*1024*1024 {
		return nil, nil, errors.New("Connect response exceeds local limit or could not be read")
	}
	return meta, b, nil
}

func (s *Connection) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	s.mu.Lock()
	already := s.attempted
	s.attempted = true
	s.mu.Unlock()
	if already || offer.Type != webrtc.SDPTypeOffer || len(offer.SDP) == 0 || len(offer.SDP) > 256*1024 {
		return webrtc.SessionDescription{}, errors.New("invalid or repeated direct signaling offer")
	}
	b, err := json.Marshal(offer)
	if err != nil {
		return webrtc.SessionDescription{}, ErrRequest
	}
	headers, _, err := s.request(ctx, "POST", s.client.root+s.prefix+"/connections", b, "application/json", s.csrf)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	base, _ := url.Parse(s.client.root)
	poll, err := base.Parse(headers.Get("Location"))
	if err != nil || headers.Get("Location") == "" || !s.client.permitted(poll) || origin(poll) != s.client.root || !strings.HasPrefix(poll.Path, s.prefix+"/") || path.Clean(poll.Path) != poll.Path || poll.RawPath != "" {
		return webrtc.SessionDescription{}, ErrBoundary
	}
	for i := 0; i < 120; i++ {
		timer := time.NewTimer(s.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return webrtc.SessionDescription{}, ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return webrtc.SessionDescription{}, s.ctx.Err()
		case <-timer.C:
		}
		_, b, err = s.request(ctx, "GET", poll.String(), nil, "application/json", "")
		if err != nil {
			return webrtc.SessionDescription{}, err
		}
		var result struct {
			Answer *webrtc.SessionDescription `json:"answer"`
		}
		if json.Unmarshal(b, &result) != nil {
			return webrtc.SessionDescription{}, errors.New("invalid Connect signaling response")
		}
		if result.Answer == nil {
			continue
		}
		if result.Answer.Type != webrtc.SDPTypeAnswer || len(result.Answer.SDP) == 0 || len(result.Answer.SDP) > 256*1024 {
			return webrtc.SessionDescription{}, errors.New("invalid Connect WebRTC answer")
		}
		go s.watch()
		return *result.Answer, nil
	}
	return webrtc.SessionDescription{}, ErrAnswerTimeout
}

func (s *Connection) watch() {
	ticker := time.NewTicker(s.timeCheck)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			headers, _, err := s.request(s.ctx, "GET", s.client.root+s.prefix+".json", nil, "application/json", "")
			// The observed official authorization check must return204.
			if err != nil || headers.status != http.StatusNoContent {
				if s.ctx.Err() != nil {
					return
				}
				select {
				case s.failures <- "direct Connect authorization check failed":
				default:
				}
				return
			}
		}
	}
}

func walk(n *html.Node, f func(*html.Node)) {
	f(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, f)
	}
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
	})
	return b.String()
}
