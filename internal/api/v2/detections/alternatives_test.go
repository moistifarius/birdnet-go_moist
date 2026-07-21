package detections

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/datastore"
)

func doAlternativesRequest(t *testing.T, e *echo.Echo, h *Handler, id string) AlternativesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/detections/"+id+"/alternatives", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	require.NoError(t, h.GetDetectionAlternatives(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AlternativesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestGetDetectionAlternatives(t *testing.T) {
	const id = "1"
	note := datastore.Note{ID: 1, Date: "2025-07-05", ScientificName: "Turdus migratorius"}
	results := []datastore.Results{
		{Species: "Spizella passerina_Chipping Sparrow", Confidence: 0.6},
		{Species: "Turdus migratorius_American Robin", Confidence: 0.9}, // the detected species, excluded
		{Species: "Passer domesticus_House Sparrow", Confidence: 0.3},
	}

	t.Run("orders alternatives by confidence and excludes the detected species", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, false, "")) // Xeno-canto off
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(note, nil)
		mockDS.On("GetNoteResults", id).Return(results, nil)

		resp := doAlternativesRequest(t, e, h, id)
		assert.True(t, resp.Enabled)
		require.Len(t, resp.Alternatives, 2)
		assert.Equal(t, "Spizella passerina", resp.Alternatives[0].ScientificName)
		assert.Equal(t, "Chipping Sparrow", resp.Alternatives[0].CommonName)
		assert.Equal(t, "Passer domesticus", resp.Alternatives[1].ScientificName)
		assert.Nil(t, resp.Alternatives[0].Example, "no example when Xeno-canto is off")
	})

	t.Run("attaches trusted examples when Xeno-canto is configured", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, true, "key"))
		h.referenceDoer = &fakeReferenceDoer{status: http.StatusOK, body: referenceSampleBody}
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(note, nil)
		mockDS.On("GetNoteResults", id).Return(results, nil)

		resp := doAlternativesRequest(t, e, h, id)
		require.Len(t, resp.Alternatives, 2)
		require.NotNil(t, resp.Alternatives[0].Example)
		assert.NotEmpty(t, resp.Alternatives[0].Example.AudioURL)
	})

	t.Run("disabled feature short-circuits", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(false, false, ""))
		mockDS.ExpectedCalls = nil

		resp := doAlternativesRequest(t, e, h, id)
		assert.False(t, resp.Enabled)
		assert.Empty(t, resp.Alternatives)
	})

	t.Run("no runner-up results yields no alternatives", func(t *testing.T) {
		e, mockDS, h := setupTestEnvironment(t)
		apitest.PublishTestSettings(t, referenceTestSettings(true, false, ""))
		mockDS.ExpectedCalls = nil
		mockDS.On("Get", id).Return(note, nil)
		mockDS.On("GetNoteResults", id).Return([]datastore.Results{}, nil)

		resp := doAlternativesRequest(t, e, h, id)
		assert.True(t, resp.Enabled)
		assert.Empty(t, resp.Alternatives)
	})
}
