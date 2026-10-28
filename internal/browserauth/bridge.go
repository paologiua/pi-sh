// Package browserauth keeps Connect account authentication inside an authorised
// browser page. A short-lived loopback companion exchanges ICE and SDP in memory;
// it never reads cookies, passwords, authorization headers or CSRF tokens.
package browserauth

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const Origin = "https://connect.raspberrypi.com"

//go:embed companion.js
var companion string

type Configuration struct {
	Device struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"device"`
	ICE webrtc.Configuration `json:"configuration"`
}

type Bridge struct {
	server      *http.Server
	listener    net.Listener
	prefix      string
	destination string
	config      chan Configuration
	offers      chan webrtc.SessionDescription
	answers     chan webrtc.SessionDescription
	undelivered chan webrtc.SessionDescription // an offer whose response write failed
	failures    chan string
	authorized  chan struct{}
	// watch starts one authorization watcher for all exchanges: several
	// watchers would split the companion's single heartbeat between them.
	watch     sync.Once
	heartbeat time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
}

func New(ctx context.Context, destination string) (*Bridge, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot open browser companion loopback listener")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		_ = l.Close()
		return nil, errors.New("cannot create browser companion capability")
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &Bridge{listener: l, prefix: "/" + hex.EncodeToString(nonce[:]) + "/", destination: destination, config: make(chan Configuration, 1), offers: make(chan webrtc.SessionDescription), answers: make(chan webrtc.SessionDescription, 1), undelivered: make(chan webrtc.SessionDescription, 1), failures: make(chan string, 1), ctx: ctx, cancel: cancel}
	b.authorized = make(chan struct{}, 1)
	b.heartbeat = 90 * time.Second
	b.server = &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 130 * time.Second, WriteTimeout: 130 * time.Second, MaxHeaderBytes: 8192}
	go func() { _ = b.server.Serve(l) }()
	go func() { <-ctx.Done(); _ = b.server.Close() }()
	return b, nil
}

// ScriptURL is an ephemeral local capability, not a Connect credential. Display
// it only to the local operator; do not persist it in logs or public artifacts.
func (b *Bridge) ScriptURL() string {
	return "http://" + b.listener.Addr().String() + b.prefix + "companion.js"
}
func (b *Bridge) Close() error            { b.cancel(); return b.server.Close() }
func (b *Bridge) Failures() <-chan string { return b.failures }

func (b *Bridge) Configuration(ctx context.Context) (Configuration, error) {
	select {
	case c := <-b.config:
		return c, nil
	case message := <-b.failures:
		return Configuration{}, errors.New("browser authentication setup failed: " + message)
	case <-ctx.Done():
		return Configuration{}, ctx.Err()
	case <-b.ctx.Done():
		return Configuration{}, errors.New("browser companion closed")
	}
}

func (b *Bridge) Exchange(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	// An abandoned earlier exchange may have left its answer or its offer here;
	// neither belongs to this offer.
	select {
	case <-b.answers:
	default:
	}
	select {
	case <-b.undelivered:
	default:
	}
	select {
	case b.offers <- offer:
	case message := <-b.failures:
		return webrtc.SessionDescription{}, errors.New("browser signaling failed before offer delivery: " + message)
	case <-ctx.Done():
		return webrtc.SessionDescription{}, ctx.Err()
	case <-b.ctx.Done():
		return webrtc.SessionDescription{}, errors.New("browser companion closed")
	}
	select {
	case a := <-b.answers:
		b.watch.Do(func() { go b.watchAuthorization(b.heartbeat) })
		return a, nil
	case message := <-b.failures:
		return webrtc.SessionDescription{}, errors.New("browser signaling failed: " + message)
	case <-ctx.Done():
		return webrtc.SessionDescription{}, ctx.Err()
	case <-b.ctx.Done():
		return webrtc.SessionDescription{}, errors.New("browser companion closed")
	}
}

func (b *Bridge) watchAuthorization(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-b.authorized:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		case <-timer.C:
			select {
			case b.failures <- "browser authorization heartbeat expired":
			default:
			}
			return
		}
	}
}

func (b *Bridge) serve(w http.ResponseWriter, r *http.Request) {
	// The unpredictable capability, exact Host and exact browser origin are all
	// required, including for preflight. No wildcard CORS or LAN listener.
	if r.Host != b.listener.Addr().String() || r.Header.Get("Origin") != Origin || !strings.HasPrefix(r.URL.Path, b.prefix) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", Origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, b.prefix)
	if r.Method == http.MethodGet {
		switch path {
		case "alive":
			w.WriteHeader(http.StatusNoContent)
		case "companion.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			base, _ := json.Marshal("http://" + b.listener.Addr().String() + b.prefix)
			dest, _ := json.Marshal(b.destination)
			_, _ = io.WriteString(w, "("+companion+")("+string(base)+","+string(dest)+");")
		case "offer":
			var offer webrtc.SessionDescription
			select {
			case offer = <-b.undelivered:
			default:
			}
			if offer.SDP != "" {
				b.deliver(w, r, offer)
				return
			}
			select {
			case offer = <-b.offers:
				b.deliver(w, r, offer)
			case <-r.Context().Done():
			case <-b.ctx.Done():
				w.WriteHeader(http.StatusGone)
			}
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	switch path {
	case "authorized":
		select {
		case b.authorized <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	case "configuration":
		var c Configuration
		if err := dec.Decode(&c); err != nil || c.Device.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if b.destination != "" && b.destination != c.Device.ID && b.destination != c.Device.Name {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case b.config <- c:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusConflict)
		}
	case "answer":
		var a webrtc.SessionDescription
		if err := dec.Decode(&a); err != nil || a.Type != webrtc.SDPTypeAnswer || len(a.SDP) > 256*1024 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case b.answers <- a:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusConflict)
		}
	case "failure":
		// Only translate recognised fixed codes. Never propagate browser exception
		// text, response bodies, URLs or unrecognised codes into diagnostics.
		var failure struct {
			Code string `json:"code"`
		}
		_ = dec.Decode(&failure)
		select {
		case b.failures <- failureMessage(failure.Code):
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func failureMessage(code string) string {
	switch code {
	case "reauthentication_required":
		return "Connect requires recent sign-in; open Remote shell in Connect, complete authentication, return to Devices, then run the command again"
	case "device_not_unique":
		return "no unique authorised device matches; check the device name or use its ID"
	case "device_mismatch":
		return "the browser device does not match the requested destination"
	case "shell_page_unavailable":
		return "Connect returned no shell page; open Remote shell in the browser to check access"
	case "access_denied":
		return "Connect denied access; refresh the signed-in Devices page and check permissions"
	case "connect_request_failed":
		return "Connect request failed; check sign-in and service availability"
	case "ice_configuration_failed":
		return "could not read ICE configuration; check Connect sign-in and device availability"
	case "invalid_connection_location":
		return "Connect returned an unexpected signaling location"
	case "answer_timeout":
		return "Connect did not return a WebRTC answer before the timeout"
	case "local_companion_failed":
		return "local companion request failed; run the command again and attach its new companion"
	default:
		return "browser operation failed"
	}
}

// deliver writes an offer to a polling companion. If the poll already ended
// (the browser's long-poll timeout), the offer is kept for the next poll
// instead of being lost while Exchange waits for its answer.
func (b *Bridge) deliver(w http.ResponseWriter, r *http.Request, offer webrtc.SessionDescription) {
	w.Header().Set("Content-Type", "application/json")
	err := r.Context().Err()
	if err == nil {
		err = json.NewEncoder(w).Encode(offer)
	}
	if err == nil {
		err = http.NewResponseController(w).Flush()
	}
	if err == nil {
		return
	}
	select {
	case b.undelivered <- offer:
	default:
	}
}
