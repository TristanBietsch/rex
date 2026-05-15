package audio

import "math"

const (
	sampleRate = 44100
	channels   = 1
)

// tone is one sine burst with an exponential decay envelope.
type tone struct {
	freq    float64 // Hz
	durMs   int
	decayMs float64 // τ
}

// burst groups one or more concurrent tones (overlay).
type burst struct {
	tones []tone
}

// bakeSequence concatenates burst PCMs in time.
func bakeSequence(bursts ...burst) []byte {
	var out []byte
	for _, b := range bursts {
		out = append(out, renderBurst(b)...)
	}
	return out
}

// renderBurst renders one burst: the longest tone defines the duration; shorter
// overlay tones are mixed in starting at sample 0.
func renderBurst(b burst) []byte {
	if len(b.tones) == 0 {
		return nil
	}
	maxDur := 0
	for _, t := range b.tones {
		if t.durMs > maxDur {
			maxDur = t.durMs
		}
	}
	totalSamples := sampleRate * maxDur / 1000
	mixed := make([]float64, totalSamples)
	for _, t := range b.tones {
		nSamples := sampleRate * t.durMs / 1000
		tau := t.decayMs * float64(sampleRate) / 1000
		for i := 0; i < nSamples; i++ {
			env := math.Exp(-float64(i) / tau)
			sine := math.Sin(2 * math.Pi * t.freq * float64(i) / float64(sampleRate))
			mixed[i] += sine * env
		}
	}
	pcm := make([]byte, totalSamples*2)
	for i, v := range mixed {
		// Soft clip via tanh; -0.5 headroom so multi-tone overlays don't saturate.
		s := int16(math.Tanh(v) * 32767 * 0.5)
		pcm[i*2] = byte(s)
		pcm[i*2+1] = byte(s >> 8)
	}
	return pcm
}
