package main

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConf = `[Interface]
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = aGVsbG8=
Jc = 5
Jmin = 50
Jmax = 1000
S1 = 42
S2 = 88
S3 = 33
S4 = 120
H1 = 100-200
H2 = 300-400
H3 = 500-600
H4 = 700

[Peer]
PublicKey = d29ybGQ=
AllowedIPs = 10.8.0.2/32
`

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wg0.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseConfig(t *testing.T) {
	p, err := ParseConfig(writeTemp(t, sampleConf))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if p.S1 != 42 || p.S2 != 88 || p.S3 != 33 || p.S4 != 120 {
		t.Errorf("S mismatch: %+v", p)
	}
	if p.H1 != (HRange{100, 200}) || p.H2 != (HRange{300, 400}) {
		t.Errorf("H1/H2 mismatch: %+v", p)
	}
	// Single value parses to a point range.
	if p.H4 != (HRange{700, 700}) {
		t.Errorf("H4 point range mismatch: %+v", p.H4)
	}
}

func TestParseConfigMissingKey(t *testing.T) {
	// Drop S4 -> must error.
	body := "[Interface]\nS1 = 1\nS2 = 2\nS3 = 3\nH1 = 1-2\nH2 = 3-4\nH3 = 5-6\nH4 = 7-8\n"
	if _, err := ParseConfig(writeTemp(t, body)); err == nil {
		t.Fatal("expected error for missing S4")
	}
}
