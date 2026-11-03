package webauth

import (
	"crypto/tls"
	"crypto/x509"
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
		category = "TLS certificate verification" + certificateHint(certificate.Err)
	case errors.As(err, &network) && network.Timeout():
		category = "timeout"
	case errors.As(err, &dns):
		category = "DNS"
	case errors.As(err, &operation):
		category = "connection"
	}
	return fmt.Errorf("%w (%s)", ErrRequest, category)
}

// certificateHint names the local cause behind common verification failures.
// The hints are fixed text: certificate subjects and hosts are not included.
func certificateHint(err error) string {
	var unknown x509.UnknownAuthorityError
	var roots x509.SystemRootsError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	switch {
	case errors.As(err, &unknown), errors.As(err, &roots):
		return ": no trusted root certificate; install or update the system CA certificates" +
			" (Alpine/iSH: apk add --upgrade ca-certificates) or set SSL_CERT_FILE"
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return ": certificate expired or not yet valid; check the system date and time"
	case errors.As(err, &hostname):
		return ": certificate does not match the server name"
	}
	return ""
}
