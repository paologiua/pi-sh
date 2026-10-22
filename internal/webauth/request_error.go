package webauth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
)

// requestFailure retains only an allowlisted category. In particular, wrapping
// the original error would disclose callback query parameters or proxy details.
func requestFailure(err error) error {
	category := "transport"
	var certificate *tls.CertificateVerificationError
	var dns *net.DNSError
	var network net.Error
	var operation *net.OpError
	switch {
	case errors.As(err, &certificate):
		category = "TLS certificate verification"
	case errors.As(err, &network) && network.Timeout():
		category = "timeout"
	case errors.As(err, &dns):
		category = "DNS"
	case errors.As(err, &operation):
		category = "connection"
	}
	return fmt.Errorf("%w (%s)", ErrRequest, category)
}
