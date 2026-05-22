package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeBrowseReader is an in-memory database.PhotoBrowseReader used to drive
// the Histogram + GeoPoints handler tests. It does not honour any of the
// filter fields beyond bbox + taken_from/taken_to — those are the ones the
// browse endpoints actually surface.
type fakeBrowseReader struct {
	hist      database.HistogramResult
	histErr   error
	points    []database.GeoPoint
	truncated bool
	pointsErr error

	lastFilter database.PhotoFilter
	lastBucket string
	lastCap    int
}

func (f *fakeBrowseReader) Histogram(
	_ context.Context, filter database.PhotoFilter, bucket string,
) (database.HistogramResult, error) {
	f.lastFilter = filter
	f.lastBucket = bucket
	if f.histErr != nil {
		return database.HistogramResult{}, f.histErr
	}
	return f.hist, nil
}

func (f *fakeBrowseReader) ListGeoPoints(
	_ context.Context, filter database.PhotoFilter, maxPoints int,
) ([]database.GeoPoint, bool, error) {
	f.lastFilter = filter
	f.lastCap = maxPoints
	if f.pointsErr != nil {
		return nil, false, f.pointsErr
	}
	out := make([]database.GeoPoint, len(f.points))
	copy(out, f.points)
	return out, f.truncated, nil
}

func createPhotosHandlerWithBrowse(cfg *config.Config, br database.PhotoBrowseReader) *PhotosHandler {
	return &PhotosHandler{
		config:       cfg,
		browseReader: br,
	}
}

func TestPhotosHandler_Histogram_Success(t *testing.T) {
	br := &fakeBrowseReader{
		hist: database.HistogramResult{
			Buckets: []database.HistogramBucket{
				{
					Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					End:   time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
					Count: 5,
				},
			},
			Total:       10,
			NoDateCount: 2,
			NoGPSCount:  3,
		},
	}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/histogram", nil)
	rec := httptest.NewRecorder()
	h.Histogram(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp HistogramResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Bucket != "month" {
		t.Errorf("Bucket = %q, want month", resp.Bucket)
	}
	if len(resp.Buckets) != 1 || resp.Buckets[0].Count != 5 {
		t.Errorf("Buckets = %+v", resp.Buckets)
	}
	if resp.Total != 10 || resp.NoDateCount != 2 || resp.NoGPSCount != 3 {
		t.Errorf("counts wrong: total=%d nodate=%d nogps=%d",
			resp.Total, resp.NoDateCount, resp.NoGPSCount)
	}
	if br.lastBucket != "month" {
		t.Errorf("lastBucket = %q, want month", br.lastBucket)
	}
}

func TestPhotosHandler_Histogram_BucketYear(t *testing.T) {
	br := &fakeBrowseReader{}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	req := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/photos/histogram?bucket=year", nil)
	rec := httptest.NewRecorder()
	h.Histogram(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if br.lastBucket != "year" {
		t.Errorf("lastBucket = %q, want year", br.lastBucket)
	}
}

func TestPhotosHandler_Histogram_InvalidBucket(t *testing.T) {
	br := &fakeBrowseReader{}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	req := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/photos/histogram?bucket=day", nil)
	rec := httptest.NewRecorder()
	h.Histogram(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
}

func TestPhotosHandler_Histogram_ForwardsBBox(t *testing.T) {
	br := &fakeBrowseReader{}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	url := "/api/v1/photos/histogram?min_lat=49&min_lng=13&max_lat=51&max_lng=15"
	req := httptest.NewRequestWithContext(context.Background(), "GET", url, nil)
	rec := httptest.NewRecorder()
	h.Histogram(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if br.lastFilter.BBox == nil {
		t.Fatal("BBox not forwarded")
	}
	if br.lastFilter.BBox.MinLat != 49 || br.lastFilter.BBox.MaxLng != 15 {
		t.Errorf("BBox = %+v", br.lastFilter.BBox)
	}
}

func TestPhotosHandler_GeoPoints_Success(t *testing.T) {
	br := &fakeBrowseReader{
		points: []database.GeoPoint{
			{PhotoUID: "p1", Lat: 49.27, Lng: 16.61},
			{PhotoUID: "p2", Lat: 50.08, Lng: 14.43},
		},
	}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/geo-points", nil)
	rec := httptest.NewRecorder()
	h.GeoPoints(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp GeoPointsResponse
	parseJSONResponse(t, rec, &resp)
	if len(resp.Points) != 2 {
		t.Errorf("Points length = %d, want 2", len(resp.Points))
	}
	if resp.Cap != maxGeoPoints {
		t.Errorf("Cap = %d, want %d", resp.Cap, maxGeoPoints)
	}
	if resp.Truncated {
		t.Errorf("Truncated should be false")
	}
	if br.lastCap != maxGeoPoints {
		t.Errorf("lastCap = %d, want %d", br.lastCap, maxGeoPoints)
	}
}

func TestPhotosHandler_GeoPoints_Truncated(t *testing.T) {
	br := &fakeBrowseReader{
		points:    []database.GeoPoint{{PhotoUID: "p1", Lat: 1, Lng: 2}},
		truncated: true,
	}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/geo-points", nil)
	rec := httptest.NewRecorder()
	h.GeoPoints(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp GeoPointsResponse
	parseJSONResponse(t, rec, &resp)
	if !resp.Truncated {
		t.Errorf("Truncated should be true")
	}
}

func TestPhotosHandler_GeoPoints_ForwardsTakenRange(t *testing.T) {
	br := &fakeBrowseReader{}
	h := createPhotosHandlerWithBrowse(testConfig(), br)

	url := "/api/v1/photos/geo-points?taken_from=2024-01-01T00:00:00Z&taken_to=2024-12-31T23:59:59Z"
	req := httptest.NewRequestWithContext(context.Background(), "GET", url, nil)
	rec := httptest.NewRecorder()
	h.GeoPoints(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if br.lastFilter.TakenFrom == nil || br.lastFilter.TakenTo == nil {
		t.Fatal("taken range not forwarded")
	}
	if br.lastFilter.TakenFrom.Year() != 2024 {
		t.Errorf("TakenFrom year = %d, want 2024", br.lastFilter.TakenFrom.Year())
	}
}

func TestPhotosHandler_Histogram_NoReader(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/histogram", nil)
	rec := httptest.NewRecorder()
	h.Histogram(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_GeoPoints_NoReader(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/geo-points", nil)
	rec := httptest.NewRecorder()
	h.GeoPoints(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}
