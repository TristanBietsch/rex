package audio

// bellCatalog is a Unix-terminal minimal soundset — one short sine blip per
// action, sparse decay, no arpeggios. For users who want feedback without
// personality.
func bellCatalog() map[string][]burst {
	return map[string][]burst{
		EventStartup: {
			{tones: []tone{{880, 24, 18}}},
		},
		EventCreate: {
			{tones: []tone{{988, 8, 5}}},
		},
		EventDone: {
			{tones: []tone{{784, 40, 32}}},
		},
		EventDelete: {
			{tones: []tone{{440, 50, 40}}},
		},
		EventNav: {
			{tones: []tone{{1175, 6, 4}}},
		},
		EventOpen: {
			{tones: []tone{{880, 14, 10}}},
		},
		EventClose: {
			{tones: []tone{{880, 10, 7}}},
		},
		EventCommand: {
			{tones: []tone{{880, 8, 5}}},
			{tones: []tone{{880, 8, 5}}},
		},
		EventFilter: {
			{tones: []tone{{988, 10, 6}}},
		},
		EventBootOK: {
			{tones: []tone{{988, 10, 6}}},
		},
		EventBootWarn: {
			{tones: []tone{{660, 22, 16}}},
		},
		EventBootFail: {
			{tones: []tone{{440, 28, 20}}},
			{tones: []tone{{330, 40, 30}}},
		},
	}
}
