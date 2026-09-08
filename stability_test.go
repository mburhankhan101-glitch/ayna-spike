package main

import "testing"

func reportWithAge(age int) SkinReport {
	return SkinReport{SkinAge: age}
}

func TestSkinAgeDriftIsMeasuredInYears(t *testing.T) {
	for _, c := range []struct {
		name      string
		a, b      int
		wantGap   int
		wantKnown bool
	}{
		{"identical is zero drift", 24, 24, 0, true},
		{"direction does not matter", 24, 27, 3, true},
		{"direction does not matter, reversed", 27, 24, 3, true},

		// The rule that matters: an arm carrying no skin age (the Basic
		// endpoint has none) must report "not measurable", never "stable".
		// Scoring a missing field as a perfect zero would let an arm pass a
		// gate on a number it never produced.
		{"missing on one side is not measurable", 24, -1, 0, false},
		{"missing on both sides is not measurable", -1, -1, 0, false},
	} {
		gap, known := skinAgeDelta(reportWithAge(c.a), reportWithAge(c.b))
		if gap != c.wantGap || known != c.wantKnown {
			t.Errorf("%s: got (%d, %v), want (%d, %v)",
				c.name, gap, known, c.wantGap, c.wantKnown)
		}
	}
}

func TestProviderSkinAgeDriftTakesTheWorstCase(t *testing.T) {
	obs := []Observation{
		// Two images, two trials each. Image A is steady, image B drifts 4.
		{Provider: "x", Image: "a.jpg", Trial: 1, Report: reportWithAge(30)},
		{Provider: "x", Image: "a.jpg", Trial: 2, Report: reportWithAge(30)},
		{Provider: "x", Image: "b.jpg", Trial: 1, Report: reportWithAge(26)},
		{Provider: "x", Image: "b.jpg", Trial: 2, Report: reportWithAge(30)},
	}

	// The worst case, not the average: a user only has to be shown one wrong
	// number once to stop believing the app, and averaging would have reported
	// a comfortable 2.
	gap, known := providerSkinAgeDrift(obs)
	if !known || gap != 4 {
		t.Errorf("got (%d, %v), want (4, true)", gap, known)
	}
}

func TestArmWithoutSkinAgeReportsUnmeasuredRatherThanPassing(t *testing.T) {
	obs := []Observation{
		{Provider: "basic", Image: "a.jpg", Trial: 1, Report: reportWithAge(-1)},
		{Provider: "basic", Image: "a.jpg", Trial: 2, Report: reportWithAge(-1)},
	}
	if _, known := providerSkinAgeDrift(obs); known {
		t.Error("an arm with no skin age must not report a measurable drift")
	}
}

func TestFailedTrialsAreNeverCountedAsStable(t *testing.T) {
	// A provider that errors on one trial has not demonstrated stability; it
	// has demonstrated an error. Only successful pairs may be compared.
	obs := []Observation{
		{Provider: "x", Image: "a.jpg", Trial: 1, Report: reportWithAge(30)},
		{Provider: "x", Image: "a.jpg", Trial: 2, Report: reportWithAge(99),
			Err: ErrNotConfigured},
	}
	if _, known := providerSkinAgeDrift(obs); known {
		t.Error("a failed trial must not form a comparable pair")
	}
	if _, known := providerStability(obs); known {
		t.Error("score stability must not be claimed from a single good trial")
	}
}
