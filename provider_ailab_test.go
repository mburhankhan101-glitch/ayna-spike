package main

import "testing"

// Pro response on the vendor's documented scale: 0-100 where HIGHER IS BETTER.
//
//	[90,100] None   [70,89] Mild   [50,69] Moderate   [30,49] Severe
//
// red_spot_score and sensitivity_score are both present, and deliberately set
// to opposite ends of the scale (91 = None, 30 = Severe). That is not padding:
// it is the regression guard for the mapping this spike got wrong. Redness must
// come from red_spot_score. If anyone reverts it to the sensitivity proxy, this
// face flips from clear to Severe and the assertion below fails loudly, which
// is exactly what should happen -- the real Pro response showed the same 60-
// point divergence on a real face.
const proSample = `{
  "result": {
    "score_info": {
      "total_score":       85,
      "acne_score":        45,
      "water_score":       95,
      "melanin_score":     72,
      "rough_score":       60,
      "pores_score":       88,
      "red_spot_score":    91,
      "sensitivity_score": 30
    }
  }
}`

func TestProScaleIsGoodnessNotSeverity(t *testing.T) {
	rep, err := NewAILabPro("k").mapResponse([]byte(proSample))
	if err != nil {
		t.Fatalf("map: %v", err)
	}

	// The bug this test exists to prevent: acne_score 45 is SEVERE on the
	// vendor's scale. Reading it as a problem magnitude would call it "mild".
	acne, _ := rep.issueByType(IssueAcne)
	if acne.Severity != SeveritySevere {
		t.Errorf("acne(45) = %v, want severe -- low vendor score means bad skin", acne.Severity)
	}
	if acne.Score != 55 {
		t.Errorf("acne problem magnitude = %d, want 55 (100-45)", acne.Score)
	}

	// water_score 95 = well hydrated = NO dryness. The old code inverted this
	// field explicitly, which under the correct scale would invert it twice.
	dry, _ := rep.issueByType(IssueDryness)
	if dry.Severity != SeverityNone {
		t.Errorf("dryness from water_score(95) = %v, want none -- no second inversion", dry.Severity)
	}

	for _, c := range []struct {
		it   IssueType
		want Severity
	}{
		{IssueDarkSpots, SeverityMild},   // melanin 72
		{IssueTexture, SeverityModerate}, // rough 60
		{IssuePores, SeverityMild},       // pores 88
		{IssueRedness, SeverityNone},     // red_spot 91 -- NOT sensitivity 30
	} {
		got, _ := rep.issueByType(c.it)
		if got.Severity != c.want {
			t.Errorf("%s = %v, want %v", c.it, got.Severity, c.want)
		}
	}

	if n, gaps := rep.Coverage(); n != 6 {
		t.Errorf("coverage = %d/6, missing %v", n, gaps)
	}
}

func TestBandBoundariesMatchThePublishedTable(t *testing.T) {
	for _, c := range []struct {
		score float64
		want  Severity
	}{
		{100, SeverityNone}, {90, SeverityNone},
		{89, SeverityMild}, {70, SeverityMild},
		{69, SeverityModerate}, {50, SeverityModerate},
		{49, SeveritySevere}, {30, SeveritySevere},
	} {
		if got := severityFromAILabScore(c.score); got != c.want {
			t.Errorf("severityFromAILabScore(%v) = %v, want %v", c.score, got, c.want)
		}
	}
}

// total_score is documented as running 70-100, not 0-100. Showing it raw would
// squeeze every user into the top third of the scale and flatten FR-8's trend.
func TestDisplayScoreStretchesTheVendorRange(t *testing.T) {
	for _, c := range []struct {
		vendor float64
		want   int
	}{
		{70, 0}, {85, 50}, {100, 100}, {60, 0},
	} {
		if got := aynaDisplayScore(c.vendor); got != c.want {
			t.Errorf("aynaDisplayScore(%v) = %d, want %d", c.vendor, got, c.want)
		}
	}
}

// A real Basic response, trimmed. Taken verbatim from
// results/raw/ailab-basic__face1__t1.json on the 2026-08-29 run -- not from
// the documentation, which described detection arrays that never arrived.
//
// Every concern is {confidence, value} with a 0/1 value. There is no rectangle
// anywhere under result; the only rectangle in the payload is the top-level
// face_rectangle crop box.
const basicSample = `{
  "face_rectangle": {"width":542,"top":730,"height":543,"left":197},
  "result": {
    "acne":       {"confidence": 0.9559629, "value": 1},
    "skin_spot":  {"confidence": 0.2719264, "value": 0},
    "pores_forehead": {"confidence": 0.9993787, "value": 0},
    "blackhead":  {"confidence": 0.0000145, "value": 0},
    "skin_type":  {"skin_type": 2}
  }
}`

func TestBasicMappingResolvesWhatItCanAndReportsWhatItCannot(t *testing.T) {
	rep, err := NewAILabBasic("k").mapResponse([]byte(basicSample))
	if err != nil {
		t.Fatalf("map: %v", err)
	}

	// The finding that decides the arm: every concern Basic answers is a
	// presence flag, so none of them can carry more than two levels. The domain
	// model needs four. No price makes that good enough.
	for _, tc := range []struct {
		issue IssueType
		want  Severity
	}{
		// Present maps to Moderate, not Severe: a flag says something is there,
		// never how much, and the midpoint is the only honest place to put it.
		{IssueAcne, SeverityModerate},
		{IssueDarkSpots, SeverityNone}, // value 0 -> absent
		{IssuePores, SeverityNone},
	} {
		got, _ := rep.issueByType(tc.issue)
		if got.Severity != tc.want {
			t.Errorf("%s = %v, want %v", tc.issue, got.Severity, tc.want)
		}
		if got.Levels != 2 {
			t.Errorf("%s levels = %d, want 2 (presence flag)", tc.issue, got.Levels)
		}
	}

	acne, _ := rep.issueByType(IssueAcne)
	if acne.Confidence <= 0 {
		t.Errorf("acne confidence not picked up: %v", acne.Confidence)
	}

	// Redness has no source at all: the sensitivity field the mapping expected
	// is absent from every real response. It must read UNKNOWN, never "none" --
	// telling a user they have no redness when nothing ever looked is the worst
	// failure this product can produce.
	red, _ := rep.issueByType(IssueRedness)
	if red.Severity != SeverityUnknown {
		t.Errorf("redness = %v, want unknown", red.Severity)
	}

	// Five of six resolve once the mapping matches reality -- which clears the
	// coverage gate. The arm still fails on levels, and that distinction is the
	// whole point of measuring them separately.
	if got, gaps := rep.Coverage(); got != 5 {
		t.Errorf("coverage = %d/6, want 5/6 (missing %v)", got, gaps)
	}
	if rep.SkinAge != -1 {
		t.Errorf("skinAge = %d, want -1 (Basic has no skin age)", rep.SkinAge)
	}
}
