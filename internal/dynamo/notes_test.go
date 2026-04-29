package dynamo

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryNotesAPI is a hand-rolled fake that satisfies both WriteAPI (used by
// DynamoNoteWriter) and the GetItem subset of ReadAPI (used by the test to
// verify round-trips). The single shared map keeps writes and reads consistent
// without needing a real DynamoDB client.
type inMemoryNotesAPI struct {
	items map[string]map[string]types.AttributeValue
}

func newInMemoryNotesAPI() *inMemoryNotesAPI {
	return &inMemoryNotesAPI{items: make(map[string]map[string]types.AttributeValue)}
}

func notesKey(serial, date string) string { return serial + "|" + date }

func (m *inMemoryNotesAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	serial := params.Item["sysSn"].(*types.AttributeValueMemberS).Value
	date := params.Item["date"].(*types.AttributeValueMemberS).Value
	m.items[notesKey(serial, date)] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *inMemoryNotesAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	serial := params.Key["sysSn"].(*types.AttributeValueMemberS).Value
	date := params.Key["date"].(*types.AttributeValueMemberS).Value
	delete(m.items, notesKey(serial, date))
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *inMemoryNotesAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	serial := params.Key["sysSn"].(*types.AttributeValueMemberS).Value
	date := params.Key["date"].(*types.AttributeValueMemberS).Value
	if av, ok := m.items[notesKey(serial, date)]; ok {
		return &dynamodb.GetItemOutput{Item: av}, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func notesTestTable() string { return "test-notes" }

// readNote is a tiny helper for the round-trip tests; it bypasses Reader
// (which gains GetNote in a later task) so the writer tests can stand alone.
func readNote(t *testing.T, api *inMemoryNotesAPI, serial, date string) *NoteItem {
	t.Helper()
	out, err := api.GetItem(context.Background(), &dynamodb.GetItemInput{
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
	})
	require.NoError(t, err)
	if out.Item == nil {
		return nil
	}
	var item NoteItem
	require.NoError(t, attributevalue.UnmarshalMap(out.Item, &item))
	return &item
}

func TestDynamoNoteWriter_PutNoteRoundTrip(t *testing.T) {
	api := newInMemoryNotesAPI()
	writer := NewDynamoNoteWriter(api, notesTestTable())

	want := NoteItem{
		SysSn:     "AB1234",
		Date:      "2026-04-15",
		Text:      "Away in Bali",
		UpdatedAt: "2026-04-15T01:23:45Z",
	}
	require.NoError(t, writer.PutNote(context.Background(), want))

	got := readNote(t, api, want.SysSn, want.Date)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestDynamoNoteWriter_PutNoteOverwriteIsLastWriteWins(t *testing.T) {
	api := newInMemoryNotesAPI()
	writer := NewDynamoNoteWriter(api, notesTestTable())

	first := NoteItem{SysSn: "AB1234", Date: "2026-04-15", Text: "first", UpdatedAt: "2026-04-15T01:00:00Z"}
	second := NoteItem{SysSn: "AB1234", Date: "2026-04-15", Text: "second", UpdatedAt: "2026-04-15T02:00:00Z"}
	require.NoError(t, writer.PutNote(context.Background(), first))
	require.NoError(t, writer.PutNote(context.Background(), second))

	got := readNote(t, api, "AB1234", "2026-04-15")
	require.NotNil(t, got)
	assert.Equal(t, second, *got, "second write should overwrite the first (req 1.5)")
}

func TestDynamoNoteWriter_DeleteNoteClearsExisting(t *testing.T) {
	api := newInMemoryNotesAPI()
	writer := NewDynamoNoteWriter(api, notesTestTable())

	item := NoteItem{SysSn: "AB1234", Date: "2026-04-15", Text: "removeme", UpdatedAt: "2026-04-15T01:00:00Z"}
	require.NoError(t, writer.PutNote(context.Background(), item))
	require.NotNil(t, readNote(t, api, "AB1234", "2026-04-15"))

	require.NoError(t, writer.DeleteNote(context.Background(), "AB1234", "2026-04-15"))
	assert.Nil(t, readNote(t, api, "AB1234", "2026-04-15"), "delete should clear the row")
}

func TestDynamoNoteWriter_DeleteNoteIdempotent(t *testing.T) {
	api := newInMemoryNotesAPI()
	writer := NewDynamoNoteWriter(api, notesTestTable())

	require.NoError(t, writer.DeleteNote(context.Background(), "AB1234", "2026-04-15"),
		"deleting a non-existent key must not error (req 1.4)")
}

func TestDynamoNoteWriter_PutNoteWrapsError(t *testing.T) {
	mock := &mockNotesWriteAPI{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	writer := NewDynamoNoteWriter(mock, notesTestTable())

	err := writer.PutNote(context.Background(), NoteItem{SysSn: "AB1234", Date: "2026-04-15"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "put note")
	assert.Contains(t, err.Error(), "test-notes")
}

func TestDynamoNoteWriter_DeleteNoteWrapsError(t *testing.T) {
	mock := &mockNotesWriteAPI{
		deleteItemFn: func(_ context.Context, _ *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			return nil, errors.New("conn reset")
		},
	}
	writer := NewDynamoNoteWriter(mock, notesTestTable())

	err := writer.DeleteNote(context.Background(), "AB1234", "2026-04-15")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete note")
	assert.Contains(t, err.Error(), "test-notes")
}

// mockNotesWriteAPI is a function-based double for the write-only error paths.
type mockNotesWriteAPI struct {
	putItemFn    func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	deleteItemFn func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
}

func (m *mockNotesWriteAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockNotesWriteAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}
