// Package idcheck computes a plain-language "identification check" verdict for a
// bird detection from signals BirdNET-Go already has locally: how clear the
// detection was, whether the species is expected near the user right now, how
// often it was heard, and whether a quality filter flagged it.
//
// The package is deliberately dependency-free and pure: Evaluate takes a plain
// Inputs value and returns a Result, so the aggregation logic is trivially
// unit-testable and reusable. Callers (the api/v2 detections handler) are
// responsible for gathering the raw signal values from the datastore, the range
// filter, and the settings, then handing them to Evaluate.
//
// The verdict intentionally avoids exposing a false-precision numeric score to
// the user. It reports one of three qualitative support levels — strong, mixed,
// or weak — backed by a small set of qualitative signals, each of which is a
// stable code plus a status. The user-facing wording and localization live in
// the frontend; this package only emits codes and statuses.
//
// Signals that cannot be assessed (for example species expectedness when no
// location is configured) are reported with StatusUnknown and are excluded from
// the verdict rather than counted against the detection, so the software never
// claims a detection is unusual when it simply could not check.
package idcheck

// Verdict values describe the overall level of support for a detection's
// identification. They map to the user-facing "Strong support", "Mixed
// evidence", and "Weak support" wording in the frontend.
const (
	// VerdictStrong means the assessable signals agree and none raise a concern.
	VerdictStrong = "strong"
	// VerdictMixed means the signals are inconclusive or partly conflicting.
	VerdictMixed = "mixed"
	// VerdictWeak means the concerns outweigh the support and the detection
	// warrants a manual review.
	VerdictWeak = "weak"
)

// Signal status values. Only StatusPass and StatusWarn influence the verdict;
// StatusNeutral is informational and StatusUnknown marks a signal that could not
// be assessed.
const (
	// StatusPass is a positive signal that supports the identification.
	StatusPass = "pass"
	// StatusWarn is a negative signal that counts against the identification.
	StatusWarn = "warn"
	// StatusNeutral is a middling signal shown for context but not scored.
	StatusNeutral = "neutral"
	// StatusUnknown marks a signal that could not be assessed and is excluded
	// from the verdict entirely.
	StatusUnknown = "unknown"
)

// Signal codes identify each assessed aspect of a detection. The frontend maps
// (code, status) pairs to localized, plain-language sentences.
const (
	// SignalSoundClarity reflects how confident and clear the detection was.
	SignalSoundClarity = "sound_clarity"
	// SignalExpectedHere reflects whether the species is expected near the user
	// at the time of the detection (range/occurrence model).
	SignalExpectedHere = "expected_here"
	// SignalHeardOften reflects how many times the species was heard that day.
	SignalHeardOften = "heard_often"
	// SignalRecordingQuality is emitted only when a quality filter flagged the
	// detection as unlikely.
	SignalRecordingQuality = "recording_quality"
)

// Thresholds for classifying individual signals. They are named constants (no
// magic numbers) and are the single place to tune the verdict's behavior.
const (
	// clearConfidence is the minimum confidence (0..1) for a detection to count
	// as clearly and confidently heard.
	clearConfidence = 0.80
	// faintConfidence is the confidence below which a detection is treated as
	// faint or uncertain.
	faintConfidence = 0.55

	// expectedOccurrence is the minimum range-model occurrence probability
	// (0..1) for a species to count as expected in the area now.
	expectedOccurrence = 0.30
	// uncommonOccurrence is the occurrence probability above which a species is
	// merely uncommon (context) rather than unusual (a concern).
	uncommonOccurrence = 0.05

	// heardSeveralTimes is the daily detection count at or above which a species
	// counts as heard several times.
	heardSeveralTimes = 3
	// heardOnce is the daily detection count at or below which a species counts
	// as heard only once.
	heardOnce = 1

	// minPassesForStrong is the minimum number of positive signals required for
	// a strong verdict (in addition to there being no concerns).
	minPassesForStrong = 2
)

// Inputs carries the raw, already-gathered signal values for a single
// detection. Callers populate it from the datastore note, the range filter, and
// the current settings.
type Inputs struct {
	// Confidence is the detection's classifier confidence in the range 0..1.
	Confidence float64
	// LocationConfigured is true when the user has configured a location, which
	// is a precondition for assessing species expectedness.
	LocationConfigured bool
	// Occurrence is the range-model occurrence probability (0..1) for the
	// species at the detection's time and the configured location. It is only
	// meaningful when LocationConfigured is true.
	Occurrence float64
	// Included is true when the species passed the range filter's inclusion
	// threshold, i.e. the station already considers it expected here. It acts as
	// a tiebreaker so user-included species are never reported as unusual.
	Included bool
	// DailyCount is how many times the species was detected on the detection's
	// date, including this detection (so it is always at least 1).
	DailyCount int
	// FlaggedUnlikely is true when a quality filter (for example the ultrasonic
	// range filter) marked the detection as unlikely.
	FlaggedUnlikely bool
}

// Signal is a single assessed aspect of a detection: a stable code and a status.
type Signal struct {
	// Code identifies the aspect assessed (one of the Signal* constants).
	Code string `json:"code"`
	// Status is one of the Status* constants.
	Status string `json:"status"`
}

// Result is the outcome of an identification check: the overall verdict plus the
// ordered signals that produced it.
type Result struct {
	// Verdict is one of the Verdict* constants.
	Verdict string `json:"verdict"`
	// Signals are the assessed signals, in a stable display order.
	Signals []Signal `json:"signals"`
}

// Evaluate computes the identification-check Result for a detection from its
// gathered Inputs. It is pure: given the same Inputs it always returns the same
// Result.
func Evaluate(in Inputs) Result {
	signals := make([]Signal, 0, 4)
	signals = append(signals,
		Signal{Code: SignalSoundClarity, Status: clarityStatus(in.Confidence)},
		Signal{Code: SignalExpectedHere, Status: expectedStatus(in)},
		Signal{Code: SignalHeardOften, Status: heardStatus(in.DailyCount)},
	)
	// The recording-quality signal is only meaningful as a concern; a detection
	// that was not flagged says nothing about positive quality, so it is omitted
	// rather than shown as a pass.
	if in.FlaggedUnlikely {
		signals = append(signals, Signal{Code: SignalRecordingQuality, Status: StatusWarn})
	}
	return Result{Verdict: aggregate(signals), Signals: signals}
}

// clarityStatus classifies the detection confidence into a clarity signal.
func clarityStatus(confidence float64) string {
	switch {
	case confidence >= clearConfidence:
		return StatusPass
	case confidence < faintConfidence:
		return StatusWarn
	default:
		return StatusNeutral
	}
}

// expectedStatus classifies whether the species is expected in the area now.
// When expectedness cannot be assessed — no configured location, or no range
// coverage for the species (occurrence is zero and it is not in the include
// set) — it returns StatusUnknown so the signal is excluded from the verdict.
func expectedStatus(in Inputs) string {
	if !in.LocationConfigured {
		return StatusUnknown
	}
	switch {
	case in.Occurrence >= expectedOccurrence:
		return StatusPass
	case in.Included:
		// The station already treats this species as expected here even though
		// the occurrence band is low; report context, not a concern.
		return StatusNeutral
	case in.Occurrence >= uncommonOccurrence:
		return StatusNeutral
	case in.Occurrence > 0:
		return StatusWarn
	default:
		// Occurrence is zero and the species is not in the include set: this is
		// ambiguous (no range-model coverage vs. genuinely absent), so it cannot
		// be assessed honestly.
		return StatusUnknown
	}
}

// heardStatus classifies the daily detection count into a repetition signal.
func heardStatus(dailyCount int) string {
	switch {
	case dailyCount >= heardSeveralTimes:
		return StatusPass
	case dailyCount <= heardOnce:
		return StatusWarn
	default:
		return StatusNeutral
	}
}

// aggregate reduces the assessed signals to an overall verdict. Only pass and
// warn statuses are counted; neutral and unknown are ignored. A strong verdict
// requires at least minPassesForStrong positive signals and no concerns; a weak
// verdict requires the concerns to outnumber the positives; everything else is
// mixed.
func aggregate(signals []Signal) string {
	passes, warns := 0, 0
	for _, s := range signals {
		switch s.Status {
		case StatusPass:
			passes++
		case StatusWarn:
			warns++
		case StatusNeutral, StatusUnknown:
			// Not scored.
		}
	}
	switch {
	case warns == 0 && passes >= minPassesForStrong:
		return VerdictStrong
	case warns > passes:
		return VerdictWeak
	default:
		return VerdictMixed
	}
}
