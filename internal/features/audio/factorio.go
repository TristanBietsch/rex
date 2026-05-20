package audio

// factorioCatalog is the original mechanical/industrial soundset — sharp attacks
// with exponential decay reading as clicks, chirps, and thunks.
func factorioCatalog() map[string][]burst {
	return map[string][]burst{
		EventStartup: {
			{tones: []tone{{220, 60, 30}}},
			{tones: []tone{{440, 60, 30}}},
			{tones: []tone{{880, 100, 60}, {1760, 40, 20}}},
		},
		EventCreate: {
			{tones: []tone{{220, 30, 20}}},
			{tones: []tone{{440, 40, 25}}},
			{tones: []tone{{660, 50, 30}}},
		},
		EventDone: {
			{tones: []tone{{880, 80, 70}, {1760, 40, 20}}},
		},
		// Descending "zip-then-thunk": a tight high tick collapses through a
		// midrange chirp into a low resonant thump, reading as "swept away".
		EventDelete: {
			{tones: []tone{{1320, 14, 8}}},
			{tones: []tone{{660, 22, 14}, {1320, 12, 6}}},
			{tones: []tone{{220, 60, 40}}},
			{tones: []tone{{110, 90, 70}, {165, 50, 35}}},
		},
		EventNav: {
			{tones: []tone{{1200, 10, 6}}},
		},
		EventOpen: {
			{tones: []tone{{330, 30, 18}}},
			{tones: []tone{{660, 30, 18}}},
		},
		EventClose: {
			{tones: []tone{{660, 30, 18}}},
			{tones: []tone{{330, 30, 18}}},
		},
		// Command (":") — crisp two-tone "menu opens" chime with a brief
		// overtone on the resolve so it reads as deliberate.
		EventCommand: {
			{tones: []tone{{1568, 16, 10}}},
			{tones: []tone{{1175, 32, 22}, {2349, 18, 10}}},
		},
		// Filter ("t") — upward ratchet "chip-click": tight low tick resolves
		// into a higher chirp with a sparkle overtone.
		EventFilter: {
			{tones: []tone{{988, 10, 6}}},
			{tones: []tone{{1480, 22, 14}, {2960, 10, 5}}},
		},
		// Boot OK: a single quick mechanical click (think "lever locking into place").
		EventBootOK: {
			{tones: []tone{{1200, 18, 10}}},
		},
		// Boot WARN: damped lower thunk — softer, slightly off-pitch.
		EventBootWarn: {
			{tones: []tone{{660, 32, 22}}},
		},
		// Boot FAIL: dropped-belt clatter — two-step descending pair.
		EventBootFail: {
			{tones: []tone{{440, 30, 18}}},
			{tones: []tone{{220, 80, 60}, {110, 60, 40}}},
		},
	}
}
