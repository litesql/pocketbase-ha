package realtime

import (
	"testing"

	"github.com/litesql/go-ha"
)

func TestRecordIDFromChange(t *testing.T) {
	change := ha.Change{
		Columns:   []string{"id", "title"},
		PKColumns: []string{"id"},
		OldValues: []any{"old-id", "old title"},
		NewValues: []any{"new-id", "new title"},
	}

	id, ok := recordIDFromChange(change, false)
	if !ok {
		t.Fatal("expected new record id to be resolved")
	}
	if id != "new-id" {
		t.Fatalf("expected new-id, got %q", id)
	}

	id, ok = recordIDFromChange(change, true)
	if !ok {
		t.Fatal("expected old record id to be resolved")
	}
	if id != "old-id" {
		t.Fatalf("expected old-id, got %q", id)
	}
}

func TestModelFromChangeUsesOldPKForDelete(t *testing.T) {
	model := ModelFromChange(ha.Change{
		Columns:   []string{"id", "title"},
		PKColumns: []string{"id"},
		Operation: "DELETE",
		OldValues: []any{"deleted-id", "deleted title"},
	}, nil)

	if model == nil {
		t.Fatal("expected model")
	}
	if model.PK() != "deleted-id" {
		t.Fatalf("expected deleted-id, got %v", model.PK())
	}
}
