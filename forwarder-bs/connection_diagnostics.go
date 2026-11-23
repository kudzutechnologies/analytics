package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Log origins only: upstream paths, query strings, and userinfo can contain secrets.
func upstreamLogOrigin(raw string) string {
	if raw == "" {
		return "unchanged"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "invalid"
	}
	return u.Scheme + "://" + u.Host
}

// Do not log arbitrary error text: transports can embed URLs or header values.
func upstreamFailureReason(err error) string {
	var handshake *upstreamHandshakeError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var tlsVerification *tls.CertificateVerificationError
	var network net.Error
	switch {
	case errors.As(err, &handshake):
		return fmt.Sprintf("HTTP %d", handshake.status)
	case errors.As(err, &unknownCA):
		return "untrusted upstream certificate"
	case errors.As(err, &hostname):
		return "upstream certificate hostname mismatch"
	case errors.As(err, &invalidCert):
		return "invalid upstream certificate"
	case errors.As(err, &tlsVerification):
		return "upstream TLS certificate verification failed"
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case errors.As(err, &network) && network.Timeout():
		return "upstream connection timeout"
	default:
		return "upstream connection failed (network, TLS, or routing)"
	}
}
