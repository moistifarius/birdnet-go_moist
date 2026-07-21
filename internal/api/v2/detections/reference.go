package detections

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/httpclient"
	"github.com/tphakala/birdnet-go/internal/logger"
	"github.com/tphakala/birdnet-go/internal/reference"
)

// referenceHTTPTimeout bounds outbound Xeno-canto requests.
const referenceHTTPTimeout = 15 * time.Second

// maxReferenceAlternatives caps how many "try another example" alternatives are
// returned alongside the best match.
const maxReferenceAlternatives = 4

// ReferenceResponse is the api/v2 response for GET /detections/:id/reference.
// When the feature is disabled only Enabled (false) is populated. When enabled
// but no example could be found (or the online source failed), Enabled is true
// and Best is nil — the frontend then simply shows no example.
type ReferenceResponse struct {
	Enabled      bool                 `json:"enabled"`
	Best         *ReferenceRecording  `json:"best,omitempty"`
	Alternatives []ReferenceRecording `json:"alternatives,omitempty"`
}

// ReferenceRecording is the metadata the UI needs to play and attribute a
// trusted example.
type ReferenceRecording struct {
	ID             string `json:"id"`
	ScientificName string `json:"scientificName,omitempty"`
	CommonName     string `json:"commonName,omitempty"`
	Recordist      string `json:"recordist,omitempty"`
	Country        string `json:"country,omitempty"`
	CallType       string `json:"callType,omitempty"`
	PageURL        string `json:"pageUrl,omitempty"`
	AudioURL       string `json:"audioUrl,omitempty"`
	SonogramURL    string `json:"sonogramUrl,omitempty"`
	LicenseName    string `json:"licenseName,omitempty"`
	LicenseURL     string `json:"licenseUrl,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Length         string `json:"length,omitempty"`
	SourceProvider string `json:"sourceProvider,omitempty"`
}

// GetDetectionReference returns the closest trusted example recording(s) for a
// detection's species from the configured reference source (Xeno-canto), ranked
// biological-plausibility-first. It is read-only and best-effort: a disabled
// feature or an online-source failure never errors the endpoint, so the local
// detection UI is never blocked by it.
func (c *Handler) GetDetectionReference(ctx echo.Context) error {
	settings := c.CurrentSettings()
	if !referenceEnabled(settings) {
		return ctx.JSON(http.StatusOK, ReferenceResponse{Enabled: false})
	}

	id := ctx.Param("id")
	note, err := c.DS.Get(id)
	if err != nil {
		return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	}

	client := c.referenceClient(settings.Realtime.IdentificationCheck.Xenocanto.APIKey)
	if !client.Configured() {
		return ctx.JSON(http.StatusOK, ReferenceResponse{Enabled: false})
	}

	recordings, err := client.Search(ctx.Request().Context(), note.ScientificName)
	if err != nil {
		// Online-source failures are non-fatal: report the feature as enabled but
		// with no example rather than surfacing an error to the user.
		c.LogWarnIfEnabled("reference recording lookup failed",
			logger.String("species", note.ScientificName),
			logger.Error(err))
		return ctx.JSON(http.StatusOK, ReferenceResponse{Enabled: true})
	}

	ranked := reference.Rank(recordings, reference.Preferences{Month: monthFromDate(note.Date)})
	resp := ReferenceResponse{Enabled: true}
	if len(ranked) > 0 {
		best := toReferenceRecording(&ranked[0])
		resp.Best = &best
		limit := min(len(ranked), 1+maxReferenceAlternatives)
		resp.Alternatives = make([]ReferenceRecording, 0, limit-1)
		for i := 1; i < limit; i++ {
			resp.Alternatives = append(resp.Alternatives, toReferenceRecording(&ranked[i]))
		}
	}
	return ctx.JSON(http.StatusOK, resp)
}

// referenceEnabled reports whether the reference-recording feature is active.
func referenceEnabled(settings *conf.Settings) bool {
	return settings != nil &&
		settings.Realtime.IdentificationCheck.Enabled &&
		settings.Realtime.IdentificationCheck.Xenocanto.Enabled
}

// referenceClient returns the shared Xeno-canto client, rebuilding it only when
// the API key changes so its per-species cache survives across requests.
func (c *Handler) referenceClient(apiKey string) *reference.Client {
	c.refMu.Lock()
	defer c.refMu.Unlock()
	if c.refClient == nil || c.refKey != apiKey {
		c.refClient = reference.NewClient(apiKey, c.referenceHTTPDoer())
		c.refKey = apiKey
	}
	return c.refClient
}

// referenceHTTPDoer returns the injected test doer when set, otherwise a
// timeout-bounded HTTP client using the project's default (CA-aware) transport.
func (c *Handler) referenceHTTPDoer() reference.Doer {
	if c.referenceDoer != nil {
		return c.referenceDoer
	}
	return &http.Client{
		Timeout:   referenceHTTPTimeout,
		Transport: httpclient.CloneDefaultTransport(),
	}
}

// monthFromDate returns the 1-12 month of a "YYYY-MM-DD" date, or 0 when it
// cannot be parsed.
func monthFromDate(date string) int {
	if t, err := time.Parse(time.DateOnly, date); err == nil {
		return int(t.Month())
	}
	return 0
}

// toReferenceRecording maps the provider-neutral recording to the API DTO.
func toReferenceRecording(r *reference.Recording) ReferenceRecording {
	return ReferenceRecording{
		ID:             r.ID,
		ScientificName: r.ScientificName,
		CommonName:     r.CommonName,
		Recordist:      r.Recordist,
		Country:        r.Country,
		CallType:       r.CallType,
		PageURL:        r.PageURL,
		AudioURL:       r.AudioURL,
		SonogramURL:    r.SonogramURL,
		LicenseName:    r.LicenseName,
		LicenseURL:     r.LicenseURL,
		Quality:        r.Quality,
		Length:         r.Length,
		SourceProvider: r.SourceProvider,
	}
}
