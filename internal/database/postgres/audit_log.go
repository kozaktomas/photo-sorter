package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// AuditLogRepository provides PostgreSQL-backed storage for the audit
// trail. Writes are append-only; reads support filtering by user, action,
// entity, and time range with offset-based pagination.
type AuditLogRepository struct {
	pool *Pool
}

// NewAuditLogRepository returns an AuditLogRepository bound to the given
// pool.
func NewAuditLogRepository(pool *Pool) *AuditLogRepository {
	return &AuditLogRepository{pool: pool}
}

// AppendAuditLog inserts a single audit log row. The id and created_at
// columns are populated by the database; the caller's entry is updated
// in place so handlers can return the freshly persisted row if they
// wish.
func (r *AuditLogRepository) AppendAuditLog(
	ctx context.Context, entry *database.AuditLogEntry,
) error {
	metadataJSON, err := marshalAuditMetadata(entry.Metadata)
	if err != nil {
		return err
	}
	// user_uid, entity_type, entity_uid, ip, user_agent: empty string is
	// stored as NULL so the column matches its declared NULL semantics
	// and the FK on user_uid does not reject blank strings.
	var userUID, entityType, entityUID, ip, userAgent sql.NullString
	if entry.UserUID != "" {
		userUID = sql.NullString{String: entry.UserUID, Valid: true}
	}
	if entry.EntityType != "" {
		entityType = sql.NullString{String: entry.EntityType, Valid: true}
	}
	if entry.EntityUID != "" {
		entityUID = sql.NullString{String: entry.EntityUID, Valid: true}
	}
	if entry.IP != "" {
		ip = sql.NullString{String: entry.IP, Valid: true}
	}
	if entry.UserAgent != "" {
		userAgent = sql.NullString{String: entry.UserAgent, Valid: true}
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO audit_log
		    (user_uid, action, entity_type, entity_uid, metadata, ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
		 RETURNING id, created_at`,
		userUID, entry.Action, entityType, entityUID, metadataJSON, ip, userAgent,
	)
	if err := row.Scan(&entry.ID, &entry.CreatedAt); err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

// ListAuditLog returns audit log entries matching the filter. The first
// return is the entry page (limited + offset), the second is the total
// number of rows matching the filter (independent of limit/offset).
// Joins users for the cached display username; deleted users surface as
// an empty Username so the caller can render "<deleted user>" without
// extra round trips.
func (r *AuditLogRepository) ListAuditLog(
	ctx context.Context, filter database.AuditLogFilter,
) ([]database.AuditLogEntry, int, error) {
	where, args := buildAuditFilterClauses(filter)

	// Count first so the UI can render pagination. A bare COUNT(*) with
	// the same predicates is fine — the (created_at DESC) index covers
	// the unfiltered path and the action / user_uid indexes cover the
	// typical filtered paths.
	//
	// The `al` alias is required, not cosmetic: buildAuditFilterClauses qualifies
	// every predicate as `al.<column>`, so without it any filtered query
	// dies with `missing FROM-clause entry for table "al"`. The unfiltered
	// path masked this — `where` is empty there, so the alias is never
	// referenced.
	var total int
	countQuery := `SELECT COUNT(*) FROM audit_log al` + where
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit log: %w", err)
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	listQuery := `SELECT al.id, al.user_uid, COALESCE(u.username, ''),
	         al.action, COALESCE(al.entity_type, ''), COALESCE(al.entity_uid, ''),
	         al.metadata, COALESCE(al.ip, ''), COALESCE(al.user_agent, ''),
	         al.created_at
	  FROM audit_log al
	  LEFT JOIN users u ON u.uid = al.user_uid` + where +
		fmt.Sprintf(` ORDER BY al.created_at DESC, al.id DESC LIMIT $%d OFFSET $%d`,
			len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit log: %w", err)
	}
	defer rows.Close()

	var entries []database.AuditLogEntry
	for rows.Next() {
		entry, err := scanAuditLogEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit log: %w", err)
	}
	return entries, total, nil
}

// buildAuditFilterClauses turns an AuditLogFilter into a WHERE clause
// (prefixed with " WHERE " when any predicate applies, empty otherwise)
// and the matching positional args. Predicate ordering doesn't matter
// because of how PostgreSQL's planner uses the partial indexes.
func buildAuditFilterClauses(f database.AuditLogFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.UserUID != "" {
		clauses = append(clauses, "al.user_uid = "+addArg(f.UserUID))
	}
	if f.Action != "" {
		clauses = append(clauses, "al.action = "+addArg(f.Action))
	}
	if f.EntityType != "" {
		clauses = append(clauses, "al.entity_type = "+addArg(f.EntityType))
	}
	if f.EntityUID != "" {
		clauses = append(clauses, "al.entity_uid = "+addArg(f.EntityUID))
	}
	if f.Since != nil {
		clauses = append(clauses, "al.created_at >= "+addArg(*f.Since))
	}
	if f.Until != nil {
		clauses = append(clauses, "al.created_at <= "+addArg(*f.Until))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// marshalAuditMetadata serialises the metadata map for the JSONB column.
// A nil map is stored as `{}` so SELECTs always return a non-NULL JSON
// object the API can hand to the frontend unchanged.
func marshalAuditMetadata(meta map[string]any) ([]byte, error) {
	if meta == nil {
		meta = map[string]any{}
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal audit metadata: %w", err)
	}
	return b, nil
}

// scanAuditLogEntry reads one row from the join query in ListAuditLog.
// The user_uid column is nullable on the DB side but COALESCEd to an
// empty string here so the caller never has to deal with sql.NullString.
func scanAuditLogEntry(s rowScanner) (*database.AuditLogEntry, error) {
	var (
		entry        database.AuditLogEntry
		userUID      sql.NullString
		metadataJSON []byte
	)
	if err := s.Scan(
		&entry.ID, &userUID, &entry.Username,
		&entry.Action, &entry.EntityType, &entry.EntityUID,
		&metadataJSON, &entry.IP, &entry.UserAgent, &entry.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan audit log row: %w", err)
	}
	if userUID.Valid {
		entry.UserUID = userUID.String
	}
	entry.Metadata = map[string]any{}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &entry.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal audit metadata: %w", err)
		}
	}
	return &entry, nil
}

// Verify interface compliance.
var (
	_ database.AuditLogReader = (*AuditLogRepository)(nil)
	_ database.AuditLogWriter = (*AuditLogRepository)(nil)
)
