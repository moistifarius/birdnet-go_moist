package detections

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/logger"
	"github.com/tphakala/birdnet-go/internal/reference"
)

// maxAlternativeSpecies caps how many alternative species are offered when a
// user is unsure about an identification.
const maxAlternativeSpecies = 3

// AlternativeSpecies is a plausible alternative identification for a detection,
// optionally with a trusted example to compare against.
type AlternativeSpecies struct {
	ScientificName string              `json:"scientificName"`
	CommonName     string              `json:"commonName,omitempty"`
	Confidence     float64             `json:"confidence,omitempty"`
	Example        *ReferenceRecording `json:"example,omitempty"`
}

// AlternativesResponse is the api/v2 response for GET
// /detections/:id/alternatives.
type AlternativesResponse struct {
	Enabled      bool                 `json:"enabled"`
	Alternatives []AlternativeSpecies `json:"alternatives,omitempty"`
}

// GetDetectionAlternatives returns the most plausible alternative species for a
// detection, drawn from the classifier's own runner-up predictions for that
// clip. When the online reference source is configured, each alternative is
// returned with its closest trusted example so the user can compare. Read-only
// and best-effort.
func (c *Handler) GetDetectionAlternatives(ctx echo.Context) error {
	settings := c.CurrentSettings()
	if settings == nil || !settings.Realtime.IdentificationCheck.Enabled {
		return ctx.JSON(http.StatusOK, AlternativesResponse{Enabled: false})
	}

	id := ctx.Param("id")
	note, err := c.DS.Get(id)
	if err != nil {
		return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	}

	results, err := c.DS.GetNoteResults(id)
	if err != nil {
		// The runner-up predictions are a nice-to-have; a failure here just means
		// no alternatives, not an error.
		c.LogWarnIfEnabled("alternative species lookup failed", logger.Error(err))
		results = nil
	}

	alternatives := buildAlternatives(results, note.ScientificName, maxAlternativeSpecies)
	c.attachAlternativeExamples(ctx, settings, note.Date, alternatives)

	return ctx.JSON(http.StatusOK, AlternativesResponse{Enabled: true, Alternatives: alternatives})
}

// buildAlternatives turns the classifier runner-up results into a de-duplicated,
// confidence-ordered list of alternative species, excluding the detected
// species itself.
func buildAlternatives(results []datastore.Results, primaryScientific string, limit int) []AlternativeSpecies {
	sorted := make([]datastore.Results, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Confidence > sorted[b].Confidence })

	seen := map[string]bool{strings.ToLower(strings.TrimSpace(primaryScientific)): true}
	out := make([]AlternativeSpecies, 0, limit)
	for i := range sorted {
		sp := detection.ParseSpeciesString(sorted[i].Species)
		key := strings.ToLower(strings.TrimSpace(sp.ScientificName))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, AlternativeSpecies{
			ScientificName: sp.ScientificName,
			CommonName:     sp.CommonName,
			Confidence:     float64(sorted[i].Confidence),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// attachAlternativeExamples fills in each alternative's trusted example when the
// reference source is configured. Failures are silently skipped per alternative.
func (c *Handler) attachAlternativeExamples(ctx echo.Context, settings *conf.Settings, detectionDate string, alternatives []AlternativeSpecies) {
	if !referenceEnabled(settings) {
		return
	}
	client := c.referenceClient(settings.Realtime.IdentificationCheck.Xenocanto.APIKey)
	if !client.Configured() {
		return
	}
	prefs := reference.Preferences{Month: monthFromDate(detectionDate)}
	for i := range alternatives {
		recordings, err := client.Search(ctx.Request().Context(), alternatives[i].ScientificName)
		if err != nil {
			continue
		}
		best, err := reference.SelectBest(recordings, prefs)
		if err != nil {
			continue
		}
		example := toReferenceRecording(&best)
		alternatives[i].Example = &example
	}
}
