package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// AILab Tools -- one adapter, two arms.
//
//	ailab-pro   Skin Analyze Pro   70 credits
//	ailab-basic Skin Analyze       15 credits   4.7x cheaper
//
// PRICE DEPENDS ON THE CREDIT BUNDLE, and that turns out to matter to a gate:
//
//	bundle          $/credit   pro/call   basic/call
//	2,000    ($6)    0.0030     0.2100      0.0450   <- entry tier
//	10,000   ($30)   0.0030     0.2100      0.0450
//	110,000  ($300)  0.0027     0.1890      0.0405   <- the rate below
//	1,000,000($2500) 0.0025     0.1750      0.0375
//
// The constants below use the $300 tier, because the cost gate models the
// PRODUCTION unit economics -- a live product buying in bulk, which is the
// scenario NFR-7 is about.
//
// The finding that falls out of this: at the entry tier Pro costs $0.21 and
// FAILS the $0.20/scan gate. Pro is only affordable at all if you commit $300
// up front. Basic clears the gate at every tier by a factor of four. That is a
// real constraint on the paid tier and belongs in ADR-003, not a footnote --
// it means "can we afford Pro" is partly a cash-flow question, not just a
// per-call one.
//
// Both arms run so the spike can answer the question that decides the paid
// tier's margin: does Basic cover FR-4's six concerns, or is Pro's price the
// price of the product? Vendor descriptions of the two overlap heavily and
// settle nothing -- only the same faces through both endpoints will.
type AILab struct {
	// wantMaps requests the heatmap overlays (FR-4). Off unless asked for.
	wantMaps   bool
	key        string
	hc         *http.Client
	name       string
	endpoint   string
	usd        float64
	fields     []issueMapping
	skinAgePth string // "" when the endpoint has no skin age (FR-5)
}

const (
	ailabProEndpoint   = "https://www.ailabapi.com/api/portrait/analysis/skin-analysis-pro"
	ailabBasicEndpoint = "https://www.ailabapi.com/api/portrait/analysis/skin-analysis"
)

func NewAILabPro(key string) *AILab {
	return &AILab{
		key: key, hc: &http.Client{Timeout: 60 * time.Second},
		name: "ailab-pro", endpoint: ailabProEndpoint, usd: 0.189,
		fields: ailabProFields, skinAgePth: "result.skin_age.value",
	}
}

func NewAILabBasic(key string) *AILab {
	return &AILab{
		key: key, hc: &http.Client{Timeout: 60 * time.Second},
		name: "ailab-basic", endpoint: ailabBasicEndpoint, usd: 0.0405,
		fields: ailabBasicFields, skinAgePth: "",
	}
}

func (a *AILab) Name() string        { return a.name }
func (a *AILab) Configured() bool    { return a.key != "" }
func (a *AILab) USDPerCall() float64 { return a.usd }

// mappingKind describes what a vendor field actually holds. This matters far
// more than it looks: a 0-100 score supports the four-level severity scale the
// domain model requires, a 0/1 presence flag supports only two, and neither
// fact is visible from a price list.
type mappingKind int

const (
	kindScore  mappingKind = iota // 0-100 -> four severity levels
	kindBinary                    // 0/1 presence -> two levels only
	kindCount                     // length of a detection array -> binned to four
)

type issueMapping struct {
	Issue IssueType
	// Paths are tried in order; the first that resolves wins. The vendor
	// documents field NAMES (`acne_score`) but not their nesting, so each
	// mapping carries the plausible parents rather than betting on one and
	// silently reporting nothing.
	Paths    []string
	MaskPath string
	Kind     mappingKind
	Note     string
}

// resolve returns the first path that produced a value, so the run can report
// which shape the vendor actually used.
func (m issueMapping) resolve(d doc) (float64, string, bool) {
	for _, p := range m.Paths {
		if v, ok := d.num(p); ok {
			return v, p, true
		}
	}
	return 0, "", false
}

func (m issueMapping) resolveCount(d doc) (int, string, bool) {
	for _, p := range m.Paths {
		if n, ok := d.arrayLen(p); ok {
			return n, p, true
		}
	}
	return 0, "", false
}

// ailabProFields -- field names are now from the vendor's published Degree &
// Score Reference, not guesswork. Every one is a 0-100 goodness score on the
// shared band scale in ailab_scale.go, so all six resolve to four levels.
//
// Two rows still worth arguing about in review:
//
//	redness    <- sensitivity_score   PROXY, not a synonym. "Sensitivity" is a
//	                                  vendor composite; FR-4 promises visible
//	                                  redness. If they diverge, FR-4 is wrong.
//	dark_spots <- melanin_score       See the warning below. This one may be
//	                                  the whole tone-calibration risk in a
//	                                  single field.
var ailabProFields = []issueMapping{
	{Issue: IssueAcne, Kind: kindScore, MaskPath: "result.acne.rectangle",
		Paths: scorePaths("acne_score"), Note: "direct"},

	// CORRECTED 2026-08-29, from the first real Pro response.
	//
	// This mapped sensitivity_score, flagged as a proxy. The response contains
	// BOTH fields, and on the same face they disagree at opposite ends of the
	// scale:
	//
	//	sensitivity_score  30  -> Severe
	//	red_spot_score     90  -> None
	//
	// FR-4 promises visible redness. red_spot_score names it; sensitivity is a
	// vendor composite about reactivity, which is a different thing that
	// happens to sound similar. On this evidence the proxy was not merely
	// imprecise, it was reporting Severe redness on a face the vendor's own
	// redness field calls clear -- the exact false alarm a health-adjacent app
	// cannot afford.
	//
	// No fallback to sensitivity_score deliberately. A missing red_spot_score
	// must read UNKNOWN, because silently substituting a field that disagrees
	// by 60 points is worse than admitting nothing was measured.
	{Issue: IssueRedness, Kind: kindScore,
		Paths: scorePaths("red_spot_score"),
		Note:  "direct -- replaced sensitivity_score proxy, see note above"},

	{Issue: IssueDryness, Kind: kindScore,
		Paths: scorePaths("water_score"), Note: "hydration; NOT inverted -- high score already means no dryness"},

	{Issue: IssueDarkSpots, Kind: kindScore, MaskPath: "result.brown_spot.rectangle",
		Paths: scorePaths("melanin_score"), Note: "WATCH: see melanin warning"},

	{Issue: IssueTexture, Kind: kindScore,
		Paths: scorePaths("rough_score"), Note: "surface texture"},

	{Issue: IssuePores, Kind: kindScore,
		Paths: scorePaths("pores_score"), Note: "combined; regional variants also exist"},
}

// WARNING, recorded here because it decides whether this vendor is usable.
//
// Dark spots map to `melanin_score`, and on the vendor's scale a LOW score is
// "Severe". If that field tracks melanin *quantity* rather than uneven
// *distribution*, then deeper skin scores worse by construction -- not because
// of any skin concern, but because of skin colour. For an app whose stated
// market is South Asian women, that would not be a bug to tune around; it
// would mean the vendor cannot serve the product.
//
// This is exactly what the tone gate measures, and it is now the single most
// important number the spike will produce. If `dark_spots` is the concern that
// trips the tone flag, suspect this field first.

// scorePaths returns the plausible nestings for a documented field name, most
// likely first. The vendor documents names, not paths.
func scorePaths(field string) []string {
	return []string{
		"result.score_info." + field,
		"result." + field,
		field,
	}
}

// ailabBasicFields -- HYPOTHESIS, and the spike exists to break it.
//
// The Basic endpoint is documented as detecting conditions rather than scoring
// them, so the expectation is detection arrays and 0/1 presence flags instead
// of 0-100 scores. Two consequences if that holds:
//
//   - Presence flags give TWO severity levels, not four. The domain model wants
//     None/Mild/Moderate/Severe; "there is a pore issue: yes/no" cannot fill it.
//   - Redness and dryness have no obvious source at all. Skin type is an enum
//     (oily/dry/neutral/combination), not a dryness magnitude.
//
// So the predicted result is roughly 4/6 coverage at two levels -- a FAIL on
// the coverage gate. Predicting it is not the same as knowing it: these paths
// are transcribed from documentation and unverified, and the run reports every
// path that misses rather than silently zero-filling. If Basic surprises us,
// the paid tier gets 4.7x cheaper.
var ailabBasicFields = []issueMapping{
	// CORRECTED AFTER THE FIRST REAL RUN, 2026-08-29.
	//
	// These two were written as `.rectangle`, expecting detection arrays that
	// could be counted and binned into four levels. Basic returns neither:
	// every concern arrives as {confidence, value} with value 0/1, and no
	// rectangle appears anywhere under result. The only `rectangle` key in the
	// payload is the top-level face_rectangle, which is the crop box.
	//
	// The first run therefore reported acne and dark_spots MISSING when the
	// vendor was returning both. That was a harness bug flattering the gate in
	// the wrong direction -- it made coverage look worse than reality -- and it
	// is exactly what the mapping audit exists to catch. Verified against all
	// four saved raw responses before changing anything.
	//
	// Open question, one call to settle: `return_maps=1` is currently sent only
	// for Pro. If Basic honours it and starts returning rectangles, acne becomes
	// a count and could support four levels. Until tested, assume it does not.
	{Issue: IssueAcne, Kind: kindBinary,
		Paths: []string{"result.acne.value"},
		Note:  "presence flag, two levels -- documented detection array did not appear"},
	{Issue: IssueDarkSpots, Kind: kindBinary,
		Paths: []string{"result.skin_spot.value"},
		Note:  "presence flag, two levels -- documented detection array did not appear"},
	{Issue: IssuePores, Kind: kindBinary,
		Paths: []string{"result.pores_forehead.value"}, Note: "presence flag -- forehead only, two levels"},
	{Issue: IssueTexture, Kind: kindBinary,
		Paths: []string{"result.blackhead.value"}, Note: "WEAK PROXY: blackheads are not texture"},
	{Issue: IssueDryness, Kind: kindBinary,
		Paths: []string{"result.skin_type.skin_type"}, Note: "EXPECTED TO MISS: enum, not a magnitude"},
	{Issue: IssueRedness, Kind: kindBinary,
		Paths: []string{"result.sensitivity.value"}, Note: "EXPECTED TO MISS: no redness signal documented"},
}

func (a *AILab) Analyze(ctx context.Context, jpeg []byte) (SkinReport, []byte, error) {
	if !a.Configured() {
		return SkinReport{}, nil, ErrNotConfigured
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("image", "scan.jpg")
	if err != nil {
		return SkinReport{}, nil, err
	}
	if _, err := part.Write(jpeg); err != nil {
		return SkinReport{}, nil, err
	}
	// return_maps takes NAMED IDENTIFIERS, comma-separated -- not a boolean.
	// Sending "1" failed every call with UNSUPPORTED_PARAMETER_VALUES, and the
	// bug sat undetected because Pro had never completed a call at all: the
	// first attempt died on insufficient credits, which masked it.
	//
	// The five requested here are the ones that correspond to an FR-4 concern.
	// There is deliberately no acne map because the vendor does not offer one,
	// which is consistent with acne being the concern it handles worst (R-1).
	//
	// Off by default: maps come back as base64 inside the JSON and inflate the
	// response enormously, so a run that does not need them should not pay the
	// bandwidth or the disk.
	if a.wantMaps {
		_ = w.WriteField("return_maps",
			"red_area,brown_area,water_area,rough_area,texture_enhanced_pores")
	}
	if err := w.Close(); err != nil {
		return SkinReport{}, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, &body)
	if err != nil {
		return SkinReport{}, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("ailabapi-api-key", a.key)

	resp, err := a.hc.Do(req)
	if err != nil {
		return SkinReport{}, nil, err
	}
	defer resp.Body.Close()

	// No LimitReader here, and that is deliberate now that maps exist: five
	// base64 images in one JSON body run to megabytes, and a cap would truncate
	// the response into a JSON parse error that looks nothing like its cause.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return SkinReport{}, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return SkinReport{}, raw, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(raw, 240))
	}

	rep, err := a.mapResponse(raw)
	return rep, raw, err
}

func (a *AILab) mapResponse(raw []byte) (SkinReport, error) {
	d, err := parseDoc(raw)
	if err != nil {
		return SkinReport{}, fmt.Errorf("decode: %w", err)
	}

	rep := SkinReport{SkinAge: -1}

	for _, m := range a.fields {
		iss := Issue{Type: m.Issue, Severity: SeverityUnknown, Score: -1, Confidence: -1, Levels: 0}
		hitPath := ""

		switch m.Kind {
		case kindScore:
			// Vendor score is GOODNESS on the documented bands; the canonical
			// Issue.Score is problem magnitude. Convert, never copy.
			if v, p, ok := m.resolve(d); ok {
				iss.Score = problemScore(v)
				iss.Severity = severityFromAILabScore(v)
				iss.Levels = 4
				hitPath = p
			}
		case kindCount:
			if n, p, ok := m.resolveCount(d); ok {
				iss.Score = clamp(n*8, 0, 100)
				iss.Severity = severityFromCount(n)
				iss.Levels = 4
				hitPath = p
			}
		case kindBinary:
			if v, p, ok := m.resolve(d); ok {
				if v > 0 {
					iss.Score = 100
					iss.Severity = SeverityModerate // "present" -- we cannot say how much
				} else {
					iss.Score = 0
					iss.Severity = SeverityNone
				}
				iss.Levels = 2
				hitPath = p
			}
		}

		if hitPath != "" {
			// A vendor severity label, when present, beats anything we derived.
			if s, ok := d.str(sevPath(hitPath)); ok {
				if parsed := parseVendorSeverity(s); parsed != SeverityUnknown {
					iss.Severity = parsed
					iss.Levels = 4
				}
			}
			if c, ok := d.num(confPath(hitPath)); ok {
				iss.Confidence = c
			}
		}
		if m.MaskPath != "" && d.present(m.MaskPath) {
			iss.HasMask = true
		}
		iss.SeverityS = iss.Severity.String()
		rep.Issues = append(rep.Issues, iss)
	}

	if a.skinAgePth != "" {
		if v, ok := d.num(a.skinAgePth); ok {
			rep.SkinAge = int(v)
		}
	}

	// total_score is also goodness, on a 70-100 band. See aynaDisplayScore for
	// why showing it raw as "out of 100" misrepresents the trend.
	if v, ok := firstNum(d, scorePaths("total_score")); ok {
		rep.OverallScore = aynaDisplayScore(v)
	} else {
		rep.OverallScore = derivedOverall(rep.Issues)
	}

	return rep, nil
}

func firstNum(d doc, paths []string) (float64, bool) {
	for _, p := range paths {
		if v, ok := d.num(p); ok {
			return v, true
		}
	}
	return 0, false
}

// severityFromCount bins a detection count. Placeholder bins, like the score
// thresholds -- the real values come from the spike's own distributions and
// then a medical review (PD-2).
func severityFromCount(n int) Severity {
	switch {
	case n == 0:
		return SeverityNone
	case n <= 3:
		return SeverityMild
	case n <= 10:
		return SeverityModerate
	default:
		return SeveritySevere
	}
}

// sevPath / confPath assume the vendor nests a label and a confidence as
// SIBLINGS of the value field, whatever that field is called -- `.score` on
// Pro, `.value` or `.rectangle` on Basic. Replacing the final segment (rather
// than only the literal "score") is what makes one adapter serve both arms.
// If the guess is wrong the paths simply miss, and the run reports that.
func sevPath(p string) string  { return siblingPath(p, "severity") }
func confPath(p string) string { return siblingPath(p, "confidence") }

func parseVendorSeverity(s string) Severity {
	switch normaliseLower(s) {
	case "none", "0", "clear":
		return SeverityNone
	case "mild", "1", "light", "slight":
		return SeverityMild
	case "moderate", "2", "medium":
		return SeverityModerate
	case "severe", "3", "heavy", "serious":
		return SeveritySevere
	}
	return SeverityUnknown
}

// derivedOverall is the fallback when a vendor gives no headline number.
// Issue.Score is problem magnitude, so goodness is its complement.
//
// Flagged deliberately: FR-4 promises a 0-100 overall score, and if we compute
// it, that formula is OUR business logic and belongs in the domain layer --
// versioned, like the severity thresholds. A plain mean also implies all six
// concerns matter equally, which is itself an unexamined product claim.
func derivedOverall(issues []Issue) int {
	sum, n := 0, 0
	for _, i := range issues {
		if i.Score >= 0 {
			sum += 100 - i.Score
			n++
		}
	}
	if n == 0 {
		return -1
	}
	return sum / n
}

// MapRaw exposes the response mapping for offline replay, so a corrected
// field path can be re-applied to responses already paid for.
func (a *AILab) MapRaw(raw []byte) (SkinReport, error) { return a.mapResponse(raw) }

// WithMaps returns the arm configured to request FR-4's overlay maps.
func (a *AILab) WithMaps() *AILab {
	b := *a
	b.wantMaps = true
	return &b
}
