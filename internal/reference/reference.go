// Package reference finds trustworthy reference recordings ("trusted examples")
// for a detected species from an external catalog (Xeno-canto), so the UI can
// play a known-good example beside what the microphone heard.
//
// The package is written for testability: the Xeno-canto client takes an
// injected HTTP doer, and the selection logic (Rank) is a pure function, so the
// parsing and the biological-plausibility-first ranking can be unit-tested with
// canned responses and no network.
//
// Ranking deliberately establishes biological plausibility first (same region,
// subspecies, season) and only then prefers clearer audio. It never lets raw
// acoustic similarity dominate: searching a huge catalog for whatever happens to
// resemble a local noise can manufacture a convincing match for a wrong
// identification. Similarity, when it is added later, must operate only within
// an already-plausible pool.
package reference

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/errors"
)

// ProviderXenoCanto is the source-provider identifier for Xeno-canto.
const ProviderXenoCanto = "xeno-canto"

// Sentinel errors returned by the package.
var (
	// ErrProviderNotConfigured is returned when the reference provider is
	// disabled or missing required configuration (e.g. an API key). It is a
	// normal state, not a failure.
	ErrProviderNotConfigured = errors.Newf("reference provider not configured").
					Component("reference").
					Category(errors.CategoryConfiguration).
					Build()

	// ErrNoRecordings is returned when a search succeeds but yields no usable
	// reference recording for the species.
	ErrNoRecordings = errors.Newf("no reference recordings found").
			Component("reference").
			Category(errors.CategoryNotFound).
			Build()
)

// Recording is the provider-neutral metadata for a single reference recording.
// It carries everything the UI needs to play the example and to attribute it
// correctly, plus the fields the ranking uses.
type Recording struct {
	ID             string   `json:"id"`             // provider catalog id (e.g. Xeno-canto "XC12345" without the XC prefix)
	ScientificName string   `json:"scientificName"` // "Genus species"
	Subspecies     string   `json:"subspecies,omitempty"`
	CommonName     string   `json:"commonName,omitempty"`
	Recordist      string   `json:"recordist,omitempty"` // author, for attribution
	Country        string   `json:"country,omitempty"`
	Location       string   `json:"location,omitempty"`
	CallType       string   `json:"callType,omitempty"` // vocalization type as labelled by the recordist
	PageURL        string   `json:"pageUrl,omitempty"`  // human-facing source page
	AudioURL       string   `json:"audioUrl,omitempty"` // downloadable audio
	FileName       string   `json:"fileName,omitempty"`
	LicenseName    string   `json:"licenseName,omitempty"`
	LicenseURL     string   `json:"licenseUrl,omitempty"`
	Quality        string   `json:"quality,omitempty"`    // "A" (best) .. "E" (worst), or "" (unrated)
	Length         string   `json:"length,omitempty"`     // mm:ss
	Background     []string `json:"background,omitempty"` // other species audible in the background
	Date           string   `json:"date,omitempty"`       // recording date "YYYY-MM-DD"
	SourceProvider string   `json:"sourceProvider"`       // ProviderXenoCanto
}

// Preferences expresses the biological context used to prefer some recordings
// over others. All fields are optional; an empty field contributes no
// preference.
type Preferences struct {
	Country    string // prefer recordings from the same country/region
	Subspecies string // prefer recordings of the same subspecies, when known
	Month      int    // 1-12: prefer recordings from the same season; 0 = unknown
}

// Ranking weights. Biological plausibility (region, subspecies, season) ranks
// above audio quality, which ranks above a clean foreground. Named constants,
// no magic numbers.
const (
	weightCountry    = 1000
	weightSubspecies = 500
	weightSeason     = 250
	weightQualityMul = 10

	qualityBest  = 4 // "A"
	seasonWindow = 1 // months of slack around the target month (inclusive)
	monthsInYear = 12
)

// Rank filters out unusable recordings (no audio or no license) and returns the
// remainder ordered best-first for the given preferences. It is a pure,
// deterministic function: a stable sort preserves the provider's original order
// among equally-scored recordings.
func Rank(recordings []Recording, prefs Preferences) []Recording {
	usable := make([]Recording, 0, len(recordings))
	for i := range recordings {
		r := &recordings[i]
		if r.AudioURL == "" || !LicenseAllowed(r.LicenseURL) {
			continue
		}
		usable = append(usable, *r)
	}

	// Score once, then stable-sort an index permutation so equally-scored
	// recordings keep the provider's original order.
	scores := make([]int, len(usable))
	order := make([]int, len(usable))
	for i := range usable {
		scores[i] = score(&usable[i], prefs)
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })

	ranked := make([]Recording, len(order))
	for i, idx := range order {
		ranked[i] = usable[idx]
	}
	return ranked
}

// SelectBest returns the single best reference recording for the preferences,
// or ErrNoRecordings when none are usable.
func SelectBest(recordings []Recording, prefs Preferences) (Recording, error) {
	ranked := Rank(recordings, prefs)
	if len(ranked) == 0 {
		return Recording{}, ErrNoRecordings
	}
	return ranked[0], nil
}

// score computes a recording's rank score for the given preferences. Higher is
// better.
func score(r *Recording, prefs Preferences) int {
	s := 0
	if prefs.Country != "" && strings.EqualFold(r.Country, prefs.Country) {
		s += weightCountry
	}
	if prefs.Subspecies != "" && r.Subspecies != "" && strings.EqualFold(r.Subspecies, prefs.Subspecies) {
		s += weightSubspecies
	}
	if prefs.Month != 0 && seasonMatches(r.Date, prefs.Month) {
		s += weightSeason
	}
	s += qualityRank(r.Quality) * weightQualityMul
	// Fewer background species means a cleaner foreground example.
	s -= len(r.Background)
	return s
}

// qualityRank maps a Xeno-canto quality letter to a numeric rank: "A" (best) is
// 4 down to "E" which is 0. Unrated recordings rank as 0.
func qualityRank(quality string) int {
	switch strings.ToUpper(strings.TrimSpace(quality)) {
	case "A":
		return qualityBest
	case "B":
		return 3
	case "C":
		return 2
	case "D":
		return 1
	default:
		return 0
	}
}

// seasonMatches reports whether a recording's date falls within seasonWindow
// months of the target month, treating the year as circular (so December and
// January are adjacent).
func seasonMatches(date string, targetMonth int) bool {
	month := monthOf(date)
	if month == 0 {
		return false
	}
	diff := abs(month - targetMonth)
	if diff > monthsInYear/2 {
		diff = monthsInYear - diff
	}
	return diff <= seasonWindow
}

// monthOf extracts the 1-12 month from a "YYYY-MM-DD" date, or 0 if it cannot be
// parsed.
func monthOf(date string) int {
	if t, err := time.Parse(time.DateOnly, date); err == nil {
		return int(t.Month())
	}
	// Tolerate a bare "YYYY-MM" or malformed day by parsing the middle field.
	parts := strings.Split(date, "-")
	if len(parts) >= 2 {
		if m, err := strconv.Atoi(parts[1]); err == nil && m >= 1 && m <= monthsInYear {
			return m
		}
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// LicenseAllowed reports whether a recording may be reused (played and
// attributed) based on its license URL. Reference catalogs like Xeno-canto use
// Creative Commons licenses (including CC0 / public domain), all of which permit
// sharing with attribution, so a recognized Creative Commons URL is allowed and
// anything else (or a missing license) is not.
func LicenseAllowed(licenseURL string) bool {
	return strings.Contains(strings.ToLower(licenseURL), "creativecommons.org")
}

// ParseLicense derives a human-readable license name and a normalized https URL
// from a raw Creative Commons license URL. Xeno-canto returns protocol-relative
// URLs like "//creativecommons.org/licenses/by-nc-sa/4.0/".
func ParseLicense(raw string) (name, url string) {
	if raw == "" {
		return "", ""
	}
	url = raw
	if strings.HasPrefix(url, "//") {
		url = "https:" + url
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "publicdomain/zero"):
		return "CC0 1.0", url
	case strings.Contains(lower, "publicdomain/mark"):
		return "Public Domain Mark 1.0", url
	}
	_, after, found := strings.Cut(lower, "/licenses/")
	if !found {
		return "", url
	}
	seg := strings.Trim(after, "/")
	if seg == "" {
		return "", url
	}
	parts := strings.Split(seg, "/")
	name = "CC " + strings.ToUpper(parts[0])
	if len(parts) > 1 && parts[1] != "" {
		name += " " + parts[1]
	}
	return name, url
}
