package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// NoteItem represents a row in the flux-notes table.
type NoteItem struct {
	SysSn     string `dynamodbav:"sysSn"`
	Date      string `dynamodbav:"date"`
	Text      string `dynamodbav:"text"`
	UpdatedAt string `dynamodbav:"updatedAt"` // RFC 3339 UTC
}

// WriteAPI is the subset of the DynamoDB client used by DynamoNoteWriter.
// Kept separate from ReadAPI and DynamoAPI so existing read-side mocks don't
// grow unused methods, and so the Lambda's IAM policy gates the write path
// at compile time.
type WriteAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

// DynamoNoteWriter writes day-note items to DynamoDB. The single live
// *dynamodb.Client also satisfies ReadAPI and DynamoAPI at compile time.
type DynamoNoteWriter struct {
	client WriteAPI
	table  string
}

// NewDynamoNoteWriter returns a writer scoped to the given table name.
func NewDynamoNoteWriter(client WriteAPI, table string) *DynamoNoteWriter {
	return &DynamoNoteWriter{client: client, table: table}
}

// PutNote upserts a single note. Last-write-wins is intentional (decision 5);
// no conditional expression is applied.
func (w *DynamoNoteWriter) PutNote(ctx context.Context, item NoteItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal note (sysSn=%s, date=%s): %w", item.SysSn, item.Date, err)
	}
	_, err = w.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &w.table,
		Item:      av,
	})
	if err != nil {
		return fmt.Errorf("put note (table=%s, sysSn=%s, date=%s): %w", w.table, item.SysSn, item.Date, err)
	}
	return nil
}

// DeleteNote removes the note for (serial, date). DynamoDB DeleteItem is
// idempotent: deleting a key that does not exist still returns success.
func (w *DynamoNoteWriter) DeleteNote(ctx context.Context, serial, date string) error {
	_, err := w.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &w.table,
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
	})
	if err != nil {
		return fmt.Errorf("delete note (table=%s, sysSn=%s, date=%s): %w", w.table, serial, date, err)
	}
	return nil
}
