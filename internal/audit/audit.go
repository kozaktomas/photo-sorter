// Package audit provides a thin wrapper around the audit_log database
// table used by HTTP handlers to record successful mutating actions.
//
// The package exposes a single Logger type. The Logger.Log method is the
// only call sites need: handlers pass an action name plus optional
// entity info, and the logger pulls user_uid, IP, and User-Agent off the
// request context. Failures to persist the row are NEVER returned —
// they are WARN-logged so the underlying request is not aborted by an
// audit-side outage.
//
// The audit middleware in internal/web/middleware injects the logger
// into the request context so handlers don't need to plumb it through
// every constructor.
package audit

import (
	"context"
	"log"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// Action constants — the symbolic action names recorded on every audit
// log row. Keep this list in sync with the spec (docs/specs/...) and
// the frontend i18n labels in web/src/i18n/locales/*/pages.json.
const (
	// Auth.
	ActionLogin          = "login"
	ActionLogout         = "logout"
	ActionLoginFailed    = "login_failed"
	ActionPasswordChange = "password_change"

	// Photo.
	ActionPhotoUpload      = "photo_upload"
	ActionPhotoUpdate      = "photo_update"
	ActionPhotoExifEdit    = "photo_exif_edit"
	ActionPhotoArchive     = "photo_archive"
	ActionPhotoRestore     = "photo_restore"
	ActionPhotoPurge       = "photo_purge"
	ActionPhotoBatchEdit   = "photo_batch_edit"
	ActionPhotoBatchLabel  = "photo_batch_label"
	ActionPhotoEditsUpdate = "photo_edits_update"
	ActionPhotoEditsClear  = "photo_edits_clear"

	// Album.
	ActionAlbumCreate       = "album_create"
	ActionAlbumUpdate       = "album_update"
	ActionAlbumDelete       = "album_delete"
	ActionAlbumPhotosAdd    = "album_photos_add"
	ActionAlbumPhotosRemove = "album_photos_remove"

	// Label.
	ActionLabelUpdate = "label_update"
	ActionLabelDelete = "label_delete"

	// Subject.
	ActionSubjectUpdate = "subject_update"

	// Face.
	ActionFaceApply           = "face_apply"
	ActionFaceOutlierUnassign = "face_outlier_unassign"
	ActionFaceCompute         = "face_compute"

	// User.
	ActionUserCreate        = "user_create"
	ActionUserUpdate        = "user_update"
	ActionUserDisable       = "user_disable"
	ActionUserEnable        = "user_enable"
	ActionUserDelete        = "user_delete"
	ActionUserPasswordReset = "user_password_reset"

	// Book.
	ActionBookCreate    = "book_create"
	ActionBookUpdate    = "book_update"
	ActionBookDelete    = "book_delete"
	ActionBookExportPDF = "book_export_pdf"
	// ActionBookExportCancel records a cancellation of an in-flight PDF
	// export job. The job lifecycle is mutating (it spawns lualatex and
	// holds temp files), so cancellation deserves a trail entry.
	ActionBookExportCancel = "book_export_cancel"

	// Book sub-resource mutations. Chapter / section / page / slot
	// operations are full DB writes; auditing them per-operation gives
	// the admin viewer the same coverage album mutations already enjoy.
	ActionBookChapterCreate   = "book_chapter_create"
	ActionBookChapterUpdate   = "book_chapter_update"
	ActionBookChapterDelete   = "book_chapter_delete"
	ActionBookChapterReorder  = "book_chapter_reorder"
	ActionBookSectionCreate   = "book_section_create"
	ActionBookSectionUpdate   = "book_section_update"
	ActionBookSectionDelete   = "book_section_delete"
	ActionBookSectionReorder  = "book_section_reorder"
	ActionBookSectionPhotoAdd = "book_section_photo_add"
	ActionBookSectionPhotoRem = "book_section_photo_remove"
	ActionBookSectionPhotoEd  = "book_section_photo_update"
	ActionBookPageCreate      = "book_page_create"
	ActionBookPageUpdate      = "book_page_update"
	ActionBookPageDelete      = "book_page_delete"
	ActionBookPageReorder     = "book_page_reorder"
	ActionBookSlotAssign      = "book_slot_assign"
	ActionBookSlotClear       = "book_slot_clear"
	ActionBookSlotCrop        = "book_slot_crop"
	ActionBookSlotSwap        = "book_slot_swap"
	ActionBookAutoLayout      = "book_auto_layout"

	// Share link.
	ActionShareLinkCreate         = "share_link_create"
	ActionShareLinkRevoke         = "share_link_revoke"
	ActionShareLinkPasswordVerify = "share_link_password_verify"
	ActionShareLinkPasswordFailed = "share_link_password_failed"

	// Smart album.
	ActionSmartAlbumCreate = "smart_album_create"
	ActionSmartAlbumUpdate = "smart_album_update"
	ActionSmartAlbumDelete = "smart_album_delete"

	// Process / sort jobs.
	ActionProcessJobStart   = "process_job_start"
	ActionProcessJobCancel  = "process_job_cancel"
	ActionProcessSyncCache  = "process_sync_cache"
	ActionProcessBuildThumb = "process_build_thumbs"
	ActionSortJobStart      = "sort_job_start"
	ActionSortJobCancel     = "sort_job_cancel"
	ActionUploadJobCancel   = "upload_job_cancel"

	// Text operations that mutate the database.
	ActionTextCheckSave     = "text_check_save"
	ActionTextVersionRestor = "text_version_restore"
)

// Entity type constants.
const (
	EntityPhoto      = "photo"
	EntityAlbum      = "album"
	EntityLabel      = "label"
	EntitySubject    = "subject"
	EntityUser       = "user"
	EntityBook       = "book"
	EntityShareLink  = "share_link"
	EntitySmartAlbum = "smart_album"
	EntitySession    = "session"
	EntityFaceMarker = "marker"
	EntityProcessJob = "process_job"
	EntitySortJob    = "sort_job"
	EntityBookExport = "book_export"
)

// Logger wraps the database AuditLogWriter and the optional request
// context helpers (auth info, IP, User-Agent) needed to populate every
// audit row. A nil Logger is a no-op: when the audit backend isn't
// registered yet, handlers can still call Log without nil-checks. The
// Logger type is otherwise stateless and safe to share across requests.
type Logger struct {
	writer database.AuditLogWriter
}

// NewLogger returns a Logger backed by the given writer. Pass nil to
// produce a no-op logger (useful in unit tests or during the boot phase
// when the writer hasn't been wired up yet).
func NewLogger(writer database.AuditLogWriter) *Logger {
	return &Logger{writer: writer}
}

// Log records a single audit event. The caller supplies the symbolic
// action, the affected entity (type+uid may both be empty), and
// optional metadata. The user_uid, IP, and User-Agent are pulled from
// the request context that was attached by RequestContext.
//
// Log NEVER returns an error: a failed insert is WARN-logged so the
// surrounding HTTP request continues to its normal response. The audit
// trail being slightly incomplete is preferable to mutations failing
// because the trail is down.
func (l *Logger) Log(
	ctx context.Context,
	action, entityType, entityUID string,
	metadata map[string]any,
) {
	if l == nil || l.writer == nil {
		return
	}
	rc := requestContextFromCtx(ctx)
	entry := &database.AuditLogEntry{
		UserUID:    rc.UserUID,
		Action:     action,
		EntityType: entityType,
		EntityUID:  entityUID,
		Metadata:   metadata,
		IP:         rc.IP,
		UserAgent:  rc.UserAgent,
	}
	if err := l.writer.AppendAuditLog(ctx, entry); err != nil {
		log.Printf("audit: append %q failed: %v", action, err)
	}
}

// LogAs records an audit event using an explicit user_uid rather than
// the one on the context. Used by the login handler, which logs a
// `login` action for a user that wasn't yet authenticated when the
// request entered the middleware stack.
func (l *Logger) LogAs(
	ctx context.Context,
	userUID, action, entityType, entityUID string,
	metadata map[string]any,
) {
	if l == nil || l.writer == nil {
		return
	}
	rc := requestContextFromCtx(ctx)
	entry := &database.AuditLogEntry{
		UserUID:    userUID,
		Action:     action,
		EntityType: entityType,
		EntityUID:  entityUID,
		Metadata:   metadata,
		IP:         rc.IP,
		UserAgent:  rc.UserAgent,
	}
	if err := l.writer.AppendAuditLog(ctx, entry); err != nil {
		log.Printf("audit: append %q failed: %v", action, err)
	}
}

// LogAnonymous records an audit event that has no authenticated user.
// Used for login_failed and share_link_password_failed where the caller
// is, by definition, not logged in. The supplied actorHint goes into
// metadata.actor so the admin viewer can show which username was being
// attacked.
func (l *Logger) LogAnonymous(
	ctx context.Context,
	action, entityType, entityUID, actorHint string,
	metadata map[string]any,
) {
	if l == nil || l.writer == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if actorHint != "" {
		metadata["actor"] = actorHint
	}
	rc := requestContextFromCtx(ctx)
	entry := &database.AuditLogEntry{
		// UserUID intentionally left empty — anonymous event.
		Action:     action,
		EntityType: entityType,
		EntityUID:  entityUID,
		Metadata:   metadata,
		IP:         rc.IP,
		UserAgent:  rc.UserAgent,
	}
	if err := l.writer.AppendAuditLog(ctx, entry); err != nil {
		log.Printf("audit: append %q failed: %v", action, err)
	}
}

// RequestContext is the per-request payload the audit middleware
// attaches: the user identity from the session, plus the client IP and
// User-Agent for forensic detail. It is shared by the Logger and the
// middleware to keep the context-key wiring in one place.
type RequestContext struct {
	UserUID   string
	IP        string
	UserAgent string
}

// requestContextKey is the unexported context key for the per-request
// audit context. Using a type prevents collisions with other packages.
type requestContextKey struct{}

// WithRequestContext attaches a RequestContext to ctx. The middleware
// calls this once per request; tests can also call it to build a
// context that mimics a logged-in caller.
func WithRequestContext(ctx context.Context, rc RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey{}, rc)
}

// requestContextFromCtx returns the RequestContext attached by
// WithRequestContext. When none is present (e.g. a CLI caller), the
// returned RequestContext is the zero value so the row is still
// written, just without IP/UA/user info.
func requestContextFromCtx(ctx context.Context) RequestContext {
	if rc, ok := ctx.Value(requestContextKey{}).(RequestContext); ok {
		return rc
	}
	return RequestContext{}
}

// loggerContextKey is the context key under which a *Logger is stashed
// by the middleware so handlers can retrieve it without holding a ref
// of their own.
type loggerContextKey struct{}

// WithLogger attaches l to ctx. The middleware injects the logger on
// every authenticated request; tests can also inject a Logger to
// exercise audit-side behaviour.
func WithLogger(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, l)
}

// FromContext returns the *Logger attached by WithLogger, or a no-op
// Logger when none is present. Handlers should always go through
// FromContext so callers that never registered a logger (CLI tools,
// unit tests) still compile and run without nil-check boilerplate.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(loggerContextKey{}).(*Logger); ok && l != nil {
		return l
	}
	return &Logger{}
}
