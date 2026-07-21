package integrations

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/httpclient"
	"github.com/tphakala/birdnet-go/internal/reference"
)

// xenocantoTestSpecies is a widely-recorded species used to verify Xeno-canto
// connectivity and that the API key is accepted.
const xenocantoTestSpecies = "Turdus merula"

// XenocantoTestRequest is the POST body for the Xeno-canto connection test.
type XenocantoTestRequest struct {
	Enabled bool   `json:"enabled"`
	APIKey  string `json:"apiKey"`
}

// TestXenocantoConnection handles POST /api/v2/integrations/xenocanto/test. It
// runs a single search against Xeno-canto with the (restored) API key and
// reports whether the key works, without the streaming multi-stage protocol used
// by the other integrations.
func (c *Handler) TestXenocantoConnection(ctx echo.Context) error {
	var request XenocantoTestRequest
	if err := ctx.Bind(&request); err != nil {
		return c.HandleError(ctx, err, "Invalid Xeno-canto test request", http.StatusBadRequest)
	}

	// Restore the redacted API key so the test runs against the real saved secret
	// when the user has not re-entered it in the form.
	apicore.RestoreRedactedSecret(
		c.CurrentSettings().Realtime.IdentificationCheck.Xenocanto.APIKey,
		&request.APIKey,
	)

	if request.APIKey == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Xeno-canto API key is required",
		})
	}

	testCtx, cancel := context.WithTimeout(ctx.Request().Context(), integrationMediumTimeout*time.Second)
	defer cancel()

	client := reference.NewClient(request.APIKey, c.xenocantoTestHTTPDoer())
	recordings, err := client.Search(testCtx, xenocantoTestSpecies)
	if err != nil {
		return ctx.JSON(http.StatusOK, map[string]any{
			"success": false,
			"message": "Could not reach Xeno-canto with this key. Check the key and your connection.",
		})
	}

	return ctx.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "Connected to Xeno-canto successfully.",
		"count":   len(recordings),
	})
}

// xenocantoTestHTTPDoer returns the injected test doer when set, otherwise a
// timeout-bounded HTTP client using the project's default (CA-aware) transport.
func (c *Handler) xenocantoTestHTTPDoer() reference.Doer {
	if c.xenocantoDoer != nil {
		return c.xenocantoDoer
	}
	return &http.Client{
		Timeout:   integrationMediumTimeout * time.Second,
		Transport: httpclient.CloneDefaultTransport(),
	}
}
