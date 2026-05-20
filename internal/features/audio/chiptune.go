package audio

// chiptuneCatalog is an 8-bit handheld UI soundset — pentatonic intervals,
// root+octave pairs for a square-wave feel, playful but still synthetic.
func chiptuneCatalog() map[string][]burst {
	return map[string][]burst{
		// Power-on triad arpeggio (C major).
		EventStartup: {
			{tones: []tone{{523, 40, 28}}},
			{tones: []tone{{659, 40, 28}}},
			{tones: []tone{{784, 50, 35}, {1568, 28, 16}}},
		},
		// Coin pickup: rising perfect fourth.
		EventCreate: {
			{tones: []tone{{523, 16, 10}}},
			{tones: []tone{{698, 24, 16}, {1397, 14, 8}}},
		},
		// Stage clear fanfare.
		EventDone: {
			{tones: []tone{{784, 30, 20}, {1568, 18, 10}}},
			{tones: []tone{{988, 30, 20}}},
			{tones: []tone{{1175, 50, 35}, {2349, 30, 18}}},
		},
		// Game over: descending minor second + low womp.
		EventDelete: {
			{tones: []tone{{622, 18, 10}}},
			{tones: []tone{{587, 24, 14}}},
			{tones: []tone{{294, 70, 55}, {147, 50, 38}}},
		},
		EventNav: {
			{tones: []tone{{988, 8, 5}}},
			{tones: []tone{{988, 8, 5}}},
		},
		EventOpen: {
			{tones: []tone{{659, 20, 12}}},
			{tones: []tone{{784, 24, 14}, {1568, 12, 6}}},
		},
		EventClose: {
			{tones: []tone{{784, 20, 12}}},
			{tones: []tone{{659, 24, 14}}},
		},
		// Pause menu: root–fifth–octave.
		EventCommand: {
			{tones: []tone{{523, 14, 9}}},
			{tones: []tone{{784, 14, 9}}},
			{tones: []tone{{1047, 22, 14}, {2093, 12, 6}}},
		},
		EventFilter: {
			{tones: []tone{{659, 12, 7}}},
			{tones: []tone{{784, 20, 12}, {1568, 10, 5}}},
		},
		EventBootOK: {
			{tones: []tone{{1047, 18, 11}, {2093, 10, 5}}},
		},
		EventBootWarn: {
			{tones: []tone{{622, 28, 20}}},
		},
		EventBootFail: {
			{tones: []tone{{494, 24, 16}}},
			{tones: []tone{{392, 60, 45}, {196, 50, 38}}},
		},
	}
}
