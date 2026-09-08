package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Gate thresholds. These are the pass/fail line the spike is measured against,
// written down BEFORE the data arrives so the result cannot be rationalised
// afterwards. They trace directly to the NFRs.
const (
	gateMaxLatencyP95   = 8 * time.Second // NFR-2: submission -> ready report
	gateMaxStabilityGap = 8               // points; FR-8 trend lines are noise above this
	gateMaxSkinAgeDrift = 1               // YEARS, not points -- see below
	gateMinCoverage     = 5               // of 6 FR-4 concerns
	gateMaxToneGap      = 12              // points; see the calibration note below
	gateMaxUSDPerScan   = 0.20            // PAID-TIER ceiling. Derived, not guessed: at Pakistan Plus
	//                                     pricing (net $2.13/mo) a user break-evens at 11.3 scans,
	//                                     so anything past ~$0.20/call breaks the cheapest market.
	//                                     The FREE tier runs a general model and is not gated here.
)

// Why skin age gets a tighter gate than everything else.
//
// The other concerns are shown as a severity band, so a few points of drift
// disappears inside "Mild". Skin age is shown as a bare number the user
// subtracts from their own age, and the UI now celebrates the result: "2 years
// younger" gets an olive card because for a lot of users that IS the result.
//
// A 2-year drift on identical pixels turns that win into "right in step" on a
// re-scan. The more prominent the number, the less drift it can survive, and
// this one is now the most prominent thing on the report after the score.
//
// 1 year allows for rounding at a band edge. A deterministic CV model on
// identical input should return identical output, so anything above 0 is
// already worth a second look -- the report prints the real figure, not just
// pass/fail, so a "1" that is really "always exactly 1" stays visible.

func writeReport(path string, obs []Observation, providers []Provider, trials int) error {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# AI Provider Spike -- Results")
	w("")
	w("Generated %s | %d observations | %d trials per image",
		time.Now().Format("2006-01-02 15:04"), len(obs), trials)
	w("")
	w("> Gate thresholds were fixed before the run: latency p95 < %s, stability gap <= %d pts,",
		gateMaxLatencyP95, gateMaxStabilityGap)
	w("> coverage >= %d/6, tone gap <= %d pts, cost <= %s per scan.",
		gateMinCoverage, gateMaxToneGap, fmtUSD(gateMaxUSDPerScan))
	w("")

	byProvider := map[string][]Observation{}
	for _, o := range obs {
		byProvider[o.Provider] = append(byProvider[o.Provider], o)
	}

	// ---------- scorecard ----------
	w("## Scorecard")
	w("")
	w("| Provider | Calls | OK | Coverage | Severity levels | Latency p95 | Stability | $/scan | $/10k scans |")
	w("|---|---|---|---|---|---|---|---|---|")

	type card struct {
		name             string
		ok, total        int
		coverage         int
		latP95           time.Duration
		stability        int
		usd              float64
		coverageMeasured bool
		stabilityKnown   bool
	}
	var cards []card

	for _, p := range providers {
		list := byProvider[p.Name()]
		c := card{name: p.Name(), total: len(list), usd: p.USDPerCall(), coverage: -1, stability: -1}

		var lats []time.Duration
		for _, o := range list {
			if o.OK() {
				c.ok++
				lats = append(lats, o.Latency)
				if got, _ := o.Report.Coverage(); got > c.coverage {
					c.coverage = got
					c.coverageMeasured = true
				}
			}
		}
		c.latP95 = p95(lats)

		if gap, known := providerStability(list); known {
			c.stability, c.stabilityKnown = gap, true
		}
		cards = append(cards, c)

		levels, levelsKnown := providerLevels(list)
		w("| %s | %d | %d | %s | %s | %s | %s | %s | %s |",
			c.name, c.total, c.ok,
			cell(c.coverageMeasured, fmt.Sprintf("%d/6", c.coverage)),
			cell(levelsKnown, levelsLabel(levels)),
			cell(len(lats) > 0, c.latP95.Round(time.Millisecond).String()),
			cell(c.stabilityKnown, fmt.Sprintf("%d pts", c.stability)),
			fmtUSD(c.usd),
			scaleUSD(c.usd, 10000),
		)
	}
	w("")

	// ---------- failures ----------
	if fails := failures(obs); len(fails) > 0 {
		w("## Failures")
		w("")
		w("| Provider | Image | Trial | Error |")
		w("|---|---|---|---|")
		for _, o := range fails {
			w("| %s | %s | %d | %s |", o.Provider, o.Image, o.Trial, truncate([]byte(o.Err.Error()), 120))
		}
		w("")
	}

	// ---------- stability ----------
	w("## Stability -- same image, repeated calls")
	w("")
	w("The same photo sent twice should score the same twice. A provider that swings")
	w("more than %d points cannot support FR-8 trend lines: users would read noise as", gateMaxStabilityGap)
	w("progress, and the streak/trend feature would be actively misleading.")
	w("")
	w("**Skin age is held to a tighter line: %d year(s), not %d points.** It is shown",
		gateMaxSkinAgeDrift, gateMaxStabilityGap)
	w("as a bare number the user subtracts from their own age, and the UI now")
	w("celebrates a low one. A 2-year swing on identical pixels turns \"2 years")
	w("younger\" into \"right in step\" on a re-scan -- the concern scores hide drift")
	w("inside a severity band, and this number has nowhere to hide it.")
	w("")
	if trials < 2 {
		w("_Not measured -- run with `-trials 2` or higher._")
	} else {
		w("| Provider | Image | Max gap | Worst concern | Skin age drift |")
		w("|---|---|---|---|---|")
		for _, p := range providers {
			for _, row := range stabilityRows(byProvider[p.Name()]) {
				age := "no skin age"
				if row.ageKnown {
					age = fmt.Sprintf("%d yr", row.ageGap)
					if row.ageGap > gateMaxSkinAgeDrift {
						age += " **!**"
					}
				}
				w("| %s | %s | %d pts | %s | %s |",
					p.Name(), row.image, row.gap, row.worst, age)
			}
		}
	}
	w("")

	// ---------- tone calibration ----------
	w("## Skin-tone calibration signal")
	w("")
	w("Mean score per concern, grouped by Fitzpatrick band. This is the finding the")
	w("whole spike exists for: published dermatology evaluations show model accuracy")
	w("falling as Fitzpatrick type rises, and this product's primary users sit at the")
	w("top of that scale.")
	w("")
	w("> [!warning] Read this as a *signal*, not a verdict.")
	w("> A gap here means scores move with skin tone. It does NOT prove bias -- a")
	w("> genuinely more-affected sample would move the same way. Interpret it only")
	w("> against a sample you know is balanced, and confirm with labelled ground")
	w("> truth before rejecting a vendor on this alone.")
	w("")
	w("Rows are flagged `UNDERPOWERED` below %d images per band. Those numbers are", minPerBand)
	w("noise -- on a 5-image run, an arm with zero built-in bias produced apparent")
	w("gaps over 20 points. Do not read them, and do not quote them.")
	w("")
	// "faces", not "n" -- the count is distinct images, while the mean behind it
	// uses every observation. Labelling both as n is what let the two be
	// conflated in the first place.
	w("| Provider | Concern | mean I-III (faces) | mean IV-VI (faces) | gap | read? |")
	w("|---|---|---|---|---|---|")
	for _, p := range providers {
		for _, r := range toneRows(byProvider[p.Name()]) {
			note := "ok"
			switch {
			case !r.powered():
				note = "UNDERPOWERED"
			case r.gap > gateMaxToneGap || r.gap < -gateMaxToneGap:
				note = "**INVESTIGATE**"
			}
			w("| %s | %s | %.1f (%d) | %.1f (%d) | %+.1f | %s |",
				p.Name(), r.issue, r.light, r.nLight, r.dark, r.nDark, r.gap, note)
		}
	}
	w("")

	// ---------- verdict ----------
	w("## Gate verdict")
	w("")
	w("| Provider | Latency | Stability | Skin age | Coverage | Levels | Cost | Tone | Verdict |")
	w("|---|---|---|---|---|---|---|---|---|")
	for _, c := range cards {
		latOK := c.latP95 > 0 && c.latP95 <= gateMaxLatencyP95
		stabOK := c.stabilityKnown && c.stability <= gateMaxStabilityGap
		covOK := c.coverageMeasured && c.coverage >= gateMinCoverage
		costOK := c.usd >= 0 && c.usd <= gateMaxUSDPerScan

		// Skin age only gates an arm that claims to produce one. An arm with
		// no skin age fails FR-5 on coverage grounds, not on drift — scoring
		// "missing" as "unstable" would be the wrong diagnosis.
		ageDrift, ageKnown := providerSkinAgeDrift(byProvider[c.name])
		ageOK := !ageKnown || ageDrift <= gateMaxSkinAgeDrift
		ageCell := "n/a"
		if ageKnown {
			ageCell = fmt.Sprintf("%s (%d yr)", mark(ageOK), ageDrift)
		}

		// The domain model's Severity is a four-level enum. An arm that can
		// only say present/absent cannot fill it, however cheap it is.
		levels, levelsKnown := providerLevels(byProvider[c.name])
		levelsOK := levelsKnown && levels >= 4

		toneGap, tonePowered := providerTone(byProvider[c.name])
		toneCell := "not measured"
		if tonePowered {
			toneCell = mark(toneGap <= gateMaxToneGap)
		}

		// A provider is never marked PASS on an unmeasured tone gate. For this
		// product that is the gate that matters most, so "we could not measure
		// it" must read as unfinished, not as approval.
		verdict := "**PASS**"
		switch {
		case c.ok == 0:
			verdict = "no data"
		case !(latOK && stabOK && ageOK && covOK && costOK && levelsOK):
			verdict = "**FAIL**"
		case !tonePowered:
			verdict = "INCOMPLETE -- needs a bigger sample"
		case toneGap > gateMaxToneGap:
			verdict = "**FAIL**"
		}
		w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |",
			c.name, mark(latOK), mark(stabOK), ageCell,
			mark(covOK), mark(levelsOK), mark(costOK), toneCell, verdict)
	}
	w("")

	// ---------- honesty ----------
	w("## What this harness does NOT measure")
	w("")
	w("Stated plainly so the result is not over-read:")
	w("")
	w("1. **Accuracy.** There is no ground truth here. The harness measures whether a")
	w("   provider is *consistent, complete, fast, and affordable* -- not whether it is")
	w("   *right*. Accuracy needs images labelled by a dermatologist. Until then, no")
	w("   arm can be called accurate, including one that passes every gate above.")
	w("2. **Perceived correctness.** FR-4's real bar is a user reading the report and")
	w("   thinking that it matches their own skin. That is a user-testing question.")
	w("3. **Capture-condition robustness.** Real users shoot in bad light with makeup on.")
	w("   Cover that deliberately in the manifest rather than assuming clean inputs.")
	w("4. **Terms of service.** Whether a vendor permits storing outputs, or reselling")
	w("   analysis to B2B partners (FR-13), is a contract question that can invalidate")
	w("   a technically winning arm. Check it before signing.")
	w("")

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// providerLevels reports the WORST severity resolution any mapped concern
// achieved -- the weakest link, because the report screen has to render all six
// on one scale. An arm returning 0-100 for five concerns and a yes/no flag for
// the sixth cannot honestly show four levels.
func providerLevels(list []Observation) (int, bool) {
	worst, known := 4, false
	for _, o := range list {
		if !o.OK() {
			continue
		}
		for _, iss := range o.Report.Issues {
			if iss.Levels == 0 {
				continue // unmapped -- counted by Coverage, not here
			}
			known = true
			if iss.Levels < worst {
				worst = iss.Levels
			}
		}
	}
	return worst, known
}

func levelsLabel(n int) string {
	switch n {
	case 4:
		return "4 (full)"
	case 2:
		return "**2 (presence only)**"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func cell(known bool, s string) string {
	if !known {
		return "n/a"
	}
	return s
}

func mark(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func scaleUSD(perCall float64, n int) string {
	if perCall < 0 {
		return "quote-only"
	}
	return fmt.Sprintf("$%.0f", perCall*float64(n))
}

func failures(obs []Observation) []Observation {
	var out []Observation
	for _, o := range obs {
		if !o.OK() {
			out = append(out, o)
		}
	}
	return out
}

type stabRow struct {
	image    string
	gap      int
	worst    string
	ageGap   int
	ageKnown bool
}

// pairTrials groups a provider's successful observations by image so trial 1
// and trial 2 of the same photo can be compared.
func pairTrials(list []Observation) map[string][]Observation {
	byImage := map[string][]Observation{}
	for _, o := range list {
		if o.OK() {
			byImage[o.Image] = append(byImage[o.Image], o)
		}
	}
	return byImage
}

func stabilityRows(list []Observation) []stabRow {
	var rows []stabRow
	for img, trials := range pairTrials(list) {
		if len(trials) < 2 {
			continue
		}
		sort.Slice(trials, func(i, j int) bool { return trials[i].Trial < trials[j].Trial })
		gap, per := stabilityDelta(trials[0].Report, trials[1].Report)

		worst, worstV := "n/a", -1
		for _, t := range sortedIssueTypes(per) {
			if per[t] > worstV {
				worst, worstV = string(t), per[t]
			}
		}
		ageGap, ageKnown := skinAgeDelta(trials[0].Report, trials[1].Report)
		rows = append(rows, stabRow{
			image: img, gap: gap, worst: worst,
			ageGap: ageGap, ageKnown: ageKnown,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].gap > rows[j].gap })
	return rows
}

// providerSkinAgeDrift returns the WORST skin-age swing across all images, in
// years, and whether any image produced a comparable pair at all.
func providerSkinAgeDrift(list []Observation) (int, bool) {
	worst, known := 0, false
	for _, r := range stabilityRows(list) {
		if !r.ageKnown {
			continue
		}
		known = true
		if r.ageGap > worst {
			worst = r.ageGap
		}
	}
	return worst, known
}

// providerTone returns the largest absolute tone gap across all concerns, and
// whether the sample was large enough to mean anything.
func providerTone(list []Observation) (float64, bool) {
	worst, powered := 0.0, false
	for _, r := range toneRows(list) {
		if !r.powered() {
			continue
		}
		powered = true
		g := r.gap
		if g < 0 {
			g = -g
		}
		if g > worst {
			worst = g
		}
	}
	return worst, powered
}

func providerStability(list []Observation) (int, bool) {
	rows := stabilityRows(list)
	if len(rows) == 0 {
		return 0, false
	}
	return rows[0].gap, true // worst case, not average -- the tail is what users hit
}

// minPerBand is the smallest number of images per Fitzpatrick band at which a
// tone gap is worth reading at all.
//
// This is not a theoretical nicety. On a 5-image demo run, an arm with a
// KNOWN-ZERO tone bias produced apparent gaps of +21 and -19 points purely
// from per-image variation. Below this threshold the column measures which
// faces you happened to pick, not how the vendor treats skin tone.
//
// 10 per band is a floor for spotting a gross effect, not a substitute for a
// designed study. If a vendor decision is going to rest on this number, the
// sample needs to be balanced for age, sex, capture conditions, and actual
// skin-concern prevalence -- otherwise a real difference in the sample reads
// as vendor bias, and vice versa.
const minPerBand = 10

type toneRow struct {
	issue            IssueType
	light, dark, gap float64
	nLight, nDark    int
}

// powered reports whether this row has enough images behind it to interpret.
func (r toneRow) powered() bool { return r.nLight >= minPerBand && r.nDark >= minPerBand }

func toneRows(list []Observation) []toneRow {
	// imgs is a set, not a counter, and that distinction is the whole point.
	//
	// n counts observations, and every image contributes one per trial. Using
	// it for the power check meant `-trials 5` on two faces satisfied a gate
	// meant to require ten -- repeat calls on identical pixels adding apparent
	// statistical power while adding no new people. Power for a between-groups
	// comparison comes from distinct subjects; the mean still uses every
	// observation, which is correct.
	type acc struct {
		sum  float64
		n    int
		imgs map[string]bool
	}
	light := map[IssueType]*acc{}
	dark := map[IssueType]*acc{}

	for _, o := range list {
		if !o.OK() {
			continue
		}
		var target map[IssueType]*acc
		switch fitzBand(o.Fitz) {
		case "I-III":
			target = light
		case "IV-VI":
			target = dark
		default:
			continue
		}
		for _, iss := range o.Report.Issues {
			if iss.Score < 0 {
				continue
			}
			if target[iss.Type] == nil {
				target[iss.Type] = &acc{imgs: map[string]bool{}}
			}
			target[iss.Type].sum += float64(iss.Score)
			target[iss.Type].n++
			target[iss.Type].imgs[o.Image] = true
		}
	}

	var rows []toneRow
	for _, t := range AllIssueTypes {
		l, d := light[t], dark[t]
		if l == nil || d == nil || l.n == 0 || d.n == 0 {
			continue // both bands must be represented or the comparison is meaningless
		}
		lm, dm := l.sum/float64(l.n), d.sum/float64(d.n)
		rows = append(rows, toneRow{
			issue: t, light: lm, dark: dm, gap: dm - lm,
			nLight: len(l.imgs), nDark: len(d.imgs),
		})
	}
	return rows
}
