package detections

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
)

// fakeReferenceDoer is an in-package HTTP doer returning a canned response.
type fakeReferenceDoer struct {
	status int
	body   string
	calls  int
}

func (f *fakeReferenceDoer) Do(_ *http.Request) (*http.Response, error) {
	f.calls++
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func referenceTestSettings(enabled, xcEnabled bool, apiKey string) *conf.Settings {
	s := apitest.NewValidTestSettings()
	s.Realtime.IdentificationCheck.Enabled = enabled
	s.Realtime.IdentificationCheck.Xenocanto.Enabled = xcEnabled
	s.Realtime.IdentificationCheck.Xenocanto.APIKey = apiKey
	return s
}

const referenceSampleBody = `{"recordings":[
  {"id":"1","gen":"Turdus","sp":"migratorius","en":"American Robin","rec":"Jane Smith","cnt":"United States","type":"song","url":"//xeno-canto.org/1","file":"//xeno-canto.org/1/download","lic":"//creativecommons.org/licenses/by-nc-sa/4.0/","q":"A","length":"0:10","date":"2025-07-01"},
  {"id":"2","gen":"Turdus","sp":"migratorius","rec":"Bob Jones","cnt":"Canada","type":"call","url":"//xeno-canto.org/2","file":"//xeno-canto.org/2/download","lic":"//creativecommons.org/licenses/by/4.0/","q":"C","length":"0:05","date":"2025-01-01"}
]}`

func TestGetDetectionReference(t *testing.T) {
	const id = "1"

	t.Run("returns best and alternatives when enabled", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, true, "key"))
		h.referenceDoer = &fakeReferenceDoer{status: http.StatusOK, body: referenceSampleBody}
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(datastore.Note{
			ID:             1,
			Date:           "2025-07-05",
			ScientificName: "Turdus migratorius",
		}, nil)

		resp := doReferenceRequest(t, e, h, id)
		assert.True(t, resp.Enabled)
		require.NotNil(t, resp.Best)
		assert.Equal(t, "1", resp.Best.ID) // quality A, same season -> best
		assert.Equal(t, "Jane Smith", resp.Best.Recordist)
		assert.Equal(t, "https://xeno-canto.org/1/download", resp.Best.AudioURL)
		assert.Equal(t, "CC BY-NC-SA 4.0", resp.Best.LicenseName)
		require.Len(t, resp.Alternatives, 1)
		assert.Equal(t, "2", resp.Alternatives[0].ID)
		mockDS.AssertExpectations(t)
	})

	t.Run("disabled feature short-circuits before datastore", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, false, "key"))
		mockDS.ExpectedCalls = nil

		resp := doReferenceRequest(t, e, h, id)
		assert.False(t, resp.Enabled)
		assert.Nil(t, resp.Best)
		mockDS.AssertExpectations(t)
	})

	t.Run("enabled without key reports disabled", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, true, ""))
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(datastore.Note{ID: 1, ScientificName: "Turdus migratorius"}, nil)

		resp := doReferenceRequest(t, e, h, id)
		assert.False(t, resp.Enabled)
	})

	t.Run("online-source failure is non-fatal", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, true, "key"))
		h.referenceDoer = &fakeReferenceDoer{status: http.StatusInternalServerError, body: "boom"}
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(datastore.Note{ID: 1, ScientificName: "Turdus migratorius"}, nil)

		resp := doReferenceRequest(t, e, h, id)
		assert.True(t, resp.Enabled, "an online failure must not disable the feature")
		assert.Nil(t, resp.Best)
	})
}

// doReferenceRequest invokes GetDetectionReference and decodes the response.
func doReferenceRequest(t *testing.T, e *echo.Echo, h *Handler, id string) ReferenceResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/detections/"+id+"/reference", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	require.NoError(t, h.GetDetectionReference(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp ReferenceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}
