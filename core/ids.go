package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RandHex returns n random bytes as 2n lowercase hex characters.
func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on any supported platform; if it does,
		// continuing with predictable identifiers would be worse.
		panic(fmt.Sprintf("core: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("core: crypto/rand: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ParseTime parses the timestamp formats these APIs use.
//
// Recon emits ISO 8601 / RFC 3339. SCRUFF emits RFC 1123 HTTP dates — including
// for the register response's "now" field, which looks like it should be a unix
// integer but is not. A zero Time is returned for an empty string.
func ParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999",
		http.TimeFormat,
		time.RFC1123,
		time.RFC1123Z,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("core: unrecognised timestamp %q", s)
}
