package api

import (
	"context"
	"log/slog"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// fetchNoteAsync starts a single-note GetNote in a goroutine and returns a
// wait closure that blocks until the read completes and yields *string (nil
// when absent or on error). The fetch runs alongside the caller's errgroup
// rather than inside it: a notes-table failure logs and leaves the field
// nil instead of cancelling the core read.
func fetchNoteAsync(ctx context.Context, reader dynamo.Reader, label, serial, date string) func() *string {
	done := make(chan *string, 1)
	go func() {
		item, err := reader.GetNote(ctx, serial, date)
		if err != nil {
			slog.Warn(label+" note fetch failed; continuing without note", "date", date, "error", err)
			done <- nil
			return
		}
		if item == nil {
			done <- nil
			return
		}
		text := item.Text
		done <- &text
	}()
	return func() *string { return <-done }
}

// fetchNotesAsync is the range equivalent of fetchNoteAsync: starts a
// QueryNotes in a goroutine and returns a wait closure yielding a date→text
// map. Failures log and yield an empty map.
func fetchNotesAsync(ctx context.Context, reader dynamo.Reader, label, serial, startDate, endDate string) func() map[string]string {
	done := make(chan map[string]string, 1)
	go func() {
		items, err := reader.QueryNotes(ctx, serial, startDate, endDate)
		if err != nil {
			slog.Warn(label+" notes query failed; continuing without notes", "error", err)
			done <- map[string]string{}
			return
		}
		out := make(map[string]string, len(items))
		for _, n := range items {
			out[n.Date] = n.Text
		}
		done <- out
	}()
	return func() map[string]string { return <-done }
}
