package forwarderbs

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"fmt"
)

// KeyDERForCupsBlob returns the private key in the DER form expected by Basic Station in the CUPS
// credentials blob: SEC1 (EC PRIVATE KEY) for ECDSA, PKCS#1 for RSA. Using PKCS#8 causes the station
// to misparse lengths ("Lengths do not align segment_len=2270 parsed_len=36698").
func KeyDERForCupsBlob(key crypto.PrivateKey) ([]byte, error) {
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		return x509.MarshalECPrivateKey(k)
	case *rsa.PrivateKey:
		return x509.MarshalPKCS1PrivateKey(k), nil
	default:
		return x509.MarshalPKCS8PrivateKey(key)
	}
}

// CredBlobFromTrustCertKey builds a CUPS credentials blob (trust + cert + key in DER) for X.509 auth.
func CredBlobFromTrustCertKey(trustDER, certDER, keyDER []byte) []byte {
	out := make([]byte, 0, len(trustDER)+len(certDER)+len(keyDER))
	out = append(out, trustDER...)
	out = append(out, certDER...)
	out = append(out, keyDER...)
	return out
}

// BuildCupsResponseWithCreds builds a full CUPS update-info binary response with the given URIs and credential blobs.
// Trailing sig/upd fields are written as zero (no signature, no update data).
func BuildCupsResponseWithCreds(proxyCupsURI, proxyTcURI string, cupsCred, tcCred []byte) ([]byte, error) {
	if len(proxyCupsURI) > 255 || len(proxyTcURI) > 255 || len(cupsCred) > 65535 || len(tcCred) > 65535 {
		return nil, fmt.Errorf("proxy URIs must be ≤255 bytes")
	}
	// Layout: cupsUriLen(1) cupsUri tcUriLen(1) tcUri cupsCredLen(2 LE) cupsCred tcCredLen(2 LE) tcCred sigLen(4) keyCRC(4) sig updLen(4) updData
	sigUpdLen := 4 + 4 // sigLen=0, updLen=0; no keyCRC when no signature
	n := 1 + len(proxyCupsURI) + 1 + len(proxyTcURI) + 2 + len(cupsCred) + 2 + len(tcCred) + sigUpdLen
	out := make([]byte, 0, n)
	out = append(out, byte(len(proxyCupsURI)))
	out = append(out, proxyCupsURI...)
	out = append(out, byte(len(proxyTcURI)))
	out = append(out, proxyTcURI...)
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(cupsCred)))
	out = append(out, lenBuf[:]...)
	out = append(out, cupsCred...)
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(tcCred)))
	out = append(out, lenBuf[:]...)
	out = append(out, tcCred...)
	// sigLen=0, updLen=0 (8 zero bytes)
	for i := 0; i < sigUpdLen; i++ {
		out = append(out, 0)
	}
	return out, nil
}

// CupsNullResponseLen is the length of the CUPS "no update" response (14 zero bytes).
const CupsNullResponseLen = 14

// IsCupsNullResponse reports whether body is the canonical CUPS null response (no update pending).
// Per spec we pass it through unchanged so the station keeps its current config.
func IsCupsNullResponse(body []byte) bool {
	if len(body) != CupsNullResponseLen {
		return false
	}
	for _, b := range body {
		if b != 0 {
			return false
		}
	}
	return true
}

// RewriteCupsResponseURIs replaces the cupsUri and tcUri in the binary CUPS response with proxyCupsURI
// and proxyTcURI so the station is told to connect to the proxy, not the upstream server.
// URIs must be 255 bytes or less (length is 1 byte).
// Do not call for the 14-byte null response; use IsCupsNullResponse and pass through instead.
func RewriteCupsResponseURIs(body []byte, proxyCupsURI, proxyTcURI string) ([]byte, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("cups response too short")
	}
	cun := int(body[0])
	if cun > 255 || 1+cun > len(body) {
		return nil, fmt.Errorf("cups response: invalid cupsUriLen")
	}
	off := 1 + cun
	if off >= len(body) {
		return nil, fmt.Errorf("cups response: no tcUriLen")
	}
	tun := int(body[off])
	off++
	if off+tun > len(body) {
		return nil, fmt.Errorf("cups response: tcUri overflow")
	}
	rest := body[off+tun:]
	if len(proxyCupsURI) > 255 || len(proxyTcURI) > 255 {
		return nil, fmt.Errorf("proxy URIs must be ≤255 bytes")
	}
	out := make([]byte, 0, 2+len(proxyCupsURI)+len(proxyTcURI)+len(rest))
	out = append(out, byte(len(proxyCupsURI)))
	out = append(out, proxyCupsURI...)
	out = append(out, byte(len(proxyTcURI)))
	out = append(out, proxyTcURI...)
	out = append(out, rest...)
	return out, nil
}

// CUPSUpdateResponse is the binary response from CUPS POST /update-info (doc.sm.tc/station/cupsproto).
// Layout: cupsUriLen(1) cupsUri(cun) tcUriLen(1) tcUri(tun) cupsCredLen(2 LE) cupsCred(ccn) tcCredLen(2 LE) tcCred(tcn) sigLen(4) keyCRC(4) sig(sig) updLen(4) updData(udn)
// Credentials blob = trust(DER) + cert(DER) + key(DER) for X.509 auth.

// ParsedCUPSResponse holds the result of parsing a full CUPS update-info response.
type ParsedCUPSResponse struct {
	CupsURI, TcURI string

	CupsCred []byte // cups credentials blob (trust+cert+key)
	TcCred   []byte // tc credentials blob (trust+cert+key)
	Trailing []byte // sigLen(4) keyCRC(4) sig updLen(4) updData — preserved when replacing creds
}

// ParseCUPSUpdateResponse parses the binary CUPS response and returns credential blobs only (kept for backward compatibility).
func parseCUPSUpdateResponse(body []byte) (cupsCred, tcCred []byte, err error) {
	p, err := ParseCUPSUpdateResponseFull(body)
	if err != nil {
		return nil, nil, err
	}
	return p.CupsCred, p.TcCred, nil
}

// ParseCUPSUpdateResponseFull parses the full binary CUPS response and returns credential blobs plus trailing sig/update data.
func ParseCUPSUpdateResponseFull(body []byte) (*ParsedCUPSResponse, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("cups response too short")
	}
	off := 0
	// cupsUriLen (1) + cupsUri
	cun := int(body[off])
	off++
	if off+cun > len(body) {
		return nil, fmt.Errorf("cups response: cupsUri overflow")
	}
	cupsURI := string(body[off : off+cun])
	off += cun
	// tcUriLen (1) + tcUri
	if off >= len(body) {
		return nil, fmt.Errorf("cups response: no tcUriLen")
	}
	tun := int(body[off])
	off++
	if off+tun > len(body) {
		return nil, fmt.Errorf("cups response: tcUri overflow")
	}
	tcURI := string(body[off : off+tun])
	off += tun
	// cupsCredLen (2 LE)
	if off+2 > len(body) {
		return nil, fmt.Errorf("cups response: no cupsCredLen")
	}
	ccn := int(binary.LittleEndian.Uint16(body[off:]))
	off += 2
	if off+ccn > len(body) {
		return nil, fmt.Errorf("cups response: cupsCred overflow")
	}
	cupsCred := body[off : off+ccn]
	off += ccn
	// tcCredLen (2 LE)
	if off+2 > len(body) {
		return nil, fmt.Errorf("cups response: no tcCredLen")
	}
	tcn := int(binary.LittleEndian.Uint16(body[off:]))
	off += 2
	if off+tcn > len(body) {
		return nil, fmt.Errorf("cups response: tcCred overflow")
	}
	tcCred := body[off : off+tcn]
	off += tcn
	trailing := body[off:]
	if err := validateCUPSTail(trailing); err != nil {
		return nil, err
	}
	return &ParsedCUPSResponse{CupsURI: cupsURI, TcURI: tcURI, CupsCred: cupsCred, TcCred: tcCred, Trailing: trailing}, nil
}

// BuildCupsResponseFull builds a full CUPS update-info binary response with proxy URIs, proxy credential blobs,
// and the original trailing segment (sigLen, keyCRC, sig, updLen, updData) from the upstream response.
func BuildCupsResponseFull(proxyCupsURI, proxyTcURI string, cupsCred, tcCred, trailing []byte) ([]byte, error) {
	if len(proxyCupsURI) > 255 || len(proxyTcURI) > 255 || len(cupsCred) > 65535 || len(tcCred) > 65535 {
		return nil, fmt.Errorf("proxy URIs must be ≤255 bytes")
	}
	n := 1 + len(proxyCupsURI) + 1 + len(proxyTcURI) + 2 + len(cupsCred) + 2 + len(tcCred) + len(trailing)
	out := make([]byte, 0, n)
	out = append(out, byte(len(proxyCupsURI)))
	out = append(out, proxyCupsURI...)
	out = append(out, byte(len(proxyTcURI)))
	out = append(out, proxyTcURI...)
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(cupsCred)))
	out = append(out, lenBuf[:]...)
	out = append(out, cupsCred...)
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(len(tcCred)))
	out = append(out, lenBuf[:]...)
	out = append(out, tcCred...)
	out = append(out, trailing...)
	return out, nil
}

// derObjectLength returns the length in bytes of the first DER object in b (tag + length + content).
func derObjectLength(b []byte) (int, error) {
	if len(b) < 2 {
		return 0, fmt.Errorf("der too short")
	}
	// b[0] = tag, b[1] = length or length-of-length
	l := b[1]
	if l < 128 {
		total := 2 + int(l)
		if total > len(b) {
			return 0, fmt.Errorf("der object overflows")
		}
		return total, nil
	}
	n := int(l & 0x7f)
	if n == 0 || n > 4 {
		return 0, fmt.Errorf("invalid DER length encoding")
	}
	if len(b) < 2+n {
		return 0, fmt.Errorf("der length bytes overflow")
	}
	var contentLen uint64
	for i := 0; i < n; i++ {
		contentLen = contentLen<<8 | uint64(b[2+i])
	}
	total := uint64(2+n) + contentLen
	if total > uint64(len(b)) {
		return 0, fmt.Errorf("der object overflows")
	}
	return int(total), nil
}

// extractCertKeyFromCredBlob parses a CUPS credentials blob (trust + cert + key in DER)
// and returns the gateway's cert (DER) and private key (DER). Trust is skipped.
// For X.509 auth: trust = CA cert DER, cert = gateway cert DER, key = gateway key DER.
func extractCertKeyFromCredBlob(blob []byte) (certDER, keyDER []byte, err error) {
	if len(blob) == 0 {
		return nil, nil, nil
	}
	rest := blob
	// First object = trust (skip)
	n, err := derObjectLength(rest)
	if err != nil {
		return nil, nil, err
	}
	rest = rest[n:]
	if len(rest) == 0 {
		return nil, nil, fmt.Errorf("no cert in cred blob")
	}
	// Second object = cert
	n, err = derObjectLength(rest)
	if err != nil {
		return nil, nil, err
	}
	certDER = make([]byte, n)
	copy(certDER, rest[:n])
	rest = rest[n:]
	if len(rest) == 0 {
		return nil, nil, fmt.Errorf("no key in cred blob")
	}
	// Third object = key
	n, err = derObjectLength(rest)
	if err != nil {
		return nil, nil, err
	}
	keyDER = make([]byte, n)
	copy(keyDER, rest[:n])
	return certDER, keyDER, nil
}

func validateCUPSTail(tail []byte) error {
	if len(tail) < 8 {
		return fmt.Errorf("truncated CUPS trailer")
	}
	sig := uint64(binary.LittleEndian.Uint32(tail))
	if sig > 4096 || (sig > 0 && sig < 8) || sig+8 > uint64(len(tail)) {
		return fmt.Errorf("invalid CUPS signature length")
	}
	update := uint64(binary.LittleEndian.Uint32(tail[4+int(sig):]))
	if sig+8+update != uint64(len(tail)) {
		return fmt.Errorf("invalid CUPS update length")
	}
	return nil
}
