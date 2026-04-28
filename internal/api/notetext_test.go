package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// fixtureEntry mirrors the testdata/note_lengths.json schema. Both the Go and
// Swift sides load this file by relative path so the grapheme counts stay in
// sync across stacks.
type fixtureEntry struct {
	Name      string `json:"name"`
	Input     string `json:"input"`
	Graphemes int    `json:"graphemes"`
}

func loadFixture(t *testing.T) []fixtureEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "note_lengths.json"))
	require.NoError(t, err)
	var entries []fixtureEntry
	require.NoError(t, json.Unmarshal(data, &entries))
	require.NotEmpty(t, entries)
	return entries
}

func TestGraphemeCountFixture(t *testing.T) {
	for _, entry := range loadFixture(t) {
		t.Run(entry.Name, func(t *testing.T) {
			assert.Equal(t, entry.Graphemes, graphemeCount(entry.Input),
				"grapheme count must match fixture for %q", entry.Input)
		})
	}
}

func TestNormaliseTrimsLeadingAndTrailing(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"strips leading and trailing spaces": {
			input: "   hello   ",
			want:  "hello",
		},
		"strips leading and trailing newlines": {
			input: "\n\nhello\n\n",
			want:  "hello",
		},
		"strips mixed whitespace": {
			input: " \t\nhello\t\n ",
			want:  "hello",
		},
		"preserves internal whitespace": {
			input: "  hello  world  ",
			want:  "hello  world",
		},
		"preserves internal newlines": {
			input: "\nhello\nworld\n",
			want:  "hello\nworld",
		},
		"empty stays empty": {
			input: "",
			want:  "",
		},
		"whitespace-only collapses to empty": {
			input: " \t\n ",
			want:  "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalise(tc.input))
		})
	}
}

func TestNormaliseAppliesNFC(t *testing.T) {
	// "café" decomposed (NFD): "cafe" + U+0301 (combining acute).
	nfd := "café"
	// "café" composed (NFC): "caf" + U+00E9.
	nfc := "café"

	assert.Equal(t, nfc, normalise(nfd), "decomposed input must be composed")
	assert.Equal(t, nfc, normalise(nfc), "already-composed input is unchanged")
}

// TestPropertyNormaliseIdempotent verifies the contract documented in the
// design: normalise(normalise(s)) == normalise(s) for any input.
func TestPropertyNormaliseIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "input")
		once := normalise(s)
		twice := normalise(once)
		if once != twice {
			t.Fatalf("normalise not idempotent: once=%q twice=%q (input=%q)", once, twice, s)
		}
	})
}
