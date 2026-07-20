package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		wantName string
		wantURL  string
	}{
		{"by-nc-sa protocol relative", "//creativecommons.org/licenses/by-nc-sa/4.0/", "CC BY-NC-SA 4.0", "https://creativecommons.org/licenses/by-nc-sa/4.0/"},
		{"by", "https://creativecommons.org/licenses/by/3.0/", "CC BY 3.0", "https://creativecommons.org/licenses/by/3.0/"},
		{"cc0", "//creativecommons.org/publicdomain/zero/1.0/", "CC0 1.0", "https://creativecommons.org/publicdomain/zero/1.0/"},
		{"public domain mark", "//creativecommons.org/publicdomain/mark/1.0/", "Public Domain Mark 1.0", "https://creativecommons.org/publicdomain/mark/1.0/"},
		{"empty", "", "", ""},
		{"unknown host keeps url", "//example.com/whatever/", "", "https://example.com/whatever/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotURL := ParseLicense(tt.raw)
			assert.Equal(t, tt.wantName, gotName)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}

func TestLicenseAllowed(t *testing.T) {
	t.Parallel()
	assert.True(t, LicenseAllowed("https://creativecommons.org/licenses/by/4.0/"))
	assert.True(t, LicenseAllowed("//creativecommons.org/publicdomain/zero/1.0/"))
	assert.False(t, LicenseAllowed(""))
	assert.False(t, LicenseAllowed("https://example.com/all-rights-reserved"))
}

func TestQualityRank(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 4, qualityRank("A"))
	assert.Equal(t, 4, qualityRank(" a "))
	assert.Equal(t, 2, qualityRank("C"))
	assert.Equal(t, 0, qualityRank("E"))
	assert.Equal(t, 0, qualityRank(""))
	assert.Equal(t, 0, qualityRank("no score"))
}

func TestSeasonMatches(t *testing.T) {
	t.Parallel()
	assert.True(t, seasonMatches("2025-07-15", 7))  // same month
	assert.True(t, seasonMatches("2025-06-01", 7))  // within window
	assert.True(t, seasonMatches("2025-12-20", 1))  // circular Dec/Jan
	assert.False(t, seasonMatches("2025-03-01", 7)) // out of window
	assert.False(t, seasonMatches("", 7))           // unparseable
	assert.False(t, seasonMatches("2025-07-15", 0)) // no target
}

// licensedCC is a valid CC license URL usable in test recordings.
const licensedCC = "//creativecommons.org/licenses/by-nc-sa/4.0/"

func rec(id, country, ssp, quality, date string, background ...string) Recording {
	return Recording{
		ID:         id,
		AudioURL:   "https://example.test/" + id + ".mp3",
		LicenseURL: licensedCC,
		Country:    country,
		Subspecies: ssp,
		Quality:    quality,
		Date:       date,
		Background: background,
	}
}

func TestRank_FiltersUnusable(t *testing.T) {
	t.Parallel()

	recordings := []Recording{
		{ID: "no-audio", LicenseURL: licensedCC},                   // missing audio
		{ID: "no-license", AudioURL: "https://example.test/x.mp3"}, // missing license
		{ID: "bad-license", AudioURL: "https://example.test/y.mp3", LicenseURL: "https://example.com/arr"},
		rec("ok", "US", "", "A", "2025-07-01"),
	}

	ranked := Rank(recordings, Preferences{})
	require.Len(t, ranked, 1)
	assert.Equal(t, "ok", ranked[0].ID)
}

func TestRank_BiologicalPlausibilityBeatsQuality(t *testing.T) {
	t.Parallel()

	// A pristine (quality A) recording from the wrong region must rank BELOW a
	// mediocre (quality C) recording from the user's region: plausibility first.
	wrongRegionHighQuality := rec("far-A", "BR", "", "A", "2025-07-01")
	sameRegionLowQuality := rec("near-C", "US", "", "C", "2025-07-01")

	ranked := Rank(
		[]Recording{wrongRegionHighQuality, sameRegionLowQuality},
		Preferences{Country: "US"},
	)
	require.Len(t, ranked, 2)
	assert.Equal(t, "near-C", ranked[0].ID, "same-region recording must win over higher quality elsewhere")
}

func TestRank_PrefersSubspeciesThenSeasonThenQuality(t *testing.T) {
	t.Parallel()

	base := rec("base", "US", "", "B", "2025-01-01")
	sameSsp := rec("ssp", "US", "migratorius", "B", "2025-01-01")
	sameSeason := rec("season", "US", "", "B", "2025-07-10")
	higherQuality := rec("hiq", "US", "", "A", "2025-01-01")

	ranked := Rank(
		[]Recording{base, higherQuality, sameSeason, sameSsp},
		Preferences{Country: "US", Subspecies: "migratorius", Month: 7},
	)

	// Subspecies match (weight 500) > season match (250) > quality bump (10).
	require.Len(t, ranked, 4)
	assert.Equal(t, "ssp", ranked[0].ID)
	assert.Equal(t, "season", ranked[1].ID)
}

func TestRank_CleanerForegroundBreaksTies(t *testing.T) {
	t.Parallel()

	noisy := rec("noisy", "US", "", "A", "2025-07-01", "Turdus merula", "Parus major")
	clean := rec("clean", "US", "", "A", "2025-07-01")

	ranked := Rank([]Recording{noisy, clean}, Preferences{Country: "US"})
	require.Len(t, ranked, 2)
	assert.Equal(t, "clean", ranked[0].ID, "fewer background species should win an otherwise-equal tie")
}

func TestSelectBest(t *testing.T) {
	t.Parallel()

	best, err := SelectBest([]Recording{rec("a", "US", "", "A", "2025-07-01")}, Preferences{})
	require.NoError(t, err)
	assert.Equal(t, "a", best.ID)

	_, err = SelectBest(nil, Preferences{})
	require.ErrorIs(t, err, ErrNoRecordings)

	_, err = SelectBest([]Recording{{ID: "no-audio", LicenseURL: licensedCC}}, Preferences{})
	require.ErrorIs(t, err, ErrNoRecordings)
}
