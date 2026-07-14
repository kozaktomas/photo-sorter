//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TestAuditLogRepository_ListFiltered is a regression test for a bug where
// every *filtered* audit-log query returned a 500.
//
// The predicates built by buildAuditFilterClauses are qualified as `al.<column>`,
// but the COUNT query selected `FROM audit_log` with no alias — so Postgres
// rejected it with `missing FROM-clause entry for table "al"`. The unfiltered
// path masked the bug completely: with no filter the WHERE clause is empty,
// so the alias is never referenced and the query happens to work.
//
// Every filter is exercised below, because each one goes through the same
// count query and any of them would have tripped it.
func TestAuditLogRepository_ListFiltered(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewAuditLogRepository(pool)

	entries := []database.AuditLogEntry{
		{Action: "api_token_create", EntityType: "api_token", EntityUID: "t1"},
		{Action: "api_token_revoke", EntityType: "api_token", EntityUID: "t1"},
		{Action: "album_create", EntityType: "album", EntityUID: "a1"},
	}
	for i := range entries {
		if err := repo.AppendAuditLog(ctx, &entries[i]); err != nil {
			t.Fatalf("AppendAuditLog: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter database.AuditLogFilter
		want   int
	}{
		{name: "unfiltered", filter: database.AuditLogFilter{}, want: 3},
		{
			name:   "by action",
			filter: database.AuditLogFilter{Action: "api_token_create"},
			want:   1,
		},
		{
			name:   "by entity type",
			filter: database.AuditLogFilter{EntityType: "api_token"},
			want:   2,
		},
		{
			name:   "by entity uid",
			filter: database.AuditLogFilter{EntityUID: "a1"},
			want:   1,
		},
		{
			name:   "no match",
			filter: database.AuditLogFilter{Action: "nonexistent"},
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filter
			f.Limit = 50
			got, total, err := repo.ListAuditLog(ctx, f)
			if err != nil {
				t.Fatalf("ListAuditLog(%+v) error = %v — a filtered count must not "+
					"blow up on the missing `al` alias", f, err)
			}
			if total != tt.want {
				t.Errorf("total = %d, want %d", total, tt.want)
			}
			if len(got) != tt.want {
				t.Errorf("len(entries) = %d, want %d", len(got), tt.want)
			}
		})
	}
}
