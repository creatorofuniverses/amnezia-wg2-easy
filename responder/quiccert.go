package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"sync"
	"time"
)

// certResolver mints and caches one self-signed ECDSA cert per requested SNI.
// It backs tls.Config.GetCertificate for the embedded QUIC handshake endpoint.
// The self-signed cert is a known weaker fingerprint (Review R2-6), inherited
// from the Rust original; a chain-validating prober still sees it is self-signed.
type certResolver struct {
	mu       sync.Mutex
	cache    map[string]*tls.Certificate
	fallback string // QUIC_CERT_DOMAIN, used when the ClientHello has no SNI
}

func newCertResolver(domain string) *certResolver {
	return &certResolver{cache: make(map[string]*tls.Certificate), fallback: domain}
}

func (r *certResolver) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := hello.ServerName
	if name == "" {
		name = r.fallback
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[name]; ok {
		return c, nil
	}
	c, err := selfSignedCert(name)
	if err != nil {
		return nil, err
	}
	r.cache[name] = c
	return c, nil
}

// selfSignedCert builds a 1-year self-signed ECDSA P-256 leaf for domain, with
// Leaf populated (so callers can read DNSNames without re-parsing).
func selfSignedCert(domain string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}
