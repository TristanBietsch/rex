package settings

import "fmt"

// FeatureOn reports whether the optional feature id is switched on. id must
// be a Features-section setting (a bool, or an enum where "off" and "" mean
// off); anything else is a programming error and panics. A nil store reads
// the registry default.
//
// Off means inert: callers start no goroutine, subprocess or network call,
// write no file, add no argv, bind no key, and answer commands with a refusal.
func FeatureOn(s *Store, id string) bool {
	def, ok := Find(id)
	if !ok || def.Section != SectionFeatures {
		panic(fmt.Sprintf("settings: FeatureOn(%q): not a Features setting", id))
	}
	v := def.Default
	if s != nil {
		v = s.Get(id)
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "off"
	}
	return false
}
