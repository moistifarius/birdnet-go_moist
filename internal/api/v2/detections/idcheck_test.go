package detections

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/idcheck"
)

// idCheckTestSettings returns valid test settings with the identification-check
// feature enabled or disabled and no location configured (so the range/occurrence
// signal is cleanly unassessed and the test does not depend on a loaded model).
func idCheckTestSettings(enabled bool) *conf.Settings {
	s := apitest.NewValidTestSettings()
	s.Realtime.IdentificationCheck.Enabled = enabled
	s.BirdNET.LocationConfigured = false
	return s
}

// signalStatusOf returns the status of the signal with the given code in the
// response, or empty string when the signal is absent.
func signalStatusOf(resp IDCheckResponse, code string) string {
	for _, s := range resp.Signals {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

func TestGetDetectionIDCheck(t *testing.T) {
	const (
		scientificName = "Corvus brachyrhynchos"
		date           = "2025-03-07"
		id             = "1"
	)

	testCases := []struct {
		name           string
		settings       *conf.Settings
		mockSetup      func(*mock.Mock)
		expectedStatus int
		checkResponse  func(*testing.T, IDCheckResponse)
	}{
		{
			name:     "strong verdict from clear, frequently heard detection",
			settings: idCheckTestSettings(true),
			mockSetup: func(m *mock.Mock) {
				m.On("Get", id).Return(datastore.Note{
					ID:             1,
					Date:           date,
					Time:           "08:15:00",
					ScientificName: scientificName,
					Confidence:     0.95,
				}, nil)
				m.On("CountSpeciesDetections", scientificName, date, "", 0).Return(int64(5), nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp IDCheckResponse) {
				t.Helper()
				assert.True(t, resp.Enabled)
				assert.Equal(t, idcheck.VerdictStrong, resp.Verdict)
				assert.Equal(t, idcheck.StatusPass, signalStatusOf(resp, idcheck.SignalSoundClarity))
				assert.Equal(t, idcheck.StatusUnknown, signalStatusOf(resp, idcheck.SignalExpectedHere))
				assert.Equal(t, idcheck.StatusPass, signalStatusOf(resp, idcheck.SignalHeardOften))
				require.NotNil(t, resp.Details)
				assert.InDelta(t, 0.95, resp.Details.Confidence, 0.001)
				assert.Equal(t, 5, resp.Details.DailyCount)
				assert.False(t, resp.Details.LocationConfigured)
				assert.Nil(t, resp.Details.Occurrence, "occurrence omitted when unassessed")
				assert.Equal(t, defaultModelType, resp.Details.ModelType)
			},
		},
		{
			name:     "weak verdict from faint, once-heard, flagged detection",
			settings: idCheckTestSettings(true),
			mockSetup: func(m *mock.Mock) {
				m.On("Get", id).Return(datastore.Note{
					ID:             1,
					Date:           date,
					Time:           "08:15:00",
					ScientificName: scientificName,
					Confidence:     0.40,
					Unlikely:       true,
				}, nil)
				m.On("CountSpeciesDetections", scientificName, date, "", 0).Return(int64(1), nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp IDCheckResponse) {
				t.Helper()
				assert.True(t, resp.Enabled)
				assert.Equal(t, idcheck.VerdictWeak, resp.Verdict)
				assert.Equal(t, idcheck.StatusWarn, signalStatusOf(resp, idcheck.SignalSoundClarity))
				assert.Equal(t, idcheck.StatusWarn, signalStatusOf(resp, idcheck.SignalHeardOften))
				assert.Equal(t, idcheck.StatusWarn, signalStatusOf(resp, idcheck.SignalRecordingQuality))
				require.NotNil(t, resp.Details)
				assert.True(t, resp.Details.FlaggedUnlikely)
			},
		},
		{
			name:     "disabled feature returns enabled=false and does no datastore work",
			settings: idCheckTestSettings(false),
			// No mock expectations: the handler must short-circuit before Get.
			mockSetup:      func(_ *mock.Mock) {},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp IDCheckResponse) {
				t.Helper()
				assert.False(t, resp.Enabled)
				assert.Empty(t, resp.Verdict)
				assert.Nil(t, resp.Details)
			},
		},
		{
			name:     "count failure floors the daily count to one",
			settings: idCheckTestSettings(true),
			mockSetup: func(m *mock.Mock) {
				m.On("Get", id).Return(datastore.Note{
					ID:             1,
					Date:           date,
					Time:           "08:15:00",
					ScientificName: scientificName,
					Confidence:     0.95,
				}, nil)
				m.On("CountSpeciesDetections", scientificName, date, "", 0).
					Return(int64(0), errors.Newf("db error").Build())
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp IDCheckResponse) {
				t.Helper()
				assert.True(t, resp.Enabled)
				require.NotNil(t, resp.Details)
				assert.Equal(t, 1, resp.Details.DailyCount)
				assert.Equal(t, idcheck.StatusWarn, signalStatusOf(resp, idcheck.SignalHeardOften))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e, mockDS, controller := setupTestEnvironment(t)
			apitest.PublishTestSettings(t, tc.settings)
			mockDS.ExpectedCalls = nil
			tc.mockSetup(&mockDS.Mock)

			req := httptest.NewRequest(http.MethodGet, "/api/v2/detections/"+id+"/id-check", http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(id)

			err := controller.GetDetectionIDCheck(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedStatus, rec.Code)

			var resp IDCheckResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			tc.checkResponse(t, resp)

			mockDS.AssertExpectations(t)
		})
	}
}

func TestGetDetectionIDCheck_NotFound(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)
	apitest.PublishTestSettings(t, idCheckTestSettings(true))
	mockDS.ExpectedCalls = nil
	mockDS.On("Get", "999").Return(datastore.Note{}, errors.Newf("record not found").Build())

	req := httptest.NewRequest(http.MethodGet, "/api/v2/detections/999/id-check", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	// HandleError writes the response and returns nil; assert on the recorder.
	_ = controller.GetDetectionIDCheck(c)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	mockDS.AssertExpectations(t)
}
