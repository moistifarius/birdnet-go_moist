package detections

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/idcheck"
)

// idCheckTimeLayout parses a note's stored Date + " " + Time into a timestamp
// when the pre-parsed BeginTime is unavailable. It matches the layout used by
// GetDetectionTimeOfDay.
const idCheckTimeLayout = "2006-01-02 15:04:05"

// IDCheckResponse is the api/v2 response for GET /detections/:id/id-check. When
// the feature is disabled only Enabled (false) is populated; the frontend hides
// the verdict in that case.
type IDCheckResponse struct {
	Enabled bool             `json:"enabled"`
	Verdict string           `json:"verdict,omitempty"`
	Signals []idcheck.Signal `json:"signals,omitempty"`
	Details *IDCheckDetails  `json:"details,omitempty"`
}

// IDCheckDetails carries the raw signal values behind the verdict for the
// "details for experts" view. Occurrence is a pointer so it is omitted when the
// range model could not assess the species.
type IDCheckDetails struct {
	Confidence         float64  `json:"confidence"`
	LocationConfigured bool     `json:"locationConfigured"`
	Occurrence         *float64 `json:"occurrence,omitempty"`
	DailyCount         int      `json:"dailyCount"`
	FlaggedUnlikely    bool     `json:"flaggedUnlikely"`
	ModelType          string   `json:"modelType,omitempty"`
}

// GetDetectionIDCheck computes the plain-language identification-check verdict
// for a single detection from local signals: how clear it was, whether the
// species is expected near the user now, how often it was heard, and whether a
// quality filter flagged it. It is a read-only, on-demand endpoint so the clean
// single-query detection list path is never burdened with the per-detection
// range inference and count this needs.
//
// The verdict is best-effort: any signal that cannot be gathered (no location
// configured, the range model not yet loaded, a failed count) is simply left
// unassessed rather than counted against the detection.
func (c *Handler) GetDetectionIDCheck(ctx echo.Context) error {
	// Read settings per request so the enable flag hot-reloads. Check it before
	// touching the datastore so a disabled feature does no work.
	settings := c.CurrentSettings()
	if settings == nil || !settings.Realtime.IdentificationCheck.Enabled {
		return ctx.JSON(http.StatusOK, IDCheckResponse{Enabled: false})
	}

	id := ctx.Param("id")
	note, err := c.DS.Get(id)
	if err != nil {
		return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	}

	modelType := note.Model.ModelType
	if modelType == "" {
		modelType = defaultModelType
	}

	in := idcheck.Inputs{
		Confidence:      note.Confidence,
		FlaggedUnlikely: note.Unlikely,
	}
	details := &IDCheckDetails{
		Confidence:      note.Confidence,
		FlaggedUnlikely: note.Unlikely,
		ModelType:       modelType,
	}

	// "Heard several times" — one cheap COUNT for this species on the detection's
	// date. The note itself is always counted, so the floor is 1.
	in.DailyCount = 1
	if count, cErr := c.DS.CountSpeciesDetections(note.ScientificName, note.Date, "", 0); cErr == nil && count > 1 {
		in.DailyCount = int(count)
	}
	details.DailyCount = in.DailyCount

	// "Expected near you now" — the range/occurrence model. Only meaningful once
	// a location is configured; any failure degrades to an unassessed signal.
	in.LocationConfigured = settings.BirdNET.LocationConfigured
	details.LocationConfigured = in.LocationConfigured
	if in.LocationConfigured {
		in.Included = settings.IsSpeciesIncluded(note.ScientificName)
		if orch, oErr := c.GetBirdNETInstance(); oErr == nil && orch != nil {
			occurrence := orch.GetSpeciesOccurrenceAtTime(note.ScientificName, idCheckDetectionTime(&note))
			in.Occurrence = occurrence
			details.Occurrence = &occurrence
		}
	}

	result := idcheck.Evaluate(in)
	return ctx.JSON(http.StatusOK, IDCheckResponse{
		Enabled: true,
		Verdict: result.Verdict,
		Signals: result.Signals,
		Details: details,
	})
}

// idCheckDetectionTime returns the best available timestamp for a note: the
// pre-parsed BeginTime when set, otherwise the stored Date + Time parsed in the
// local timezone.
func idCheckDetectionTime(note *datastore.Note) time.Time {
	if !note.BeginTime.IsZero() {
		return note.BeginTime
	}
	if t, err := time.ParseInLocation(idCheckTimeLayout, note.Date+" "+note.Time, time.Local); err == nil {
		return t
	}
	return note.BeginTime
}
