package detections

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/reference"
)

func TestIsAllowedReferenceAudioURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"canonical https host", "https://xeno-canto.org/1/download", true},
		{"subdomain", "https://www.xeno-canto.org/1/download", true},
		{"asset subdomain", "https://sound.xeno-canto.org/XC1.mp3", true},
		{"uppercase host", "https://XENO-CANTO.ORG/1/download", true},
		{"trailing dot host", "https://xeno-canto.org./1/download", true},
		{"http not https", "http://xeno-canto.org/1/download", false},
		{"foreign host", "https://evil.example.com/1/download", false},
		{"look-alike suffix trick", "https://xeno-canto.org.evil.com/1", false},
		{"substring not suffix", "https://notxeno-canto.org/1", false},
		{"ip literal", "https://127.0.0.1/1/download", false},
		{"ipv6 literal", "https://[::1]/1/download", false},
		{"empty", "", false},
		{"scheme-relative", "//xeno-canto.org/1/download", false},
		{"not a url", "::::", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isAllowedReferenceAudioURL(tt.url))
		})
	}
}

func TestReferenceClipCacheKey(t *testing.T) {
	t.Parallel()
	base := referenceClipCacheKey("xeno-canto", "https://xeno-canto.org/1/download", 6)
	assert.Equal(t, base, referenceClipCacheKey("xeno-canto", "https://xeno-canto.org/1/download", 6),
		"same inputs must produce the same key")
	assert.NotEqual(t, base, referenceClipCacheKey("xeno-canto", "https://xeno-canto.org/2/download", 6),
		"different URL must produce a different key")
	assert.NotEqual(t, base, referenceClipCacheKey("xeno-canto", "https://xeno-canto.org/1/download", 10),
		"different crop length must produce a different key")
}

func TestReferenceClipCache(t *testing.T) {
	t.Parallel()

	t.Run("get returns stored bytes", func(t *testing.T) {
		t.Parallel()
		c := newReferenceClipCache(4)
		c.put("a", []byte("one"))
		got, ok := c.get("a")
		require.True(t, ok)
		assert.Equal(t, []byte("one"), got)

		_, ok = c.get("missing")
		assert.False(t, ok)
	})

	t.Run("evicts oldest when full (FIFO)", func(t *testing.T) {
		t.Parallel()
		c := newReferenceClipCache(2)
		c.put("a", []byte("1"))
		c.put("b", []byte("2"))
		c.put("c", []byte("3")) // evicts "a"

		_, ok := c.get("a")
		assert.False(t, ok, "oldest entry should be evicted")
		_, ok = c.get("b")
		assert.True(t, ok)
		_, ok = c.get("c")
		assert.True(t, ok)
	})

	t.Run("overwriting a key does not grow or evict", func(t *testing.T) {
		t.Parallel()
		c := newReferenceClipCache(2)
		c.put("a", []byte("1"))
		c.put("b", []byte("2"))
		c.put("a", []byte("1b")) // update in place
		c.put("c", []byte("3"))  // evicts the true-oldest "b", keeps "a"

		got, ok := c.get("a")
		require.True(t, ok)
		assert.Equal(t, []byte("1b"), got)
		_, ok = c.get("c")
		assert.True(t, ok)
	})
}

func TestSelectReferenceRecording(t *testing.T) {
	t.Parallel()
	ranked := []reference.Recording{
		{ID: "1", AudioURL: "https://xeno-canto.org/1/download"},
		{ID: "2", AudioURL: "https://xeno-canto.org/2/download"},
	}

	t.Run("defaults to best-ranked", func(t *testing.T) {
		t.Parallel()
		got := selectReferenceRecording(ranked, "")
		require.NotNil(t, got)
		assert.Equal(t, "1", got.ID)
	})

	t.Run("honors an explicit recording id", func(t *testing.T) {
		t.Parallel()
		got := selectReferenceRecording(ranked, "2")
		require.NotNil(t, got)
		assert.Equal(t, "2", got.ID)
	})

	t.Run("falls back to best when id is unknown", func(t *testing.T) {
		t.Parallel()
		got := selectReferenceRecording(ranked, "999")
		require.NotNil(t, got)
		assert.Equal(t, "1", got.ID)
	})

	t.Run("nil when empty", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, selectReferenceRecording(nil, ""))
	})
}

func TestReferenceAudioCheckRedirect(t *testing.T) {
	t.Parallel()

	mustReq := func(u string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, u, http.NoBody)
		require.NoError(t, err)
		return r
	}

	t.Run("allows redirect that stays on host", func(t *testing.T) {
		t.Parallel()
		err := referenceAudioCheckRedirect(mustReq("https://sound.xeno-canto.org/XC1.mp3"), nil)
		assert.NoError(t, err)
	})

	t.Run("rejects redirect to a foreign host", func(t *testing.T) {
		t.Parallel()
		err := referenceAudioCheckRedirect(mustReq("https://evil.example.com/x.mp3"), nil)
		assert.Error(t, err)
	})

	t.Run("rejects after too many hops", func(t *testing.T) {
		t.Parallel()
		via := make([]*http.Request, maxReferenceRedirects)
		err := referenceAudioCheckRedirect(mustReq("https://xeno-canto.org/x.mp3"), via)
		assert.Error(t, err)
	})
}

// clipTestSettings builds enabled identification-check settings with a non-empty
// ffmpeg path so the clip handler proceeds past its prerequisite checks.
func clipTestSettings(enabled, xcEnabled bool, apiKey string) *conf.Settings {
	s := referenceTestSettings(enabled, xcEnabled, apiKey)
	s.Realtime.Audio.FfmpegPath = "ffmpeg"
	return s
}

// doClipRequest invokes GetDetectionReferenceClip and returns the recorder.
func doClipRequest(t *testing.T, e *echo.Echo, h *Handler, id, rec string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/v2/detections/" + id + "/reference/clip"
	if rec != "" {
		target += "?rec=" + rec
	}
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
	w := httptest.NewRecorder()
	c := e.NewContext(req, w)
	c.SetParamNames("id")
	c.SetParamValues(id)
	require.NoError(t, h.GetDetectionReferenceClip(c))
	return w
}

func TestGetDetectionReferenceClip(t *testing.T) {
	const id = "1"

	t.Run("disabled feature returns 404 without touching datastore", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, clipTestSettings(true, false, "key"))
		mockDS.ExpectedCalls = nil

		w := doClipRequest(t, e, h, id, "")
		assert.Equal(t, http.StatusNotFound, w.Code)
		mockDS.AssertExpectations(t)
	})

	t.Run("missing ffmpeg returns 404 before datastore", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		s := clipTestSettings(true, true, "key")
		s.Realtime.Audio.FfmpegPath = ""
		apitest.PublishTestSettings(t, s)
		mockDS.ExpectedCalls = nil

		w := doClipRequest(t, e, h, id, "")
		assert.Equal(t, http.StatusNotFound, w.Code)
		mockDS.AssertExpectations(t)
	})

	t.Run("serves the cached processed clip without fetching or invoking ffmpeg", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, clipTestSettings(true, true, "key"))
		h.referenceDoer = &fakeReferenceDoer{status: http.StatusOK, body: referenceSampleBody}
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(datastore.Note{
			ID:             1,
			Date:           "2025-07-05",
			ScientificName: "Turdus migratorius",
		}, nil)

		// Pre-seed the cache for the best-ranked recording (id "1"), so the handler
		// returns it directly and never reaches the network/ffmpeg path.
		key := referenceClipCacheKey(reference.ProviderXenoCanto, "https://xeno-canto.org/1/download", referenceClipCropSeconds)
		h.clipCache().put(key, []byte("FAKE-MP3-BYTES"))

		w := doClipRequest(t, e, h, id, "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, referenceClipContentType, w.Header().Get("Content-Type"))
		assert.Equal(t, "FAKE-MP3-BYTES", w.Body.String())
		assert.Contains(t, w.Header().Get("Cache-Control"), "max-age=")
	})

	t.Run("rejects a source host outside the allowlist (SSRF guard)", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, clipTestSettings(true, true, "key"))
		const offHostBody = `{"recordings":[{"id":"9","gen":"Turdus","sp":"migratorius","rec":"X","file":"//evil.example.com/9/download","lic":"//creativecommons.org/licenses/by/4.0/","q":"A","date":"2025-07-01"}]}`
		h.referenceDoer = &fakeReferenceDoer{status: http.StatusOK, body: offHostBody}
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(datastore.Note{
			ID:             1,
			Date:           "2025-07-05",
			ScientificName: "Turdus migratorius",
		}, nil)

		w := doClipRequest(t, e, h, id, "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
