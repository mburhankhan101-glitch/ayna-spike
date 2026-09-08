package main

// AILab's documented score scale -- and the trap in it.
//
// From the vendor's "Degree & Score Reference": every condition score runs
// 30-100 and **higher means BETTER skin**:
//
//	[90,100] None      [70,89] Mild      [50,69] Moderate      [30,49] Severe
//
// That is the opposite of the intuitive reading, and it is the opposite of
// what this harness originally assumed. An unverified mapping had these
// backwards, which would have reported healthy skin as severe and severe skin
// as healthy -- on every concern, for every user, silently. Nothing in the
// response shape would have revealed it; only the documentation does.
//
// Three consequences worth stating, because each is a decision:
//
//  1. NO per-field inversion is needed any more. `water_score` (hydration) high
//     already means "no dryness problem" under this scale, so running it
//     through the same bands is correct. The old "invert moisture" step would
//     now be a SECOND inversion and wrong again.
//
//  2. The canonical `Issue.Score` in this harness means MAGNITUDE OF PROBLEM
//     (a wider bar = worse), because that is what the report screen renders.
//     Vendor score is goodness. They are converted, not copied.
//
//  3. The documented bands are the closest thing to a real starting point for
//     PD-2's referral thresholds. They are a vendor's opinion, not a clinical
//     standard -- but a documented opinion beats a number an engineer invented.
const (
	ailabBandNone     = 90 // >= this is None
	ailabBandMild     = 70 // >= this is Mild
	ailabBandModerate = 50 // >= this is Moderate
	// below ailabBandModerate is Severe; the documented floor is 30
)

// severityFromAILabScore maps a vendor goodness score to the domain enum.
func severityFromAILabScore(vendorScore float64) Severity {
	switch {
	case vendorScore < 0 || vendorScore > 100:
		return SeverityUnknown
	case vendorScore >= ailabBandNone:
		return SeverityNone
	case vendorScore >= ailabBandMild:
		return SeverityMild
	case vendorScore >= ailabBandModerate:
		return SeverityModerate
	default:
		return SeveritySevere
	}
}

// problemScore converts vendor goodness into the harness's problem magnitude.
func problemScore(vendorScore float64) int {
	return clamp(100-int(vendorScore), 0, 100)
}

// aynaDisplayScore rescales the vendor's overall total_score for display.
//
// The vendor documents total_score bands starting at 70 ([70,75] = "Poor"),
// so the usable range is roughly 70-100, not 0-100. FR-4 promises the user a
// score "0-100". Showing a raw 70-100 number as if it were out of 100 would
// compress every user into the top third of the scale and make the trend in
// FR-8 look flatter than it is -- a 4-point gain reads as noise on a 0-100
// axis and as real movement on a 70-100 one.
//
// This stretches [70,100] onto [0,100]. It is a PRODUCT decision disguised as
// arithmetic: it makes low scores look much worse than the vendor intends, so
// it needs a deliberate call before shipping, not a default. Recorded here so
// the choice is visible rather than buried in a UI formatter.
func aynaDisplayScore(vendorTotal float64) int {
	const floor = 70.0
	if vendorTotal <= floor {
		return 0
	}
	return clamp(int((vendorTotal-floor)/(100.0-floor)*100.0), 0, 100)
}
