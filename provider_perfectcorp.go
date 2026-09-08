package main

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Perfect Corp (YouCam) -- Skin Analysis API.
//
// Included as an arm because it is the market leader and publishes a
// clinically-validated 95% test-retest reliability claim, which is directly
// the property NFR-2/FR-8 need (a trend line is worthless if the same face
// scores differently on Tuesday). It covers 15+ concerns with 0-100 severity
// and returns heatmaps/binary masks, with a self-serve free tier.
//
// KNOWN GAP vs FR-5: the published product page does not list a skin-age
// output. If this vendor wins on accuracy, FR-5 ("skin age") either needs a
// second source or has to be computed by us -- which makes skin age OUR
// modelling claim rather than a vendor's, with the disclosure burden that
// implies under NFR-6.
//
// INTEGRATION COST FINDING (this is a scored criterion, not an excuse):
// unlike AILab's single multipart POST, Perfect Corp requires a multi-step
// flow before a single image is analysed:
//
//  1. POST /s2s/v1.0/client/auth   -- exchange API key + RSA-signed payload
//     for a short-lived id_token
//  2. POST /s2s/v1.1/file/skin-analysis -- request a signed upload URL
//  3. PUT  <signed url>            -- upload the JPEG bytes
//  4. POST /s2s/v1.0/task/skin-analysis -- start the task, get task_id
//  5. GET  /s2s/v1.0/task/skin-analysis?task_id=... -- poll until done
//
// That is a genuine half-day of work and an async task model the harness
// would need to poll. It is deliberately NOT stubbed out with fake data:
// implement it only if the cheaper arms fail the calibration test, and record
// the real elapsed integration time as this arm's integration score.
type PerfectCorp struct {
	key    string
	secret string
	hc     *http.Client
}

func NewPerfectCorp(key, secret string) *PerfectCorp {
	return &PerfectCorp{key: key, secret: secret, hc: &http.Client{Timeout: 90 * time.Second}}
}

func (p *PerfectCorp) Name() string        { return "perfectcorp" }
func (p *PerfectCorp) Configured() bool    { return p.key != "" && p.secret != "" }
func (p *PerfectCorp) USDPerCall() float64 { return -1 } // published as "units"; quote-only past the free 40

var ErrPerfectCorpTODO = errors.New(
	"perfectcorp: 5-step auth+upload+poll flow not implemented -- see the header comment in provider_perfectcorp.go; " +
		"implement only if a cheaper arm fails the calibration gate")

func (p *PerfectCorp) Analyze(ctx context.Context, jpeg []byte) (SkinReport, []byte, error) {
	if !p.Configured() {
		return SkinReport{}, nil, ErrNotConfigured
	}
	return SkinReport{}, nil, ErrPerfectCorpTODO
}
