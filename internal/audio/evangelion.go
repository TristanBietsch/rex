package audio

// evangelionCatalog is an anime mecha cockpit / NERV-terminal soundset —
// brighter digital chirps, FM-style overtone partials, sweeps and alert edges.
// Centered higher than the factorio set to read as "synthetic interface".
func evangelionCatalog() map[string][]burst {
	return map[string][]burst{
		// Magi boot scan: ascending arpeggio with shimmer overtone on the apex.
		EventStartup: {
			{tones: []tone{{523, 50, 30}}},
			{tones: []tone{{784, 50, 30}}},
			{tones: []tone{{1047, 90, 55}, {2093, 50, 25}}},
		},
		// Uplink chirp: tight perfect-fifth pair (data engaged).
		EventCreate: {
			{tones: []tone{{1568, 20, 14}, {3136, 12, 6}}},
			{tones: []tone{{2349, 26, 16}, {4699, 14, 7}}},
		},
		// Objective confirmed: rising fanfare with bright harmonic.
		EventDone: {
			{tones: []tone{{1318, 50, 35}, {2637, 30, 18}}},
			{tones: []tone{{1976, 90, 60}, {3951, 50, 25}}},
		},
		// Pattern blue / deletion alarm: pulsed mid descends to a low klaxon
		// bed — distinct alert character vs the factorio "thunk".
		EventDelete: {
			{tones: []tone{{1760, 18, 10}, {880, 18, 12}}},
			{tones: []tone{{880, 24, 14}}},
			{tones: []tone{{440, 60, 40}, {220, 80, 60}}},
			{tones: []tone{{147, 140, 110}, {220, 60, 45}}},
		},
		// HUD cursor tick: single sharp digital blip.
		EventNav: {
			{tones: []tone{{2349, 8, 5}}},
		},
		// Panel slides open: rising duo with sparkle.
		EventOpen: {
			{tones: []tone{{587, 26, 16}, {1175, 14, 8}}},
			{tones: []tone{{1175, 30, 18}, {2349, 16, 9}}},
		},
		// Panel slides closed: inverse — falling duo, sparkle leads.
		EventClose: {
			{tones: []tone{{1175, 26, 16}, {2349, 14, 8}}},
			{tones: []tone{{587, 30, 18}}},
		},
		// Command issued: rapid 4-step rising telegraph (status: directive).
		EventCommand: {
			{tones: []tone{{1568, 14, 9}}},
			{tones: []tone{{1865, 14, 9}}},
			{tones: []tone{{2093, 14, 9}}},
			{tones: []tone{{2349, 22, 14}, {4699, 12, 6}}},
		},
		// Filter engaged: two-step ping ratchet, harmonic on resolve.
		EventFilter: {
			{tones: []tone{{1318, 12, 7}}},
			{tones: []tone{{1976, 24, 15}, {2637, 14, 7}}},
		},
	}
}
