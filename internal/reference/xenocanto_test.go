package reference

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// fakeDoer is an injectable HTTP doer returning a canned response.
type fakeDoer struct {
	calls   int
	lastReq *http.Request
	status  int
	body    string
	err     error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

const sampleResponse = `{
  "recordings": [
    {
      "id": "111", "gen": "Turdus", "sp": "migratorius", "ssp": "migratorius",
      "en": "American Robin", "rec": "Jane Smith", "cnt": "United States",
      "loc": "Central Park", "type": "song", "url": "//xeno-canto.org/111",
      "file": "//xeno-canto.org/111/download", "file-name": "XC111.mp3",
      "lic": "//creativecommons.org/licenses/by-nc-sa/4.0/", "q": "A",
      "length": "0:12", "also": ["Passer domesticus"], "date": "2025-07-01"
    }
  ]
}`

func newTestClient(t *testing.T, doer Doer, opts ...Option) *Client {
	t.Helper()
	base := make([]Option, 0, 1+len(opts))
	base = append(base, WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))
	base = append(base, opts...)
	return NewClient("test-key", doer, base...)
}

func TestSearch_ParsesAndMapsResponse(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	c := newTestClient(t, doer)

	recordings, err := c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	require.Len(t, recordings, 1)

	r := recordings[0]
	assert.Equal(t, "111", r.ID)
	assert.Equal(t, "Turdus migratorius", r.ScientificName)
	assert.Equal(t, "migratorius", r.Subspecies)
	assert.Equal(t, "American Robin", r.CommonName)
	assert.Equal(t, "Jane Smith", r.Recordist)
	assert.Equal(t, "United States", r.Country)
	assert.Equal(t, "song", r.CallType)
	assert.Equal(t, "https://xeno-canto.org/111", r.PageURL)
	assert.Equal(t, "https://xeno-canto.org/111/download", r.AudioURL)
	assert.Equal(t, "CC BY-NC-SA 4.0", r.LicenseName)
	assert.Equal(t, "https://creativecommons.org/licenses/by-nc-sa/4.0/", r.LicenseURL)
	assert.Equal(t, "A", r.Quality)
	assert.Equal(t, []string{"Passer domesticus"}, r.Background)
	assert.Equal(t, ProviderXenoCanto, r.SourceProvider)
}

func TestSearch_SendsQueryAndKey(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	c := newTestClient(t, doer)

	_, err := c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	require.NotNil(t, doer.lastReq)

	q := doer.lastReq.URL.Query()
	assert.Equal(t, `gen:"Turdus" sp:"migratorius"`, q.Get("query"))
	assert.Equal(t, "test-key", q.Get("key"))
	assert.Equal(t, "application/json", doer.lastReq.Header.Get("Accept"))
	assert.NotEmpty(t, doer.lastReq.Header.Get("User-Agent"))
}

func TestSearch_CachesPerSpecies(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	c := newTestClient(t, doer)

	_, err := c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	_, err = c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)

	assert.Equal(t, 1, doer.calls, "second identical search must be served from cache")
}

func TestSearch_CacheExpires(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	now := time.Unix(1_000_000, 0)
	c := newTestClient(t, doer,
		WithClock(func() time.Time { return now }),
		WithCacheTTL(time.Minute),
	)

	_, err := c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	assert.Equal(t, 1, doer.calls)

	now = now.Add(30 * time.Second) // still fresh
	_, err = c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	assert.Equal(t, 1, doer.calls)

	now = now.Add(2 * time.Minute) // expired
	_, err = c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	assert.Equal(t, 2, doer.calls)
}

func TestSearch_NotConfiguredWithoutKey(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	c := NewClient("", doer, WithRateLimiter(rate.NewLimiter(rate.Inf, 1)))

	assert.False(t, c.Configured())
	_, err := c.Search(t.Context(), "Turdus migratorius")
	require.ErrorIs(t, err, ErrProviderNotConfigured)
	assert.Equal(t, 0, doer.calls, "must not hit the network when unconfigured")
}

func TestSearch_EmptyNameIsValidationError(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: sampleResponse}
	c := newTestClient(t, doer)

	_, err := c.Search(t.Context(), "   ")
	require.Error(t, err)
	assert.Equal(t, 0, doer.calls)
}

func TestSearch_Non200IsError(t *testing.T) {
	doer := &fakeDoer{status: http.StatusInternalServerError, body: "boom"}
	c := newTestClient(t, doer)

	_, err := c.Search(t.Context(), "Turdus migratorius")
	require.Error(t, err)
}

func TestSearch_EmptyRecordingsReturnsEmptySlice(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK, body: `{"recordings":[]}`}
	c := newTestClient(t, doer)

	recordings, err := c.Search(t.Context(), "Turdus migratorius")
	require.NoError(t, err)
	assert.Empty(t, recordings)
}
