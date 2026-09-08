package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// Provider is deliberately the same shape as the AIAnalysisProvider port in
// 03-DDD-and-Onion-Architecture.md. If a real vendor cannot be made to sit
// behind this interface, the port is wrong -- and it is much cheaper to learn
// that here than inside internal/modules/skinanalysis.
type Provider interface {
	Name() string

	// Configured reports whether credentials are present, so the harness can
	// skip a provider cleanly instead of failing the whole run.
	Configured() bool

	// Analyze sends one image and returns the canonical report plus the raw
	// vendor body (kept for the mapping audit -- see notes in the vault).
	Analyze(ctx context.Context, jpeg []byte) (SkinReport, []byte, error)

	// Pricing is the vendor's list cost per successful call, in USD.
	// -1 means "not published / quote only", which is itself a finding
	// against NFR-7 (cost per job must be trackable).
	USDPerCall() float64
}

var ErrNotConfigured = errors.New("provider not configured: missing API key")

// Registry returns every candidate arm of the spike, configured or not.
func Registry() []Provider {
	return []Provider{
		NewAILabPro(os.Getenv("AILAB_API_KEY")),
		NewAILabBasic(os.Getenv("AILAB_API_KEY")),
		NewPerfectCorp(os.Getenv("PERFECTCORP_API_KEY"), os.Getenv("PERFECTCORP_SECRET")),
		NewVLMBaseline(os.Getenv("ANTHROPIC_API_KEY")),
	}
}

// Observation is one (provider, image) trial.
type Observation struct {
	Provider   string
	Image      string
	Fitz       string // Fitzpatrick label from the manifest
	Trial      int    // 1 or 2 -- same image twice, to measure stability
	Report     SkinReport
	Latency    time.Duration
	Err        error
	RawBytes   int
	ReceivedAt time.Time
}

func (o Observation) OK() bool { return o.Err == nil }

// stabilityDelta compares two trials of the same image: the max absolute
// difference in any issue score. A vendor that swings 30 points on an
// identical input cannot support FR-8 trend lines -- users will see
// "improvement" that is just noise.
func stabilityDelta(a, b SkinReport) (maxDelta int, per map[IssueType]int) {
	per = map[IssueType]int{}
	for _, t := range AllIssueTypes {
		ia, oka := a.issueByType(t)
		ib, okb := b.issueByType(t)
		if !oka || !okb {
			continue
		}
		d := ia.Score - ib.Score
		if d < 0 {
			d = -d
		}
		per[t] = d
		if d > maxDelta {
			maxDelta = d
		}
	}
	return maxDelta, per
}

// skinAgeDelta compares the skin-age reading across two trials of the same
// photo, in YEARS. It is deliberately separate from stabilityDelta: the units
// differ, and so does the tolerance.
//
// Skin age is now a headline number in the UI — it gets its own card, and the
// user reads it as a delta against their real age ("two years younger"). That
// makes it far less forgiving than a concern score. A concern drifting 3
// points is invisible inside a severity band; skin age drifting 3 years turns
// "two years younger" into "three years older" on a re-scan of the same face,
// and takes the app's credibility with it.
//
// Returns false when either trial had no skin age at all — the Basic arm
// carries none, and "missing" must never be scored as "stable".
func skinAgeDelta(a, b SkinReport) (int, bool) {
	if a.SkinAge < 0 || b.SkinAge < 0 {
		return 0, false
	}
	d := a.SkinAge - b.SkinAge
	if d < 0 {
		d = -d
	}
	return d, true
}

func sortedIssueTypes(m map[IssueType]int) []IssueType {
	out := make([]IssueType, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func fmtUSD(v float64) string {
	if v < 0 {
		return "quote-only"
	}
	return fmt.Sprintf("$%.4f", v)
}
