package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeSubjectRepo is an in-memory database.SubjectWriter used by handler
// tests. It mimics the bits of the real repository the face / subject
// handlers exercise — accent-insensitive name lookup, EnsureSubject
// upsert semantics, and computed PhotoCount/FaceCount derived from the
// companion fakeMarkerRepo (when wired). The Photo/Face counts default
// to zero when no marker repo is attached.
type fakeSubjectRepo struct {
	mu        sync.Mutex
	byUID     map[string]*database.Subject
	byNameKey map[string]string // lower(no-accent) name → uid

	markers *fakeMarkerRepo // optional; used for PhotoCount/FaceCount

	GetError       error
	ListError      error
	EnsureError    error
	UpdateError    error
	GetByNameError error
}

func newFakeSubjectRepo() *fakeSubjectRepo {
	return &fakeSubjectRepo{
		byUID:     map[string]*database.Subject{},
		byNameKey: map[string]string{},
	}
}

// seed inserts a subject row directly, returning the canonical copy.
func (f *fakeSubjectRepo) seed(uid, name string) *database.Subject {
	f.mu.Lock()
	defer f.mu.Unlock()
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	now := time.Now()
	s := &database.Subject{
		UID:       uid,
		Slug:      slug,
		Name:      name,
		Type:      "person",
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.byUID[uid] = s
	f.byNameKey[strings.ToLower(name)] = uid
	return s
}

func (f *fakeSubjectRepo) attachMarkers(m *fakeMarkerRepo) {
	f.markers = m
}

// snapshot returns a copy of the subject with PhotoCount/FaceCount filled
// from the attached marker repo (if any).
func (f *fakeSubjectRepo) snapshot(s *database.Subject) database.Subject {
	cp := *s
	if f.markers != nil {
		pc, fc := f.markers.countsForSubject(s.UID)
		cp.PhotoCount = pc
		cp.FaceCount = fc
	}
	return cp
}

// --- SubjectReader implementation. ---

func (f *fakeSubjectRepo) GetSubject(_ context.Context, uid string) (*database.Subject, error) {
	if f.GetError != nil {
		return nil, f.GetError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byUID[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := f.snapshot(s)
	return &cp, nil
}

func (f *fakeSubjectRepo) GetSubjectByName(_ context.Context, name string) (*database.Subject, error) {
	if f.GetByNameError != nil {
		return nil, f.GetByNameError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	uid, ok := f.byNameKey[strings.ToLower(name)]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := f.snapshot(f.byUID[uid])
	return &cp, nil
}

func (f *fakeSubjectRepo) ListSubjects(
	_ context.Context, q database.SubjectQuery,
) ([]database.Subject, error) {
	if f.ListError != nil {
		return nil, f.ListError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]database.Subject, 0, len(f.byUID))
	for _, s := range f.byUID {
		out = append(out, f.snapshot(s))
	}
	limit := q.Limit
	if limit <= 0 {
		limit = len(out)
	}
	start := min(q.Offset, len(out))
	end := min(start+limit, len(out))
	return out[start:end], nil
}

func (f *fakeSubjectRepo) ListSubjectsForPhoto(
	_ context.Context, photoUID string,
) ([]database.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markers == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []database.Subject
	for _, m := range f.markers.allMarkers() {
		if m.PhotoUID != photoUID || m.Invalid {
			continue
		}
		if m.SubjectUID == "" || seen[m.SubjectUID] {
			continue
		}
		seen[m.SubjectUID] = true
		if s, ok := f.byUID[m.SubjectUID]; ok {
			out = append(out, f.snapshot(s))
		}
	}
	return out, nil
}

// --- SubjectWriter implementation. ---

func (f *fakeSubjectRepo) EnsureSubject(
	ctx context.Context, name, subjectType string,
) (*database.Subject, error) {
	return f.EnsureSubjectWithUID(ctx, "", name, subjectType)
}

func (f *fakeSubjectRepo) EnsureSubjectWithUID(
	_ context.Context, preferredUID, name, subjectType string,
) (*database.Subject, error) {
	if f.EnsureError != nil {
		return nil, f.EnsureError
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ensure subject: name required")
	}
	if subjectType == "" {
		subjectType = "person"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.ToLower(name)
	if uid, ok := f.byNameKey[key]; ok {
		cp := f.snapshot(f.byUID[uid])
		return &cp, nil
	}
	uid := preferredUID
	if uid == "" {
		uid = "s-fake-" + strings.ReplaceAll(key, " ", "-")
	}
	slug := strings.ReplaceAll(key, " ", "-")
	now := time.Now()
	s := &database.Subject{
		UID:       uid,
		Slug:      slug,
		Name:      name,
		Type:      subjectType,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.byUID[uid] = s
	f.byNameKey[key] = uid
	cp := f.snapshot(s)
	return &cp, nil
}

func (f *fakeSubjectRepo) UpdateSubject(_ context.Context, s *database.Subject) error {
	if f.UpdateError != nil {
		return f.UpdateError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.byUID[s.UID]
	if !ok {
		return database.ErrNotFound
	}
	// Update the name index if the name changed.
	if !strings.EqualFold(existing.Name, s.Name) {
		delete(f.byNameKey, strings.ToLower(existing.Name))
		f.byNameKey[strings.ToLower(s.Name)] = s.UID
	}
	if s.Slug == "" {
		s.Slug = strings.ToLower(strings.ReplaceAll(s.Name, " ", "-"))
	}
	s.UpdatedAt = time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = existing.CreatedAt
	}
	cp := *s
	f.byUID[s.UID] = &cp
	return nil
}

func (f *fakeSubjectRepo) DeleteSubject(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byUID[uid]
	if !ok {
		return database.ErrNotFound
	}
	delete(f.byNameKey, strings.ToLower(s.Name))
	delete(f.byUID, uid)
	return nil
}

// fakeMarkerRepo is an in-memory database.MarkerWriter used by handler
// tests. CreateMarker assigns a deterministic UID when none is supplied so
// tests can assert against a known marker id.
type fakeMarkerRepo struct {
	mu      sync.Mutex
	byUID   map[string]*database.Marker
	nextSeq int

	CreateError   error
	UpdateError   error
	GetError      error
	ListError     error
	AssignError   error
	UnassignError error
}

func newFakeMarkerRepo() *fakeMarkerRepo {
	return &fakeMarkerRepo{byUID: map[string]*database.Marker{}}
}

// seed inserts a marker row directly with the supplied UID. Used to set up
// pre-existing markers for assign/unassign tests.
func (f *fakeMarkerRepo) seed(m database.Marker) *database.Marker {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.UID == "" {
		f.nextSeq++
		m.UID = "m-fake-" + intToStr(f.nextSeq)
	}
	if m.Type == "" {
		m.Type = "face"
	}
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	cp := m
	f.byUID[m.UID] = &cp
	return &cp
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// allMarkers returns a snapshot slice of every marker. Caller must NOT
// mutate the returned entries.
func (f *fakeMarkerRepo) allMarkers() []database.Marker {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]database.Marker, 0, len(f.byUID))
	for _, m := range f.byUID {
		out = append(out, *m)
	}
	return out
}

// countsForSubject returns (photoCount, faceCount) for the given subject,
// excluding markers flagged invalid. Both counts are 0 when the subject
// has no markers.
func (f *fakeMarkerRepo) countsForSubject(subjectUID string) (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	photos := map[string]bool{}
	faces := 0
	for _, m := range f.byUID {
		if m.SubjectUID != subjectUID || m.Invalid {
			continue
		}
		photos[m.PhotoUID] = true
		faces++
	}
	return len(photos), faces
}

// --- MarkerReader implementation. ---

func (f *fakeMarkerRepo) GetMarker(_ context.Context, uid string) (*database.Marker, error) {
	if f.GetError != nil {
		return nil, f.GetError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byUID[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMarkerRepo) ListMarkersForPhoto(
	_ context.Context, photoUID string,
) ([]database.Marker, error) {
	if f.ListError != nil {
		return nil, f.ListError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []database.Marker
	for _, m := range f.byUID {
		if m.PhotoUID == photoUID {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (f *fakeMarkerRepo) ListMarkersForSubject(
	_ context.Context, subjectUID string, limit, offset int,
) ([]database.Marker, error) {
	if f.ListError != nil {
		return nil, f.ListError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]database.Marker, 0)
	for _, m := range f.byUID {
		if m.SubjectUID == subjectUID {
			all = append(all, *m)
		}
	}
	if limit <= 0 {
		limit = len(all)
	}
	start := min(offset, len(all))
	end := min(start+limit, len(all))
	return all[start:end], nil
}

// --- MarkerWriter implementation. ---

func (f *fakeMarkerRepo) CreateMarker(_ context.Context, m *database.Marker) error {
	if f.CreateError != nil {
		return f.CreateError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.UID == "" {
		f.nextSeq++
		m.UID = "m-fake-" + intToStr(f.nextSeq)
	}
	if m.Type == "" {
		m.Type = "face"
	}
	now := time.Now()
	m.CreatedAt = now
	m.UpdatedAt = now
	cp := *m
	f.byUID[m.UID] = &cp
	return nil
}

func (f *fakeMarkerRepo) UpdateMarker(_ context.Context, m *database.Marker) error {
	if f.UpdateError != nil {
		return f.UpdateError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byUID[m.UID]; !ok {
		return database.ErrNotFound
	}
	m.UpdatedAt = time.Now()
	cp := *m
	f.byUID[m.UID] = &cp
	return nil
}

func (f *fakeMarkerRepo) DeleteMarker(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byUID[uid]; !ok {
		return database.ErrNotFound
	}
	delete(f.byUID, uid)
	return nil
}

func (f *fakeMarkerRepo) AssignSubject(_ context.Context, markerUID, subjectUID string) error {
	if f.AssignError != nil {
		return f.AssignError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byUID[markerUID]
	if !ok {
		return database.ErrNotFound
	}
	m.SubjectUID = subjectUID
	m.UpdatedAt = time.Now()
	return nil
}

func (f *fakeMarkerRepo) UnassignSubject(_ context.Context, markerUID string) error {
	if f.UnassignError != nil {
		return f.UnassignError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byUID[markerUID]
	if !ok {
		return database.ErrNotFound
	}
	m.SubjectUID = ""
	m.UpdatedAt = time.Now()
	return nil
}

func (f *fakeMarkerRepo) SetInvalid(_ context.Context, markerUID string, invalid bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byUID[markerUID]
	if !ok {
		return database.ErrNotFound
	}
	m.Invalid = invalid
	m.UpdatedAt = time.Now()
	return nil
}

// Note: fakePhotoReader is defined in photos_test.go (shared across handler
// tests). Face_photos_test.go uses it via newFakePhotoReader().
