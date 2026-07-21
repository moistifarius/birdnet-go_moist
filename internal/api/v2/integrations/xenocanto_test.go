package integrations

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
)

// fakeXCDoer is an injectable HTTP doer returning a canned response.
type fakeXCDoer struct {
	status int
	body   string
	err    error
}

func (f *fakeXCDoer) Do(_ *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func postXenocantoTest(t *testing.T, e *echo.Echo, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/integrations/xenocanto/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, h.TestXenocantoConnection(c))
	return rec
}

func TestTestXenocantoConnection(t *testing.T) {
	const okBody = `{"recordings":[{"id":"1","gen":"Turdus","sp":"merula","file":"//xeno-canto.org/1/download","lic":"//creativecommons.org/licenses/by/4.0/"}]}`

	t.Run("reports success when the key works", func(t *testing.T) {
		e, h := newIntegrationsTestHandler(t)
		apitest.PublishTestSettings(t, apitest.NewValidTestSettings())
		h.xenocantoDoer = &fakeXCDoer{status: http.StatusOK, body: okBody}

		rec := postXenocantoTest(t, e, h, `{"enabled":true,"apiKey":"real-key"}`)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"success":true`)
	})

	t.Run("reports failure when Xeno-canto rejects the request", func(t *testing.T) {
		e, h := newIntegrationsTestHandler(t)
		apitest.PublishTestSettings(t, apitest.NewValidTestSettings())
		h.xenocantoDoer = &fakeXCDoer{status: http.StatusUnauthorized, body: "nope"}

		rec := postXenocantoTest(t, e, h, `{"enabled":true,"apiKey":"bad-key"}`)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"success":false`)
	})

	t.Run("requires an API key", func(t *testing.T) {
		e, h := newIntegrationsTestHandler(t)
		apitest.PublishTestSettings(t, apitest.NewValidTestSettings())

		rec := postXenocantoTest(t, e, h, `{"enabled":true,"apiKey":""}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "API key is required")
	})
}
