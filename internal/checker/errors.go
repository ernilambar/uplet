package checker

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
)

func isTimeoutErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

func isDNSNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && !dnsErr.IsTimeout
}

func isDNSTimeout(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsTimeout
}

// isTLSCertError reports whether err is a definitive certificate
// validation failure (expired/self-signed/unknown-CA/hostname mismatch),
// as opposed to a transient handshake timeout.
func isTLSCertError(err error) bool {
	var certErr x509.CertificateInvalidError
	var authErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	if errors.As(err, &certErr) || errors.As(err, &authErr) || errors.As(err, &hostErr) {
		return true
	}
	var verifyErr *tls.CertificateVerificationError
	return errors.As(err, &verifyErr)
}

// isTransientErr reports whether err is a transient failure worth
// retrying: dial timeout, connection reset, TLS-handshake timeout, and
// similar transport hiccups. DNS errors, blocked targets, TLS certificate
// failures, and connection-refused are definitive and are never retried.
func isTransientErr(err error) bool {
	if err == nil || errors.Is(err, errBlockedTarget) {
		return false
	}
	if isTLSCertError(err) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && errors.Is(opErr.Err, syscall.ECONNREFUSED) {
		return false
	}
	return true
}
