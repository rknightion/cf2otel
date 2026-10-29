package aigateway

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

// A byte cap that lands inside a multi-byte rune produced a string the OTLP
// protobuf marshal rejects, which stalled the whole commit window.
func TestCapBodyKeepsWholeRunes(t *testing.T) {
	body := json.RawMessage(`{"text":"` + "aé€😀" + `"}`)
	for limit := int64(1); limit <= int64(len(body)); limit++ {
		got, truncated := capBody(body, limit)
		if !utf8.Valid(got) {
			t.Fatalf("limit %d: capped body is not valid UTF-8: % x", limit, got)
		}
		if int64(len(got)) > limit {
			t.Fatalf("limit %d: capped body has %d bytes", limit, len(got))
		}
		if truncated != (limit < int64(len(body))) {
			t.Fatalf("limit %d: truncated=%v", limit, truncated)
		}
	}
}

func TestCapStringKeepsWholeRunes(t *testing.T) {
	s := "agent/é€😀"
	for n := 0; n <= len(s); n++ {
		got := capString(s, n)
		if !utf8.ValidString(got) || len(got) > n {
			t.Fatalf("n %d: capString = %q", n, got)
		}
	}
}
