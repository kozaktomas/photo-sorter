package verify

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppPhotoFull is the rich PhotoPrism projection used by the field-diff
// pass. Every column the migrator copies onto a native photo row is
// pulled in so the verifier can compare cell-by-cell. Keywords are
// loaded separately (the migrator unions details.keywords with
// photos_keywords) and merged in.
type ppPhotoFull struct {
	PhotoUID      string
	TakenAt       *time.Time
	TakenAtLocal  *time.Time
	TakenSrc      string
	TimeZone      string
	Caption       string
	Notes         string
	Lat           *float64
	Lng           *float64
	Altitude      *float64
	CameraMake    string
	CameraModel   string
	LensModel     string
	ISO           *int
	FNumber       *float64
	Exposure      string
	FocalLength   *float64
	FileWidth     int
	FileHeight    int
	Orientation   int
	Favorite      bool
	Private       bool
	Panorama      bool
	Scan          bool
	Quality       int16
	ExifArtist    string
	ExifCopyright string
	ExifLicense   string
	ExifSoftware  string
	Keywords      []string
}

// ensurePhotoMap populates v.photoMap + v.nativeHashByPhotoUID without
// running the structural disk walk. Used when Options.FieldsOnly is set:
// the field-diff phase still needs to know which native photo
// corresponds to each PhotoPrism photo, but the operator does not want
// to wait for a full SHA256 rehash. The shortcut is to read every
// primary file's recorded hash from PhotoPrism (file_hash, MD5) and the
// matching photo's file_hash on the native side. If both match, we have
// a mapping; if not we skip the row and let the field-diff phase
// implicitly treat it as a structural miss.
//
// The native side uses SHA256 (photo-sorter standard) while PhotoPrism
// stores SHA1/MD5/SHA256 depending on the indexer version. The
// migrator's recompute step is what guarantees equality; we cannot
// rebuild it here without a disk walk. Instead, we use the photo_uid
// preservation invariant from the migrator: native photos.uid is the
// PhotoPrism photo_uid verbatim. That is more brittle than a hash join
// (it breaks for legacy databases that pre-date UID preservation) but
// is fast and correct for current migrations.
func (v *verifier) ensurePhotoMap(ctx context.Context) error {
	pairs, err := v.readPPPhotoFilePairs(ctx)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		native, err := v.opts.Photos.GetPhoto(ctx, pair.photoUID)
		if err != nil || native == nil {
			continue
		}
		v.photoMap[pair.photoUID] = native.UID
		v.nativeHashByPhotoUID[native.UID] = native.FileHash
		// fileMap is normally populated by the structural pass via the
		// rehash walk; in fields-only mode we mirror PhotoPrism's
		// file_uid → native photo UID through the same UID-preservation
		// invariant so the marker field-diff still has a route from
		// markers.file_uid to native photos.uid.
		if pair.fileUID != "" {
			v.fileMap[pair.fileUID] = native.UID
		}
	}
	return nil
}

// ppPhotoFilePair pairs PhotoPrism's photo_uid with the primary file's
// file_uid so ensurePhotoMap can populate both photoMap and fileMap
// without running the rehash walk.
type ppPhotoFilePair struct {
	photoUID, fileUID string
}

// readPPPhotoFilePairs loads every (photo_uid, primary file_uid) pair
// PhotoPrism knows about. Primary file is the row with file_primary=1;
// if the photo has no primary file (PhotoPrism does this for some
// stack kinds), an empty fileUID is recorded.
func (v *verifier) readPPPhotoFilePairs(ctx context.Context) ([]ppPhotoFilePair, error) {
	const query = `
		SELECT p.photo_uid, COALESCE(f.file_uid, '')
		FROM photos p
		LEFT JOIN files f ON f.photo_uid = p.photo_uid
		                  AND COALESCE(f.file_primary, 0) = 1
		                  AND f.deleted_at IS NULL
		WHERE p.deleted_at IS NULL`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp photo uids: %w", err)
	}
	defer rows.Close()
	var out []ppPhotoFilePair
	for rows.Next() {
		var photoUID, fileUID string
		if err := rows.Scan(&photoUID, &fileUID); err != nil {
			return nil, fmt.Errorf("scan pp photo uid: %w", err)
		}
		out = append(out, ppPhotoFilePair{photoUID: photoUID, fileUID: fileUID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp photo uids: %w", err)
	}
	return out, nil
}

// diffPhotoFields compares every photo column the migrator is supposed
// to copy. Iterates PhotoPrism photos that have a matched native row
// (via v.photoMap) so structurally missing rows are skipped — they are
// already reported by the photos section.
func (v *verifier) diffPhotoFields(ctx context.Context) error {
	full, err := v.readPPPhotosFull(ctx)
	if err != nil {
		return fmt.Errorf("read pp photos full: %w", err)
	}
	keywords, err := v.readPPPhotoKeywords(ctx)
	if err != nil {
		return fmt.Errorf("read pp keywords: %w", err)
	}
	for i := range full {
		full[i].Keywords = mergeKeywords(full[i].Keywords, keywords[full[i].PhotoUID])
	}

	collector := newFieldCollector("photo", &v.report.Photos.FieldDiffs)
	for i := range full {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("photos field diff canceled: %w", err)
		}
		pp := &full[i]
		nativeUID, ok := v.photoMap[pp.PhotoUID]
		if !ok {
			// Structural miss; already in PhotoReport.MissingInSorter.
			continue
		}
		native, err := v.opts.Photos.GetPhoto(ctx, nativeUID)
		if err != nil || native == nil {
			continue
		}
		if v.nativeHashByPhotoUID[native.UID] == "" {
			v.nativeHashByPhotoUID[native.UID] = native.FileHash
		}
		key := photoKey(native.FileHash, native.UID)
		v.comparePhotoRow(collector, key, pp, native)
	}
	return nil
}

// photoKey returns a short identifier for the row used in diff entries.
// PhotoPrism hashes are 64 chars (SHA256) which is unhelpful in a
// terminal; the first 8 are enough to disambiguate in any human-sized
// library. Fall back to the UID when the hash is missing (the
// fields-only path may have empty hashes for legacy native rows).
func photoKey(hash, uid string) string {
	if len(hash) >= 8 {
		return hash[:8]
	}
	if hash != "" {
		return hash
	}
	return uid
}

// comparePhotoRow drives every per-field comparison for one matched
// (pp, native) pair. Each helper appends to the collector when a
// mismatch is detected.
func (v *verifier) comparePhotoRow(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	v.diffPhotoDates(c, key, pp, native)
	v.diffPhotoText(c, key, pp, native)
	v.diffPhotoGPS(c, key, pp, native)
	v.diffPhotoCamera(c, key, pp, native)
	v.diffPhotoExposure(c, key, pp, native)
	v.diffPhotoDimensions(c, key, pp, native)
	v.diffPhotoFlags(c, key, pp, native)
	v.diffPhotoExif(c, key, pp, native)
	v.diffPhotoKeywords(c, key, pp, native)
}

// diffPhotoDates compares taken_at (under tolerance), time_zone,
// taken_at_offset, and taken_at_source.
func (v *verifier) diffPhotoDates(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	srcTime := timePtrToLike(pp.TakenAt)
	dstTime := timePtrToLike(native.TakenAt)
	if !v.comparer.secondsEq(srcTime, dstTime) {
		c.Push(key, "taken_at", formatTimePtr(pp.TakenAt), formatTimePtr(native.TakenAt))
	}
	srcZone := normaliseString(pp.TimeZone)
	dstZone := normaliseString(native.TimeZone)
	if srcZone != dstZone {
		c.Push(key, "time_zone", srcZone, dstZone)
	}
	if srcOffset := computeOffsetSeconds(pp.TakenAt, pp.TakenAtLocal); srcOffset != native.TakenAtOffset {
		c.Push(key, "taken_at_offset", strconv.Itoa(srcOffset), strconv.Itoa(native.TakenAtOffset))
	}
	expected := expectedTakenSrc(pp.TakenAt)
	dstSrc := normaliseString(native.TakenAtSource)
	if !takenSrcEquivalent(expected, dstSrc, pp.TakenSrc) {
		c.Push(key, "taken_at_source", expected+"/"+pp.TakenSrc, dstSrc)
	}
}

// diffPhotoText compares description and notes. PhotoPrism's
// photo_caption maps to native.Description; details.notes maps to
// native.Notes.
func (v *verifier) diffPhotoText(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if a, b := normaliseString(pp.Caption), normaliseString(native.Description); a != b {
		c.Push(key, "description", a, b)
	}
	if a, b := normaliseString(pp.Notes), normaliseString(native.Notes); a != b {
		c.Push(key, "notes", a, b)
	}
}

// diffPhotoGPS compares lat/lng/altitude under their respective
// tolerance bands.
func (v *verifier) diffPhotoGPS(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if !v.comparer.floatPtrEq(pp.Lat, native.Lat, LatLngTolerance) {
		c.Push(key, "lat", formatFloatPtr(pp.Lat), formatFloatPtr(native.Lat))
	}
	if !v.comparer.floatPtrEq(pp.Lng, native.Lng, LatLngTolerance) {
		c.Push(key, "lng", formatFloatPtr(pp.Lng), formatFloatPtr(native.Lng))
	}
	if !v.comparer.floatPtrEq(pp.Altitude, native.Altitude, AltitudeTolerance) {
		c.Push(key, "altitude", formatFloatPtr(pp.Altitude), formatFloatPtr(native.Altitude))
	}
}

// diffPhotoCamera compares camera_make / camera_model / lens_model.
func (v *verifier) diffPhotoCamera(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if a, b := normaliseString(pp.CameraMake), normaliseString(native.CameraMake); a != b {
		c.Push(key, "camera_make", a, b)
	}
	if a, b := normaliseString(pp.CameraModel), normaliseString(native.CameraModel); a != b {
		c.Push(key, "camera_model", a, b)
	}
	if a, b := normaliseString(pp.LensModel), normaliseString(native.LensModel); a != b {
		c.Push(key, "lens_model", a, b)
	}
}

// diffPhotoExposure compares ISO / f_number / exposure / focal_length.
func (v *verifier) diffPhotoExposure(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if !intPtrEq(pp.ISO, native.ISO) {
		c.Push(key, "iso", formatIntPtr(pp.ISO), formatIntPtr(native.ISO))
	}
	if !v.comparer.floatPtrEq(pp.FNumber, native.Aperture, 0) {
		c.Push(key, "f_number", formatFloatPtr(pp.FNumber), formatFloatPtr(native.Aperture))
	}
	if a, b := normaliseString(pp.Exposure), normaliseString(native.Exposure); a != b {
		c.Push(key, "exposure", a, b)
	}
	if !v.comparer.floatPtrEq(pp.FocalLength, native.FocalLength, 0) {
		c.Push(key, "focal_length", formatFloatPtr(pp.FocalLength), formatFloatPtr(native.FocalLength))
	}
}

// diffPhotoDimensions compares width / height / orientation. Native
// orientation defaults to 1 when PhotoPrism's column is NULL/0, so we
// fold that on the source side too.
func (v *verifier) diffPhotoDimensions(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if pp.FileWidth != native.FileWidth {
		c.Push(key, "width", strconv.Itoa(pp.FileWidth), strconv.Itoa(native.FileWidth))
	}
	if pp.FileHeight != native.FileHeight {
		c.Push(key, "height", strconv.Itoa(pp.FileHeight), strconv.Itoa(native.FileHeight))
	}
	srcO := pp.Orientation
	if srcO <= 0 {
		srcO = 1
	}
	if srcO != native.FileOrientation {
		c.Push(key, "orientation", strconv.Itoa(srcO), strconv.Itoa(native.FileOrientation))
	}
}

// diffPhotoFlags compares favorite / private / panorama / scan / quality.
func (v *verifier) diffPhotoFlags(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	if pp.Favorite != native.Favorite {
		c.Push(key, "favorite", formatBool(pp.Favorite), formatBool(native.Favorite))
	}
	if pp.Private != native.Private {
		c.Push(key, "private", formatBool(pp.Private), formatBool(native.Private))
	}
	if pp.Panorama != native.Panorama {
		c.Push(key, "panorama", formatBool(pp.Panorama), formatBool(native.Panorama))
	}
	if pp.Scan != native.Scan {
		c.Push(key, "scan", formatBool(pp.Scan), formatBool(native.Scan))
	}
	if clampPPQualityVerifier(pp.Quality) != native.Quality {
		c.Push(key, "quality", strconv.Itoa(int(pp.Quality)), strconv.Itoa(int(native.Quality)))
	}
}

// diffPhotoExif compares the four EXIF text columns.
func (v *verifier) diffPhotoExif(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	pairs := []struct {
		field, src, dst string
	}{
		{"exif_artist", pp.ExifArtist, native.ExifArtist},
		{"exif_copyright", pp.ExifCopyright, native.ExifCopyright},
		{"exif_license", pp.ExifLicense, native.ExifLicense},
		{"exif_software", pp.ExifSoftware, native.ExifSoftware},
	}
	for _, p := range pairs {
		a, b := normaliseString(p.src), normaliseString(p.dst)
		if a != b {
			c.Push(key, p.field, a, b)
		}
	}
}

// diffPhotoKeywords compares the (sorted, NFC-normalised, deduped)
// keyword union. PhotoPrism details.keywords + photos_keywords on one
// side; native TEXT[] on the other.
func (v *verifier) diffPhotoKeywords(
	c *fieldDiffCollector, key string, pp *ppPhotoFull, native *database.Photo,
) {
	src := normaliseStringSlice(pp.Keywords)
	dst := normaliseStringSlice(native.Keywords)
	if !stringSliceEq(src, dst) {
		c.Push(key, "keywords", formatStringSlice(src), formatStringSlice(dst))
	}
}

// readPPPhotosFull loads the rich PhotoPrism projection. The join with
// `files` is restricted to file_primary so width/height/orientation
// come from the primary file (which matches how the migrator chooses
// them). The driver returns DATETIME columns as UTC time.Time.
func (v *verifier) readPPPhotosFull(ctx context.Context) ([]ppPhotoFull, error) {
	const query = `
		SELECT p.photo_uid,
		       p.taken_at, p.taken_at_local, COALESCE(p.taken_src, ''),
		       COALESCE(p.time_zone, ''),
		       COALESCE(p.photo_caption, ''),
		       COALESCE(d.notes, ''),
		       p.photo_lat, p.photo_lng, p.photo_altitude,
		       p.photo_iso, p.photo_f_number,
		       COALESCE(p.photo_exposure, ''),
		       p.photo_focal_length,
		       COALESCE(c.camera_make, ''), COALESCE(c.camera_model, ''),
		       COALESCE(l.lens_model, ''),
		       COALESCE(f.file_width, 0), COALESCE(f.file_height, 0),
		       COALESCE(f.file_orientation, 1),
		       COALESCE(p.photo_favorite, 0), COALESCE(p.photo_private, 0),
		       COALESCE(p.photo_panorama, 0), COALESCE(p.photo_scan, 0),
		       COALESCE(p.photo_quality, 0),
		       COALESCE(d.artist, ''), COALESCE(d.copyright, ''),
		       COALESCE(d.license, ''), COALESCE(d.software, ''),
		       COALESCE(d.keywords, '')
		FROM photos p
		LEFT JOIN cameras c ON c.id = p.camera_id
		LEFT JOIN lenses  l ON l.id = p.lens_id
		LEFT JOIN details d ON d.photo_id = p.id
		LEFT JOIN files   f ON f.photo_uid = p.photo_uid AND COALESCE(f.file_primary, 0) = 1
		                                                AND f.deleted_at IS NULL
		WHERE p.deleted_at IS NULL
		ORDER BY p.id`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query photos full: %w", err)
	}
	defer rows.Close()
	var out []ppPhotoFull
	for rows.Next() {
		p, err := scanPPPhotoFull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos full: %w", err)
	}
	return out, nil
}

// ppPhotoScanBuf bundles every scratch variable used by scanPPPhotoFull.
// Keeping it in one struct keeps the scan callsite short enough for the
// linter's function-length budget.
type ppPhotoScanBuf struct {
	takenAt, takenAtLocal             sql.NullTime
	altInt, iso, focalInt             sql.NullInt64
	fnum, lat, lng                    sql.NullFloat64
	fav, priv, pano, scan             int
	quality                           int16
	takenSrcRaw, tz                   []byte
	caption, notes                    []byte
	exposure, cameraMake, cameraModel []byte
	lensModel                         []byte
	artist, copyrightRaw              []byte
	license, software, keywords       []byte
}

// scanPPPhotoFull pulls one row of readPPPhotosFull into a ppPhotoFull.
// Nullable numeric columns become *T so we can distinguish zero from
// missing. The actual rows.Scan call and the buffer-to-struct copy
// live in dedicated helpers so this function stays inside the linter's
// length budget.
func scanPPPhotoFull(rows *sql.Rows) (ppPhotoFull, error) {
	var (
		p   ppPhotoFull
		buf ppPhotoScanBuf
	)
	if err := scanPPPhotoFullInto(rows, &p, &buf); err != nil {
		return ppPhotoFull{}, err
	}
	applyPPPhotoScanBuf(&p, &buf)
	return p, nil
}

// scanPPPhotoFullInto wraps the rows.Scan call for ppPhotoFull. Split
// off scanPPPhotoFull so the buffer struct can be referenced once and
// the call site stays narrow.
func scanPPPhotoFullInto(rows *sql.Rows, p *ppPhotoFull, b *ppPhotoScanBuf) error {
	if err := rows.Scan(
		&p.PhotoUID,
		&b.takenAt, &b.takenAtLocal, &b.takenSrcRaw,
		&b.tz,
		&b.caption,
		&b.notes,
		&b.lat, &b.lng, &b.altInt,
		&b.iso, &b.fnum,
		&b.exposure,
		&b.focalInt,
		&b.cameraMake, &b.cameraModel,
		&b.lensModel,
		&p.FileWidth, &p.FileHeight, &p.Orientation,
		&b.fav, &b.priv,
		&b.pano, &b.scan,
		&b.quality,
		&b.artist, &b.copyrightRaw,
		&b.license, &b.software,
		&b.keywords,
	); err != nil {
		return fmt.Errorf("scan ppPhotoFull: %w", err)
	}
	return nil
}

// applyPPPhotoScanBuf copies the typed scratch buffer onto the result
// struct. Keeps scanPPPhotoFull readable by removing the long
// assignment block.
func applyPPPhotoScanBuf(p *ppPhotoFull, b *ppPhotoScanBuf) {
	if b.takenAt.Valid && b.takenAt.Time.Year() > 1 {
		t := b.takenAt.Time
		p.TakenAt = &t
	}
	if b.takenAtLocal.Valid && b.takenAtLocal.Time.Year() > 1 {
		t := b.takenAtLocal.Time
		p.TakenAtLocal = &t
	}
	p.TakenSrc = string(b.takenSrcRaw)
	p.TimeZone = normalizeTimeZoneVerifier(string(b.tz))
	p.Caption = string(b.caption)
	p.Notes = string(b.notes)
	p.Lat = nullFloatPtr(b.lat)
	p.Lng = nullFloatPtr(b.lng)
	p.Altitude = nullIntFloatPtr(b.altInt)
	p.ISO = nullIntPtr(b.iso)
	p.FNumber = nullFloatPtr(b.fnum)
	p.Exposure = string(b.exposure)
	p.FocalLength = nullIntFloatPtr(b.focalInt)
	p.CameraMake = string(b.cameraMake)
	p.CameraModel = string(b.cameraModel)
	p.LensModel = string(b.lensModel)
	p.Favorite = b.fav != 0
	p.Private = b.priv != 0
	p.Panorama = b.pano != 0
	p.Scan = b.scan != 0
	p.Quality = b.quality
	p.ExifArtist = string(b.artist)
	p.ExifCopyright = string(b.copyrightRaw)
	p.ExifLicense = string(b.license)
	p.ExifSoftware = string(b.software)
	p.Keywords = parseDetailsKeywordsVerifier(string(b.keywords))
}

// readPPPhotoKeywords loads the photos_keywords join, returning a map
// photo_uid → []keyword. Mirrors internal/migrate's keyword union.
func (v *verifier) readPPPhotoKeywords(ctx context.Context) (map[string][]string, error) {
	const query = `
		SELECT p.photo_uid, k.keyword
		FROM photos_keywords pk
		JOIN photos p   ON p.id = pk.photo_id
		JOIN keywords k ON k.id = pk.keyword_id
		WHERE COALESCE(k.skip, 0) = 0
		  AND p.deleted_at IS NULL`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		// keywords table may not exist on minimal fixtures; treat as no data.
		return map[string][]string{}, nil
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var uid string
		var kwRaw []byte
		if err := rows.Scan(&uid, &kwRaw); err != nil {
			return nil, fmt.Errorf("scan photos_keywords: %w", err)
		}
		kw := string(kwRaw)
		if kw == "" {
			continue
		}
		out[uid] = append(out[uid], kw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos_keywords: %w", err)
	}
	return out, nil
}

// timePtrToLike converts a *time.Time into the verifier's timeLike
// shape so the comparer can compare instants without dragging time.Time
// through every helper.
func timePtrToLike(t *time.Time) *timeLike {
	if t == nil {
		return nil
	}
	return &timeLike{unixNano: t.UnixNano()}
}

// formatTimePtr renders a time pointer as RFC3339 or empty.
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatFloatPtr renders a *float64 in a stable short form.
func formatFloatPtr(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'g', -1, 64)
}

// formatIntPtr renders a *int as its decimal string or empty.
func formatIntPtr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// formatBool renders a bool as "true"/"false".
func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// intPtrEq reports equality between two *int pointers. nil == nil; one
// nil and one non-nil is unequal.
func intPtrEq(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// nullFloatPtr returns a *float64 set when v is non-NULL.
func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// nullIntPtr returns a *int set when v is non-NULL.
func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

// nullIntFloatPtr converts an integer column into *float64 (used for
// altitude / focal length which native stores as floats).
func nullIntFloatPtr(v sql.NullInt64) *float64 {
	if !v.Valid {
		return nil
	}
	f := float64(v.Int64)
	return &f
}

// normalizeTimeZoneVerifier is the verifier-local copy of the
// migrator's normalizeTimeZone. Keeping it here keeps the verify
// package independent of internal/migrate.
func normalizeTimeZoneVerifier(tz string) string {
	switch tz {
	case "", "Local":
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ""
	}
	return tz
}

// parseDetailsKeywordsVerifier splits PhotoPrism's comma-separated
// details.keywords column. Each token is trimmed and empty tokens are
// dropped; ordering is preserved so a stable diff is emitted when the
// two sides have the same tokens but in different orders (the
// normalisation in normaliseStringSlice handles that for comparison).
func parseDetailsKeywordsVerifier(s string) []string {
	return splitCommaTrim(s)
}

// splitCommaTrim returns a slice of trimmed, non-empty tokens from a
// comma-separated string.
func splitCommaTrim(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0)
	for raw := range strings.SplitSeq(s, ",") {
		token := strings.TrimSpace(raw)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

// mergeKeywords folds two keyword slices, deduplicating and preserving
// the order of the first slice. Mirrors internal/migrate's
// mergeKeywords without taking a package dependency.
func mergeKeywords(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	out := make([]string, 0, len(a)+len(b))
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, src := range [][]string{a, b} {
		for _, kw := range src {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			if _, dup := seen[kw]; dup {
				continue
			}
			seen[kw] = struct{}{}
			out = append(out, kw)
		}
	}
	return out
}

// clampPPQualityVerifier mirrors the migrator clamp so the destination
// row's stored value can be compared 1:1 against the source.
func clampPPQualityVerifier(q int16) int16 {
	switch {
	case q < 0:
		return 0
	case q > 7:
		return 7
	default:
		return q
	}
}

// computeOffsetSeconds returns the wall-clock delta between PhotoPrism
// taken_at_local and taken_at as a whole-second integer, the same
// derivation the migrator uses.
func computeOffsetSeconds(takenAt, takenAtLocal *time.Time) int {
	if takenAt == nil || takenAtLocal == nil {
		return 0
	}
	return int(takenAtLocal.Sub(*takenAt) / time.Second)
}

// takenSrcExif and takenSrcUnknown are the two values the migrator
// writes onto native.TakenAtSource.
const (
	takenSrcExif    = "exif"
	takenSrcUnknown = "unknown"
)

// expectedTakenSrc returns the value the migrator would write for the
// given PhotoPrism row: "exif" when the row carries a date, "unknown"
// otherwise. The native side accepts "" as a synonym for "unknown".
func expectedTakenSrc(takenAt *time.Time) string {
	if takenAt == nil {
		return takenSrcUnknown
	}
	return takenSrcExif
}

// takenSrcEquivalent decides whether the native taken_at_source agrees
// with the migrator's expected mapping. Per the spec the verifier
// should accept either side's mapping — so we treat the union
// {"exif", PhotoPrism's literal taken_src value} as compatible with
// native "exif", and {"unknown", ""} as compatible with each other.
func takenSrcEquivalent(expected, native, ppSrc string) bool {
	expected = normaliseString(expected)
	native = normaliseString(native)
	ppSrc = normaliseString(ppSrc)
	if expected == native {
		return true
	}
	if (expected == "" || expected == takenSrcUnknown) && (native == "" || native == takenSrcUnknown) {
		return true
	}
	if expected == takenSrcExif && (native == takenSrcExif || native == ppSrc) {
		return true
	}
	return false
}
