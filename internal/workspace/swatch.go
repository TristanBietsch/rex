package workspace

import (
	"crypto/rand"
	"math/big"
	"slices"
	"strings"
)

// swatches is the Okabe-Ito palette: eight hues that stay distinct under the
// common forms of color blindness, minus black.
var swatches = []string{"#0072B2", "#E69F00", "#009E73", "#CC79A7", "#56B4E9", "#D55E00", "#F0E442"}

// Swatches returns the colors a workspace can be given, in the order Init
// hands them out. The slice is a copy.
func Swatches() []string { return slices.Clone(swatches) }

// pickColor returns the first swatch not in used, or a random one when every
// swatch is taken.
func pickColor(used []string) string {
	for _, s := range swatches {
		if !slices.ContainsFunc(used, func(u string) bool { return strings.EqualFold(u, s) }) {
			return s
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(swatches))))
	if err != nil {
		return swatches[0]
	}
	return swatches[n.Int64()]
}
