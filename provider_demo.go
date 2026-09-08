package main

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math/rand"
	"time"
)

// DemoProvider generates deterministic synthetic data so the harness and the
// report can be exercised without spending a cent or holding any API key.
//
// It is ONLY reachable via -demo, and it deliberately fabricates two of the
// exact failure modes the gates exist to catch, so you can confirm the report
// actually detects them before trusting it on real vendors:
//
//	demo-jittery : swings wildly between identical calls  -> should FAIL stability
//	demo-tonebias: scores darker skin worse for dark_spots -> should trip the tone flag
//
// If a run with -demo does not flag both, the analysis code is broken, not the
// vendors.
type DemoProvider struct {
	name      string
	jitter    int // max random swing between trials
	toneBias  int // points added to dark_spots/texture for Fitz IV-VI
	latencyMs int
	skinAge   bool
}

func demoRegistry() []Provider {
	return []Provider{
		&DemoProvider{name: "demo-clean", jitter: 2, toneBias: 0, latencyMs: 900, skinAge: true},
		&DemoProvider{name: "demo-jittery", jitter: 25, toneBias: 0, latencyMs: 2400, skinAge: true},
		&DemoProvider{name: "demo-tonebias", jitter: 3, toneBias: 22, latencyMs: 1100, skinAge: false},
	}
}

func (d *DemoProvider) Name() string        { return d.name }
func (d *DemoProvider) Configured() bool    { return true }
func (d *DemoProvider) USDPerCall() float64 { return 0.018 }

// demoFitz is set by the harness before each call so the synthetic provider can
// react to skin tone -- the whole point of the tonebias arm.
var demoFitz string

func (d *DemoProvider) Analyze(ctx context.Context, jpeg []byte) (SkinReport, []byte, error) {
	// Seed from the image bytes so the same photo gives the same base scores;
	// jitter is layered on top from a clock-derived source.
	h := fnv.New64a()
	_, _ = h.Write(jpeg)
	base := rand.New(rand.NewSource(int64(h.Sum64())))
	noise := rand.New(rand.NewSource(time.Now().UnixNano()))

	select {
	case <-ctx.Done():
		return SkinReport{}, nil, ctx.Err()
	case <-time.After(time.Duration(d.latencyMs) * time.Millisecond):
	}

	rep := SkinReport{SkinAge: -1}
	for _, t := range AllIssueTypes {
		score := base.Intn(60) + 10
		score += noise.Intn(d.jitter+1) - d.jitter/2

		if d.toneBias > 0 && fitzBand(demoFitz) == "IV-VI" &&
			(t == IssueDarkSpots || t == IssueTexture) {
			score += d.toneBias
		}
		score = clamp(score, 0, 100)

		rep.Issues = append(rep.Issues, Issue{
			Type:       t,
			Score:      score,
			Severity:   SeverityFromScore(score),
			SeverityS:  SeverityFromScore(score).String(),
			Confidence: 0.6 + base.Float64()*0.35,
			HasMask:    true,
			Levels:     4,
		})
	}

	if d.skinAge {
		age := 20 + base.Intn(20)
		// An unstable model is unstable everywhere, so the jittery arm drifts
		// its skin age too -- otherwise the skin-age gate is never exercised
		// and a regression in it would go unnoticed. Scaled down from the
		// score jitter because the units differ: years, not points.
		if ageJitter := d.jitter / 8; ageJitter > 0 {
			age += noise.Intn(ageJitter+1) - ageJitter/2
		}
		rep.SkinAge = clamp(age, 12, 90)
	}
	rep.OverallScore = derivedOverall(rep.Issues)

	raw, _ := json.MarshalIndent(map[string]any{
		"_note":  "SYNTHETIC DATA from -demo mode. Not a real vendor response.",
		"report": rep,
	}, "", "  ")
	return rep, raw, nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
