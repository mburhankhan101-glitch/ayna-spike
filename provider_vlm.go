package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// VLMBaseline is the CONTROL ARM: a general-purpose vision model asked to fill
// in the same six FR-4 concerns, with no skin-specific training.
//
// It is here to answer a question the vendor arms cannot: how much of a
// specialist API's value is real signal versus a confident-looking number?
// If a general model matches a $0.02/call specialist on agreement and
// stability, the specialist is not buying much. If it is clearly worse, that
// is direct evidence the specialist vendors add value -- which is also the
// evidence a B2B buyer (FR-13) will eventually ask you for.
//
// Read the result honestly: a VLM's published dermatology accuracy drops
// sharply on higher Fitzpatrick types, which is exactly the population this
// product targets. Expect this arm to fail the calibration gate. Measuring
// that failure is the point -- it is the baseline the others must beat.
type VLMBaseline struct {
	client anthropic.Client
	ok     bool
}

func NewVLMBaseline(key string) *VLMBaseline {
	if key == "" {
		return &VLMBaseline{}
	}
	return &VLMBaseline{
		client: anthropic.NewClient(option.WithAPIKey(key)),
		ok:     true,
	}
}

func (v *VLMBaseline) Name() string     { return "vlm-baseline" }
func (v *VLMBaseline) Configured() bool { return v.ok }

// Rough per-call cost at claude-opus-5 rates ($5/$25 per Mtok): a ~1.5Mpx
// JPEG runs ~1.5k input tokens, output is a small tool call. Recomputed for
// real from usage after a run -- see the cost column in the report.
func (v *VLMBaseline) USDPerCall() float64 { return 0.012 }

const vlmSystem = `You are scoring a face photo for a consumer skincare app.
This is NOT a medical diagnosis and must never be presented as one.

Score each of the six concerns 0-100, where 0 = not present at all and
100 = the most severe presentation you would expect to see in a consumer app.
Report a calibrated confidence for each. If lighting, focus, framing, or
occlusion make a concern genuinely unassessable, set its confidence below 0.3
rather than guessing -- an honest low confidence is far more useful here than
a plausible number.

Judge the skin against what is typical FOR THIS PERSON'S OWN skin tone.
Do not treat deeper skin tones as inherently more "spotted" or "uneven":
melanin variation is not a skin concern.`

func skinReportTool() anthropic.ToolParam {
	issueSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":       map[string]any{"type": "string", "enum": AllIssueTypes},
			"score":      map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"type", "score", "confidence"},
		"additionalProperties": false,
	}

	return anthropic.ToolParam{
		Name:        "report_skin",
		Description: anthropic.String("Report the skin assessment for the supplied photo."),
		Strict:      anthropic.Bool(true),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"overallScore": map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
				"skinAge":      map[string]any{"type": "integer", "minimum": 10, "maximum": 90},
				"issues": map[string]any{
					"type": "array", "items": issueSchema,
					"minItems": 6, "maxItems": 6,
				},
			},
			Required: []string{"overallScore", "skinAge", "issues"},
			ExtraFields: map[string]any{
				"additionalProperties": false,
			},
		},
	}
}

type vlmOut struct {
	OverallScore int `json:"overallScore"`
	SkinAge      int `json:"skinAge"`
	Issues       []struct {
		Type       IssueType `json:"type"`
		Score      int       `json:"score"`
		Confidence float64   `json:"confidence"`
	} `json:"issues"`
}

func (v *VLMBaseline) Analyze(ctx context.Context, jpeg []byte) (SkinReport, []byte, error) {
	if !v.Configured() {
		return SkinReport{}, nil, ErrNotConfigured
	}

	tool := skinReportTool()
	resp, err := v.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-opus-5",
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: vlmSystem}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		Tools:     []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: tool.Name},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64("image/jpeg", base64.StdEncoding.EncodeToString(jpeg)),
				anthropic.NewTextBlock("Assess this photo and call report_skin."),
			),
		},
	})
	if err != nil {
		return SkinReport{}, nil, err
	}

	// A safety refusal is a real operational outcome for a face-photo product,
	// not a bug in the harness -- record it as such.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return SkinReport{}, nil, fmt.Errorf("refused (%s): %s",
			resp.StopDetails.Category, resp.StopDetails.Explanation)
	}

	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		raw := []byte(tu.JSON.Input.Raw())

		var out vlmOut
		if err := json.Unmarshal(raw, &out); err != nil {
			return SkinReport{}, raw, fmt.Errorf("decode tool input: %w", err)
		}

		rep := SkinReport{OverallScore: out.OverallScore, SkinAge: out.SkinAge}
		for _, i := range out.Issues {
			rep.Issues = append(rep.Issues, Issue{
				Type:       i.Type,
				Score:      i.Score,
				Severity:   SeverityFromScore(i.Score),
				SeverityS:  SeverityFromScore(i.Score).String(),
				Confidence: i.Confidence,
				HasMask:    false, // a VLM gives no pixel mask -- FR-4's heatmap is not satisfiable from this arm alone
				Levels:     4,
			})
		}
		return rep, raw, nil
	}

	return SkinReport{}, nil, errors.New("model returned no tool_use block")
}
