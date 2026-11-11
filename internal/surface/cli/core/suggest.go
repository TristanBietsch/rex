package core

// maxSuggestDistance is the largest edit distance at which a word is
// offered as "did you mean".
const maxSuggestDistance = 2

// Suggest returns the candidate closest to word, or "" when none is within
// two edits. Ties go to the earlier candidate.
func Suggest(word string, candidates []string) string {
	best, bestDist := "", maxSuggestDistance+1
	for _, c := range candidates {
		if d := editDistance(word, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b, in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
