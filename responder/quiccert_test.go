package main

import (
	"crypto/tls"
	"testing"
)

func TestCertResolverMintsForSNI(t *testing.T) {
	r := newCertResolver("cloudflare.com")
	cert, err := r.getCertificate(&tls.ClientHelloInfo{ServerName: "example.org"})
	if err != nil {
		t.Fatalf("getCertificate: %v", err)
	}
	if cert == nil || cert.Leaf == nil {
		t.Fatal("nil cert/leaf")
	}
	found := false
	for _, n := range cert.Leaf.DNSNames {
		if n == "example.org" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cert DNSNames %v missing requested SNI", cert.Leaf.DNSNames)
	}
}

func TestCertResolverFallbackAndCaches(t *testing.T) {
	r := newCertResolver("cloudflare.com")
	// Empty SNI -> fallback domain.
	c1, err := r.getCertificate(&tls.ClientHelloInfo{ServerName: ""})
	if err != nil {
		t.Fatalf("getCertificate: %v", err)
	}
	if len(c1.Leaf.DNSNames) == 0 || c1.Leaf.DNSNames[0] != "cloudflare.com" {
		t.Fatalf("fallback DNSNames = %v, want [cloudflare.com]", c1.Leaf.DNSNames)
	}
	// Same name -> same cached *tls.Certificate pointer.
	c2, _ := r.getCertificate(&tls.ClientHelloInfo{ServerName: ""})
	if c1 != c2 {
		t.Fatal("expected cached cert to be reused")
	}
}
