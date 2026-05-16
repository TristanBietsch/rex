package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 tk"},
		{200, "200 tk"},
		{999, "999 tk"},
		{1500, "1.5K tk"},
		{12345, "12.3K tk"},
		{99999, "100.0K tk"},
		{123456, "123K tk"},
		{1000000, "1000K tk"},
	}
	for _, c := range cases {
		got := formatTokens(c.n)
		require.Equal(t, c.want, got, "formatTokens(%d)", c.n)
	}
}

func TestModelLabel_WithTokens(t *testing.T) {
	cases := []struct {
		tokens int64
		effort string
		want   string
	}{
		{0, "", "gpt-5.2"},
		{0, "high", "gpt-5.2 · high"},
		{200, "", "gpt-5.2 · 200 tk"},
		{1500, "low", "gpt-5.2 · low · 1.5K tk"},
		{123456, "", "gpt-5.2 · 123K tk"},
	}
	for _, c := range cases {
		s := protocol.SessionSummary{ModelID: "gpt-5.2", Effort: c.effort, Tokens: c.tokens}
		got := modelLabel(s)
		require.Equal(t, c.want, got, "modelLabel(tokens=%d, effort=%q)", c.tokens, c.effort)
	}
}

func TestFleetColor_Deterministic(t *testing.T) {
	// Same name must always produce the same color.
	for _, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		c1 := fleetColor(name)
		c2 := fleetColor(name)
		require.Equal(t, c1, c2, "fleetColor(%q) not deterministic", name)
	}
}

func TestFleetColor_Distinct(t *testing.T) {
	// 10 distinct names should map to at least 4 distinct colors (palette has 10 entries).
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	seen := map[string]struct{}{}
	for _, n := range names {
		seen[string(fleetColor(n))] = struct{}{}
	}
	require.GreaterOrEqual(t, len(seen), 4, "expected at least 4 distinct fleet colors")
}
