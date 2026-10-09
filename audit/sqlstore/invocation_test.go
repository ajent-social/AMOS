package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ajent-social/amos/audit"
	"github.com/google/uuid"
)

func TestInvocationWriterRejectsUntrustedAttributionBeforeSQL(t *testing.T) {
	id := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	for _, tc := range []struct {
		name   string
		writer *InvocationWriter
		ctx    context.Context
		tx     *sql.Tx
		want   error
	}{
		{"nil writer", nil, context.Background(), &sql.Tx{}, audit.ErrInvalidEvent},
		{"nil context", NewInvocationWriter(), nil, &sql.Tx{}, audit.ErrInvalidEvent},
		{"nil transaction", NewInvocationWriter(), context.Background(), nil, audit.ErrInvalidEvent},
		{"untrusted context", NewInvocationWriter(), context.Background(), &sql.Tx{}, audit.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.writer.AppendInvocationTx(tc.ctx, tc.tx, id, "continuity.case.change", audit.OutcomeDenied, id); !errors.Is(err, tc.want) {
				t.Fatalf("rejection=%v", err)
			}
		})
	}
}

func TestGenericWriterRejectsInvocationBeforeSQL(t *testing.T) {
	e := validEvent(t)
	e.Action = audit.ActionOperationInvoked
	e.ResourceType = audit.ResourceInvocation
	e.OperationID = "continuity.case.change"
	e.Attributes = nil
	s := &Store{db: &sql.DB{}}
	if _, err := s.AppendTx(context.Background(), &sql.Tx{}, e); !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("generic invocation write=%v", err)
	}
}
