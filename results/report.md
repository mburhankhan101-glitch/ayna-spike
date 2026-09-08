# AI Provider Spike -- Results

Generated 2026-09-01 03:54 | 1 observations | 1 trials per image

> Gate thresholds were fixed before the run: latency p95 < 8s, stability gap <= 8 pts,
> coverage >= 5/6, tone gap <= 12 pts, cost <= $0.2000 per scan.

## Scorecard

| Provider | Calls | OK | Coverage | Severity levels | Latency p95 | Stability | $/scan | $/10k scans |
|---|---|---|---|---|---|---|---|---|
| ailab-pro | 1 | 1 | 6/6 | 4 (full) | 3.007s | n/a | $0.1890 | $1890 |

## Stability -- same image, repeated calls

The same photo sent twice should score the same twice. A provider that swings
more than 8 points cannot support FR-8 trend lines: users would read noise as
progress, and the streak/trend feature would be actively misleading.

**Skin age is held to a tighter line: 1 year(s), not 8 points.** It is shown
as a bare number the user subtracts from their own age, and the UI now
celebrates a low one. A 2-year swing on identical pixels turns "2 years
younger" into "right in step" on a re-scan -- the concern scores hide drift
inside a severity band, and this number has nowhere to hide it.

_Not measured -- run with `-trials 2` or higher._

## Skin-tone calibration signal

Mean score per concern, grouped by Fitzpatrick band. This is the finding the
whole spike exists for: published dermatology evaluations show model accuracy
falling as Fitzpatrick type rises, and this product's primary users sit at the
top of that scale.

> [!warning] Read this as a *signal*, not a verdict.
> A gap here means scores move with skin tone. It does NOT prove bias -- a
> genuinely more-affected sample would move the same way. Interpret it only
> against a sample you know is balanced, and confirm with labelled ground
> truth before rejecting a vendor on this alone.

Rows are flagged `UNDERPOWERED` below 10 images per band. Those numbers are
noise -- on a 5-image run, an arm with zero built-in bias produced apparent
gaps over 20 points. Do not read them, and do not quote them.

| Provider | Concern | mean I-III (faces) | mean IV-VI (faces) | gap | read? |
|---|---|---|---|---|---|

## Gate verdict

| Provider | Latency | Stability | Skin age | Coverage | Levels | Cost | Tone | Verdict |
|---|---|---|---|---|---|---|---|---|
| ailab-pro | pass | fail | n/a | pass | pass | pass | not measured | **FAIL** |

## What this harness does NOT measure

Stated plainly so the result is not over-read:

1. **Accuracy.** There is no ground truth here. The harness measures whether a
   provider is *consistent, complete, fast, and affordable* -- not whether it is
   *right*. Accuracy needs images labelled by a dermatologist. Until then, no
   arm can be called accurate, including one that passes every gate above.
2. **Perceived correctness.** FR-4's real bar is a user reading the report and
   thinking that it matches their own skin. That is a user-testing question.
3. **Capture-condition robustness.** Real users shoot in bad light with makeup on.
   Cover that deliberately in the manifest rather than assuming clean inputs.
4. **Terms of service.** Whether a vendor permits storing outputs, or reselling
   analysis to B2B partners (FR-13), is a contract question that can invalidate
   a technically winning arm. Check it before signing.

