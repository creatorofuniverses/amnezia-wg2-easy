package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ParseConfig reads the [Interface] block of a wg0.conf and extracts the
// S1..S4 padding offsets and H1..H4 header ranges. Keys are case-insensitive.
func ParseConfig(path string) (AwgParams, error) {
	f, err := os.Open(path)
	if err != nil {
		return AwgParams{}, err
	}
	defer f.Close()

	kv := map[string]string{}
	inInterface := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inInterface = strings.EqualFold(line, "[Interface]")
			continue
		}
		if !inInterface {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		kv[key] = val
	}
	if err := sc.Err(); err != nil {
		return AwgParams{}, err
	}

	u32 := func(name string) (uint32, error) {
		v, ok := kv[name]
		if !ok {
			return 0, fmt.Errorf("missing %q in [Interface]", name)
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("bad %q: %w", name, err)
		}
		return uint32(n), nil
	}
	hrange := func(name string) (HRange, error) {
		v, ok := kv[name]
		if !ok {
			return HRange{}, fmt.Errorf("missing %q in [Interface]", name)
		}
		if dash := strings.IndexByte(v, '-'); dash > 0 {
			lo, err := strconv.ParseUint(strings.TrimSpace(v[:dash]), 10, 32)
			if err != nil {
				return HRange{}, fmt.Errorf("bad %q min: %w", name, err)
			}
			hi, err := strconv.ParseUint(strings.TrimSpace(v[dash+1:]), 10, 32)
			if err != nil {
				return HRange{}, fmt.Errorf("bad %q max: %w", name, err)
			}
			return HRange{uint32(lo), uint32(hi)}, nil
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return HRange{}, fmt.Errorf("bad %q: %w", name, err)
		}
		return HRange{uint32(n), uint32(n)}, nil
	}

	var p AwgParams
	var err2 error
	set := func(dst *uint32, name string) {
		if err2 != nil {
			return
		}
		*dst, err2 = u32(name)
	}
	setH := func(dst *HRange, name string) {
		if err2 != nil {
			return
		}
		*dst, err2 = hrange(name)
	}
	set(&p.S1, "s1")
	set(&p.S2, "s2")
	set(&p.S3, "s3")
	set(&p.S4, "s4")
	setH(&p.H1, "h1")
	setH(&p.H2, "h2")
	setH(&p.H3, "h3")
	setH(&p.H4, "h4")
	if err2 != nil {
		return AwgParams{}, err2
	}
	return p, nil
}
