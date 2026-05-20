// Package ids generates session IDs and derives short ids.
package ids

import (
	"crypto/rand"
	"fmt"
)

// NewSessionID returns a random RFC 4122 v4 UUID.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Should never happen on a healthy system; panic is acceptable per Power-of-Ten escape clause.
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ShortID returns the first 4 hex chars of an id.
func ShortID(id string) string {
	if len(id) < 4 {
		return id
	}
	return id[:4]
}

// ExtendShortID returns the smallest prefix of id (>=4 chars) not present in taken.
// Used to disambiguate when two sessions happen to share the first four hex.
func ExtendShortID(id string, taken map[string]struct{}) string {
	for n := 4; n <= len(id); n++ {
		if id[n-1] == '-' { // skip the dash positions in UUIDs
			continue
		}
		candidate := id[:n]
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
	return id
}
