package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// auditLogDefaultLimit is the default page size returned when the
// client omits the limit query param.
const auditLogDefaultLimit = 50

// auditLogMaxLimit caps the page size to protect the admin UI from a
// huge response and the database from a runaway scan. The frontend
// hard-codes the same value in its limit selector.
const auditLogMaxLimit = 200

// AuditLogHandler serves the admin-only GET /api/v1/audit-log endpoint.
// It owns an AuditLogReader; reads are paginated and filtered by the
// query params documented in docs/specs/task-7658cf63.... Logger writes
// happen elsewhere (each mutation handler calls
// audit.FromContext(ctx).Log).
type AuditLogHandler struct {
	repo database.AuditLogReader
}

// NewAuditLogHandler returns an AuditLogHandler bound to repo. Passing
// nil is allowed: the handler then surfaces a 503 on every call, the
// same pattern other repository-backed handlers use during the
// PhotoPrism-to-native transition.
func NewAuditLogHandler(repo database.AuditLogReader) *AuditLogHandler {
	return &AuditLogHandler{repo: repo}
}

// AuditLogEntryResponse is the wire shape for one row. CreatedAt is
// formatted as RFC3339 so the frontend can pass it straight to Date().
// UserUsername is "" when the actor was anonymous or has since been
// deleted; the frontend renders that as "<deleted user>" / "—".
type AuditLogEntryResponse struct {
	ID           int64          `json:"id"`
	UserUID      string         `json:"user_uid"`
	UserUsername string         `json:"user_username"`
	Action       string         `json:"action"`
	EntityType   string         `json:"entity_type"`
	EntityUID    string         `json:"entity_uid"`
	Metadata     map[string]any `json:"metadata"`
	IP           string         `json:"ip"`
	UserAgent    string         `json:"user_agent"`
	CreatedAt    string         `json:"created_at"`
}

// AuditLogListResponse is the envelope returned by GET /audit-log.
type AuditLogListResponse struct {
	Entries []AuditLogEntryResponse `json:"entries"`
	Total   int                     `json:"total"`
	Limit   int                     `json:"limit"`
	Offset  int                     `json:"offset"`
}

// List handles GET /api/v1/audit-log. Admin-only — the
// RequireRole(RoleAdmin) middleware on the route gates the call before
// we get here. Filter params are documented in the spec.
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		respondError(w, http.StatusServiceUnavailable, "audit log unavailable")
		return
	}

	filter, errResp := parseAuditLogQuery(r)
	if errResp != "" {
		respondError(w, http.StatusBadRequest, errResp)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	entries, total, err := h.repo.ListAuditLog(ctx, filter)
	if err != nil {
		log.Printf("audit-log: list failed: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list audit log")
		return
	}

	out := AuditLogListResponse{
		Entries: make([]AuditLogEntryResponse, 0, len(entries)),
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, AuditLogEntryResponse{
			ID:           e.ID,
			UserUID:      e.UserUID,
			UserUsername: e.Username,
			Action:       e.Action,
			EntityType:   e.EntityType,
			EntityUID:    e.EntityUID,
			Metadata:     e.Metadata,
			IP:           e.IP,
			UserAgent:    e.UserAgent,
			CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// parseAuditLogQuery extracts an AuditLogFilter from the request query
// string and validates each field. Returns a human-readable error
// string when validation fails; the caller wraps it in a 400.
func parseAuditLogQuery(r *http.Request) (database.AuditLogFilter, string) {
	q := r.URL.Query()
	filter := database.AuditLogFilter{
		UserUID:    strings.TrimSpace(q.Get("user_uid")),
		Action:     strings.TrimSpace(q.Get("action")),
		EntityType: strings.TrimSpace(q.Get("entity_type")),
		EntityUID:  strings.TrimSpace(q.Get("entity_uid")),
		Limit:      auditLogDefaultLimit,
	}

	since, errSince := parseOptionalTime(q.Get("since"))
	if errSince != "" {
		return filter, "invalid since (expected RFC3339)"
	}
	filter.Since = since

	until, errUntil := parseOptionalTime(q.Get("until"))
	if errUntil != "" {
		return filter, "invalid until (expected RFC3339)"
	}
	filter.Until = until

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := parseClampedInt(raw, 1, auditLogMaxLimit)
		if err != nil {
			return filter, "invalid limit"
		}
		filter.Limit = n
	}

	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := parseClampedInt(raw, 0, 0)
		if err != nil {
			return filter, "invalid offset"
		}
		filter.Offset = n
	}

	return filter, ""
}

// parseOptionalTime parses an optional RFC3339 timestamp. An empty input
// returns (nil, "") so the caller can leave the filter unconstrained;
// a malformed input returns (nil, error message).
func parseOptionalTime(raw string) (*time.Time, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ""
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, "invalid"
	}
	return &t, ""
}

// parseClampedInt parses a positive integer with an optional upper
// clamp. The caller supplies min (any value below it is rejected) and
// max (0 disables the clamp). Returns the parsed (and clamped) value.
func parseClampedInt(raw string, minVal, maxVal int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < minVal {
		return 0, errors.New("invalid int")
	}
	if maxVal > 0 && n > maxVal {
		n = maxVal
	}
	return n, nil
}
