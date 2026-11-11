package summarizer

// Detail is how much a description says. It sets the rune cap the cleaned
// text is clamped to, the token budget of the call, and the length rule in
// the prompt.
type Detail int32

// The zero value is DetailMedium, the default, so an unset Config.Detail
// means medium.
const (
	DetailMedium Detail = iota
	DetailShort
	DetailLong
)

// ParseDetail maps a summary_detail setting value to a Detail; anything
// unknown is medium, the default.
func ParseDetail(s string) Detail {
	switch s {
	case "short":
		return DetailShort
	case "long":
		return DetailLong
	}
	return DetailMedium
}

type detailCaps struct{ maxRunes, numPredict int }

var detailTable = [...]detailCaps{
	DetailShort:  {60, 40},
	DetailMedium: {110, 64},
	DetailLong:   {180, 100},
}

// MaxRunes is the longest cleaned description at this level.
func (d Detail) MaxRunes() int { return detailTable[d.valid()].maxRunes }

// NumPredict is the model's token budget at this level.
func (d Detail) NumPredict() int { return detailTable[d.valid()].numPredict }

func (d Detail) String() string {
	switch d.valid() {
	case DetailShort:
		return "short"
	case DetailLong:
		return "long"
	case DetailMedium:
	}
	return "medium"
}

// valid maps an out-of-range value to the default, medium.
func (d Detail) valid() Detail {
	if d < DetailMedium || d > DetailLong {
		return DetailMedium
	}
	return d
}
