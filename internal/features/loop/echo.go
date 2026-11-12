package loop

import "strings"

// stripEcho removes the agent's echo of the given prompts from output. A
// prompt that the terminal soft-wrapped can leave a marker alone on a screen
// row, which would look like the agent printing it. The prompts and the
// output are compared as runs of non-space text, so wrapping, indentation and
// line breaks do not matter. Each removed echo is replaced by a line break,
// so text around it stays on its own lines.
func stripEcho(output string, prompts ...string) string {
	type span struct{ start, end int }
	var toks []span
	start := -1
	for i, r := range output {
		sp := r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
		switch {
		case !sp && start < 0:
			start = i
		case sp && start >= 0:
			toks = append(toks, span{start, i})
			start = -1
		}
	}
	if start >= 0 {
		toks = append(toks, span{start, len(output)})
	}
	remove := make([]bool, len(toks))
	seen := map[string]bool{}
	for _, p := range prompts {
		words := strings.Fields(p)
		if len(words) == 0 || seen[strings.Join(words, " ")] {
			continue
		}
		seen[strings.Join(words, " ")] = true
		for i := 0; i+len(words) <= len(toks); {
			match := true
			for j, w := range words {
				if output[toks[i+j].start:toks[i+j].end] != w {
					match = false
					break
				}
			}
			if !match {
				i++
				continue
			}
			for j := range words {
				remove[i+j] = true
			}
			i += len(words)
		}
	}
	var sb strings.Builder
	prev := 0
	for i, t := range toks {
		if !remove[i] {
			continue
		}
		sb.WriteString(output[prev:t.start])
		prev = t.end
		// Join the whole echoed run into one line break.
		if i+1 >= len(toks) || !remove[i+1] {
			sb.WriteString("\n")
		}
	}
	sb.WriteString(output[prev:])
	return sb.String()
}

// MarkerMatches reports whether output shows the loop's marker on a line of
// its own, ignoring the agent's echo of the loop's own prompts.
func (d Def) MarkerMatches(output string) bool {
	if d.Until.Marker == "" {
		return false
	}
	return d.Until.MatchMarker(stripEcho(output, d.Prompt, d.FollowupPrompt))
}
