package protocol

import "strings"

// MsgClearing is the bad_state message for an intent that lost the race with
// the retire sweep: the session is already being cleared.
const MsgClearing = "session is already being cleared"

// ValidResumeID reports whether s is a canonical UUID (8-4-4-4-12 hex, either
// case). A resume id becomes one argv element of a spawned agent, so the
// check also rules out a leading "-" (flag injection), whitespace and NUL.
func ValidResumeID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexByte(c) {
				return false
			}
		}
	}
	return true
}

// NormalizeResumeID returns s lowercased and true when it is a valid resume
// id, else "" and false. Every ingest point (hook, regex capture, spawn)
// stores the normalized form.
func NormalizeResumeID(s string) (string, bool) {
	if !ValidResumeID(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
