package webauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type requestErrorTransport func(*http.Request) (*http.Response, error)

func (f requestErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequestFailureCategoriesRedactUnderlyingDetails(t *testing.T) {
	secret := "synthetic-secret-callback"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: secret, Name: secret}, "DNS"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"certificate", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "TLS certificate verification"},
		{"connection", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New(secret)}, "connection"},
		{"other", errors.New(secret), "transport"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.http.Transport = requestErrorTransport(func(*http.Request) (*http.Response, error) { return nil, tc.err })
			u, _ := url.Parse(c.root + "/callback?code=" + secret)
			_, err := c.load(context.Background(), http.MethodGet, u, nil)
			if !errors.Is(err, ErrRequest) || err.Error() != ErrRequest.Error()+" ("+tc.want+")" {
				t.Fatalf("category: %v", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), c.root) {
				t.Fatal("unsafe diagnostic")
			}
		})
	}
}

type failingLoginBody struct{}

func (failingLoginBody) Read([]byte) (int, error) { return 0, errors.New("synthetic-secret-body") }
func (failingLoginBody) Close() error             { return nil }

func TestResponseFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.ReadCloser
		want string
	}{
		{"read", failingLoginBody{}, "response read"},
		{"limit", io.NopCloser(strings.NewReader(strings.Repeat("x", 2*1024*1024+1))), "response too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.http.Transport = requestErrorTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: tc.body, Request: r}, nil
			})
			u, _ := url.Parse(c.root)
			_, err := c.load(context.Background(), http.MethodGet, u, nil)
			if !errors.Is(err, ErrRequest) || err.Error() != ErrRequest.Error()+" ("+tc.want+")" {
				t.Fatalf("category: %v", err)
			}
		})
	}
}
