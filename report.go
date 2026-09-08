package main

import "fmt"

// Severity mirrors the domain value object from 02-Domain-Discovery.
// Ordered so it can be compared: None < Mild < Moderate < Severe.
type Severity int

const (
	SeverityUnknown Severity = iota
	SeverityNone
	SeverityMild
	SeverityModerate
	SeveritySevere
)

func (s Severity) String() string {
	switch s {
	case SeverityNone:
		return "none"
	case SeverityMild:
		return "mild"
	case SeverityModerate:
		return "moderate"
	case SeveritySevere:
		return "severe"
	default:
		return "unknown"
	}
}

// SeverityFromScore is the shared 0-100 -> enum bucketing used when a vendor
// gives us a number but no severity label.
//
// NOTE: these cutoffs are a SPIKE PLACEHOLDER. The real thresholds are the
// single most sensitive piece of business logic in the product (FR-7) and are
// an open product decision, not an engineering constant. See the hot spot in
// 02-Domain-Discovery-and-Event-Storming.md.
func SeverityFromScore(score int) Severity {
	switch {
	case score < 0:
		return SeverityUnknown
	case score < 25:
		return SeverityNone
	case score < 50:
		return SeverityMild
	case score < 75:
		return SeverityModerate
	default:
		return SeveritySevere
	}
}

// IssueType is the closed set the product promises in FR-4. A provider that
// cannot be mapped onto all six is a finding, not a footnote.
type IssueType string

const (
	IssueAcne      IssueType = "acne"
	IssueRedness   IssueType = "redness"
	IssueDryness   IssueType = "dryness"
	IssueDarkSpots IssueType = "dark_spots"
	IssueTexture   IssueType = "texture"
	IssuePores     IssueType = "pores"
)

// AllIssueTypes is the FR-4 contract, in display order.
var AllIssueTypes = []IssueType{
	IssueAcne, IssueRedness, IssueDryness, IssueDarkSpots, IssueTexture, IssuePores,
}

type Issue struct {
	Type       IssueType `json:"type"`
	Severity   Severity  `json:"-"`
	SeverityS  string    `json:"severity"`
	Score      int       `json:"score"`      // 0-100, vendor-normalised
	Confidence float64   `json:"confidence"` // 0-1; -1 when the vendor gives none
	HasMask    bool      `json:"hasMask"`    // vendor returned a polygon/heatmap for this issue
	Levels     int       `json:"levels"`     // severity resolution this field supports: 4 (scored), 2 (presence), 0 (unmapped)
}

// SkinReport is the canonical shape from 05-Project-Structure-and-Contracts.
// Every provider adapter must produce THIS, or explain why it cannot.
type SkinReport struct {
	OverallScore      int     `json:"overallScore"`
	SkinAge           int     `json:"skinAge"` // -1 when unsupported
	Issues            []Issue `json:"issues"`
	ReferralSuggested bool    `json:"referralSuggested"`
}

// Coverage reports which FR-4 issues the provider actually filled in.
// This is the headline number of the spike: a provider at 4/6 means the
// report screen has two empty rows, which is a product decision, not a bug.
func (r SkinReport) Coverage() (got int, missing []IssueType) {
	present := map[IssueType]bool{}
	for _, i := range r.Issues {
		if i.Severity != SeverityUnknown {
			present[i.Type] = true
		}
	}
	for _, t := range AllIssueTypes {
		if present[t] {
			got++
		} else {
			missing = append(missing, t)
		}
	}
	return got, missing
}

func (r SkinReport) issueByType(t IssueType) (Issue, bool) {
	for _, i := range r.Issues {
		if i.Type == t {
			return i, true
		}
	}
	return Issue{}, false
}

func (r SkinReport) String() string {
	got, _ := r.Coverage()
	return fmt.Sprintf("score=%d skinAge=%d issues=%d/6", r.OverallScore, r.SkinAge, got)
}
