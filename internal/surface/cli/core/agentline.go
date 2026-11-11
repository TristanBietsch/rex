package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Field is one key=value pair of an agent line.
type Field struct {
	Key string
	Val any
}

// EncodeLine renders one agent-mode line: `<kind> key=value key=value ...`.
//
// A value is bare when it is non-empty and every character is one of
// [A-Za-z0-9._:/@+%-]. Anything else, including the empty string, is
// Go-quoted with strconv.Quote, which escapes ESC, newlines, bidi controls
// and every other non-printable, so a value can never break the line or
// smuggle a terminal sequence. Integers, bools and time.Time (RFC 3339 UTC)
// are bare. Durations are passed as integer seconds by the caller.
//
// kind and keys must be lowercase snake_case; EncodeLine panics otherwise,
// because they are written in code, never taken from data.
func EncodeLine(kind string, fields ...Field) string {
	if !validIdent(kind) {
		panic(fmt.Sprintf("core.EncodeLine: invalid kind %q", kind))
	}
	var b strings.Builder
	b.WriteString(kind)
	for _, f := range fields {
		if !validIdent(f.Key) {
			panic(fmt.Sprintf("core.EncodeLine: invalid key %q", f.Key))
		}
		b.WriteByte(' ')
		b.WriteString(f.Key)
		b.WriteByte('=')
		b.WriteString(encodeValue(f.Val))
	}
	return b.String()
}

func validIdent(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func encodeValue(v any) string {
	switch v := v.(type) {
	case nil:
		return `""`
	case string:
		return quoteValue(v)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	case fmt.Stringer:
		return quoteValue(v.String())
	default:
		return quoteValue(fmt.Sprint(v))
	}
}

func quoteValue(s string) string {
	if s != "" && isBare(s) {
		return s
	}
	return strconv.Quote(s)
}

func isBare(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("._:/@+%-", c) >= 0:
		default:
			return false
		}
	}
	return true
}
