package settings

import (
	"fmt"
	"math"
	"strconv"
)

const (
	// VolumeMax is master_volume's top step: full gain, shown as "max".
	VolumeMax = 50
	// VolumeDefault is master_volume's default (0.80 gain).
	VolumeDefault = 40
)

// migrateVolume is master_volume's Migrate. Before the 0–50 scale the value
// was a float gain from 0.0 to 1.0. The rule that tells the two apart:
//
//   - a float (a YAML float literal such as 0.8, 1.0 or 0.0, or a Go
//     float64) from 0 to 1 is the old scale and becomes round(v*50);
//   - a whole float above 1 (40.0) is the new scale;
//   - a string that is not a whole number but parses as a float (the CLI's
//     "0.8", "1.0") follows the same two rules;
//   - anything else (ints, whole-number strings, other values) passes
//     through to coercion unchanged. So 1 and 0 are the new scale, while
//     1.0 and 0.0 are the old one.
//
// A non-whole float outside 0–1 (1.5, -0.2) or NaN/Inf is refused rather
// than truncated.
func migrateVolume(raw any) (any, error) {
	var f float64
	switch v := raw.(type) {
	case float64:
		f = v
	case string:
		if _, err := strconv.Atoi(v); err == nil {
			return v, nil
		}
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return v, nil // coercion reports "not an int"
		}
		f = parsed
	default:
		return raw, nil
	}
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		return nil, fmt.Errorf("master_volume: %v is not a number from 0 to %d", f, VolumeMax)
	case f >= 0 && f <= 1:
		return int(math.Round(f * VolumeMax)), nil
	case f == math.Trunc(f) && math.Abs(f) <= 1<<40:
		return int(f), nil
	}
	return nil, fmt.Errorf("master_volume: %v is not a whole number from 0 to %d", f, VolumeMax)
}

// VolumeGain is the playback gain (0.0–1.0) for a master_volume value: v/50,
// clamped. A value that is not an int reads as the default.
func VolumeGain(v any) float64 {
	n, ok := v.(int)
	if !ok {
		n = VolumeDefault
	}
	return min(max(float64(n)/VolumeMax, 0), 1)
}
