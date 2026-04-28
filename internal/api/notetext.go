package api

import (
	"strings"

	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

// normalise applies Unicode NFC normalisation and trims leading/trailing
// whitespace (including Unicode whitespace and newlines). Internal whitespace
// is preserved exactly. Identical to NoteText.normalised on the Swift side.
func normalise(text string) string {
	return strings.TrimSpace(norm.NFC.String(text))
}

// graphemeCount returns the number of user-perceived characters (grapheme
// clusters per UAX #29) in s after NFC + leading/trailing-trim normalisation.
// Mirrors NoteText.graphemeCount on the Swift side so the fixture stays in
// sync across stacks.
func graphemeCount(s string) int {
	return graphemeCountNormalised(normalise(s))
}

// graphemeCountNormalised counts grapheme clusters without re-normalising.
// Use when the caller has already invoked normalise on the same string.
func graphemeCountNormalised(s string) int {
	return uniseg.GraphemeClusterCount(s)
}
