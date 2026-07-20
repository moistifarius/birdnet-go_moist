package idcheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalStatus returns the status of the signal with the given code, or empty
// string if the signal is absent from the result.
func signalStatus(r Result, code string) string {
	for _, s := range r.Signals {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

func TestEvaluate_Verdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Inputs
		want string
	}{
		{
			name: "all positive signals yield strong",
			in: Inputs{
				Confidence:         0.92,
				LocationConfigured: true,
				Occurrence:         0.7,
				DailyCount:         5,
			},
			want: VerdictStrong,
		},
		{
			name: "two passes and an unknown still strong",
			in: Inputs{
				Confidence:         0.9,   // pass
				LocationConfigured: false, // expected_here unknown
				DailyCount:         4,     // pass
			},
			want: VerdictStrong,
		},
		{
			name: "two passes with one warn is mixed",
			in: Inputs{
				Confidence:         0.9, // pass
				LocationConfigured: true,
				Occurrence:         0.6, // pass
				DailyCount:         1,   // warn
			},
			want: VerdictMixed,
		},
		{
			name: "one pass and two warns is weak",
			in: Inputs{
				Confidence:         0.9, // pass
				LocationConfigured: true,
				Occurrence:         0.01, // warn (unusual, has coverage)
				DailyCount:         1,    // warn
			},
			want: VerdictWeak,
		},
		{
			name: "single positive signal is mixed (not enough for strong)",
			in: Inputs{
				Confidence:         0.9,   // pass
				LocationConfigured: false, // unknown
				DailyCount:         2,     // neutral
			},
			want: VerdictMixed,
		},
		{
			name: "faint, unusual, once heard is weak",
			in: Inputs{
				Confidence:         0.4, // warn
				LocationConfigured: true,
				Occurrence:         0.02, // warn
				DailyCount:         1,    // warn
			},
			want: VerdictWeak,
		},
		{
			name: "flagged unlikely adds a concern that tips to mixed",
			in: Inputs{
				Confidence:         0.9, // pass
				LocationConfigured: true,
				Occurrence:         0.6,  // pass
				DailyCount:         4,    // pass
				FlaggedUnlikely:    true, // warn -> not strong anymore
			},
			want: VerdictMixed,
		},
		{
			name: "equal passes and warns is mixed",
			in: Inputs{
				Confidence:         0.9, // pass
				LocationConfigured: true,
				Occurrence:         0.6,  // pass
				DailyCount:         1,    // warn
				FlaggedUnlikely:    true, // warn
			},
			want: VerdictMixed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(tt.in)
			assert.Equal(t, tt.want, got.Verdict)
		})
	}
}

func TestEvaluate_ClarityStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		confidence float64
		want       string
	}{
		{"clear at threshold", clearConfidence, StatusPass},
		{"clear above threshold", 0.95, StatusPass},
		{"neutral between bands", 0.7, StatusNeutral},
		{"faint just below band", faintConfidence - 0.01, StatusWarn},
		{"faint low", 0.1, StatusWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(Inputs{Confidence: tt.confidence})
			assert.Equal(t, tt.want, signalStatus(got, SignalSoundClarity))
		})
	}
}

func TestEvaluate_ExpectedStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Inputs
		want string
	}{
		{
			name: "unknown when location not configured",
			in:   Inputs{LocationConfigured: false, Occurrence: 0.9},
			want: StatusUnknown,
		},
		{
			name: "pass when clearly expected",
			in:   Inputs{LocationConfigured: true, Occurrence: 0.5},
			want: StatusPass,
		},
		{
			name: "neutral when included despite low occurrence",
			in:   Inputs{LocationConfigured: true, Occurrence: 0.0, Included: true},
			want: StatusNeutral,
		},
		{
			name: "neutral when uncommon",
			in:   Inputs{LocationConfigured: true, Occurrence: 0.1},
			want: StatusNeutral,
		},
		{
			name: "warn when unusual but has coverage",
			in:   Inputs{LocationConfigured: true, Occurrence: 0.01},
			want: StatusWarn,
		},
		{
			name: "unknown when no coverage and not included",
			in:   Inputs{LocationConfigured: true, Occurrence: 0.0, Included: false},
			want: StatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(tt.in)
			assert.Equal(t, tt.want, signalStatus(got, SignalExpectedHere))
		})
	}
}

func TestEvaluate_HeardStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
		want  string
	}{
		{"several", heardSeveralTimes, StatusPass},
		{"many", 12, StatusPass},
		{"a couple", 2, StatusNeutral},
		{"once", heardOnce, StatusWarn},
		{"defensive zero", 0, StatusWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(Inputs{DailyCount: tt.count})
			assert.Equal(t, tt.want, signalStatus(got, SignalHeardOften))
		})
	}
}

func TestEvaluate_RecordingQualitySignalOnlyWhenFlagged(t *testing.T) {
	t.Parallel()

	notFlagged := Evaluate(Inputs{Confidence: 0.9})
	assert.Empty(t, signalStatus(notFlagged, SignalRecordingQuality),
		"recording_quality signal must be absent when the detection is not flagged")

	flagged := Evaluate(Inputs{Confidence: 0.9, FlaggedUnlikely: true})
	assert.Equal(t, StatusWarn, signalStatus(flagged, SignalRecordingQuality))
}

func TestEvaluate_SignalsAreStableAndOrdered(t *testing.T) {
	t.Parallel()

	got := Evaluate(Inputs{
		Confidence:         0.9,
		LocationConfigured: true,
		Occurrence:         0.6,
		DailyCount:         4,
		FlaggedUnlikely:    true,
	})

	require.Len(t, got.Signals, 4)
	assert.Equal(t, SignalSoundClarity, got.Signals[0].Code)
	assert.Equal(t, SignalExpectedHere, got.Signals[1].Code)
	assert.Equal(t, SignalHeardOften, got.Signals[2].Code)
	assert.Equal(t, SignalRecordingQuality, got.Signals[3].Code)
}

func TestEvaluate_Deterministic(t *testing.T) {
	t.Parallel()

	in := Inputs{Confidence: 0.72, LocationConfigured: true, Occurrence: 0.2, DailyCount: 2}
	first := Evaluate(in)
	second := Evaluate(in)
	assert.Equal(t, first, second)
}
