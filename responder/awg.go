package main

// HRange is an inclusive AmneziaWG header magic range [Min, Max].
type HRange struct {
	Min, Max uint32
}

// Contains reports whether v falls within the inclusive range.
func (r HRange) Contains(v uint32) bool {
	return v >= r.Min && v <= r.Max
}

// AwgParams holds the S-padding offsets and H header ranges read from wg0.conf.
// S1/H1 = handshake-init, S2/H2 = handshake-response, S3/H3 = cookie-reply,
// S4/H4 = transport-data.
type AwgParams struct {
	S1, S2, S3, S4 uint32
	H1, H2, H3, H4 HRange
}
