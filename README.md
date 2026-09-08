# ayna-spike — AI provider evaluation harness

Throwaway code with a single job: find out whether any affordable vision API
can actually deliver the report [FR-4 / FR-5](../files/01-Product-Vision-and-Requirements.md)
promises, **for the skin tones this product is aimed at**, before a line of
`ayna-backend` gets written.

This is Phase 0 risk-reduction, not production code. Delete it when the
decision is made — the answer belongs in the vault note, not in this repo.

## Why this exists

The Phase 0 docs specify the whole system on top of one unexamined assumption:
that a third-party "AI Vision Provider" returns six per-issue severities, a
confidence, a pixel heatmap, and a skin age. Every downstream artifact — the
`SkinReport` aggregate, the event payload, the report screen, the unit
economics in NFR-7 — inherits that assumption. If it is wrong, none of them
survive contact with a real vendor.

## Setup

Needs Go 1.26+. No other tooling.

```bash
go build ./...
```

Set whichever keys you have. Unconfigured arms are skipped, not failed:

```bash
export AILAB_API_KEY=...          # AILab Tools Skin Analyze Pro
export ANTHROPIC_API_KEY=...      # general-vision control arm
```

## Try it with no keys and no cost

```bash
go run . -demo -trials 2
```

Synthetic providers, fabricated numbers. Two of them deliberately reproduce the
failure modes the gates exist to catch (`demo-jittery` is unstable,
`demo-tonebias` scores darker skin worse). If a `-demo` run stops flagging
those, the analysis code has regressed.

## A real run

1. **Replace the placeholder images.** `images/` currently holds five flat
   colour swatches, not faces — they exist so `-demo` has something to read.
   The harness will happily "analyse" them and produce meaningless numbers.

2. **Build a real sample.** Aim for **at least 10 images per Fitzpatrick band**
   (I–III and IV–VI). Below that the tone comparison measures which faces you
   picked, not how the vendor behaves — the harness marks such rows
   `UNDERPOWERED` and refuses to conclude from them. Balance the bands for age,
   sex, and capture conditions, or a genuine difference in your sample will read
   as vendor bias.

3. **Get consent in writing** for every face you upload, and keep it. You are
   sending other people's biometric-adjacent data to third-party vendors — the
   same standard NFR-4 sets for your users applies to your test set. Do not
   scrape faces off the internet for this.

4. **Label them** in `images/manifest.csv`:

   ```csv
   filename,fitzpatrick,notes
   p001.jpg,V,good light / no makeup
   p002.jpg,IV,indoor evening / mild acne
   ```

5. **Run:**

   ```bash
   go run . -trials 2
   ```

Output lands in `results/report.md`, with every raw vendor response in
`results/raw/` for the mapping audit.

## The arms

An **arm** is one option the test measures. Same photos, same conditions, side
by side — the word comes from clinical trials, where one arm gets the drug and
one gets the placebo.

| Arm | What it is | $/call |
|---|---|---|
| `ailab-pro` | AILab Skin Analyze Pro — 70 credits | $0.1890 |
| `ailab-basic` | AILab Skin Analyze — 15 credits | **$0.0405** |
| `perfectcorp` | Perfect Corp — not implemented (see its file) | quote-only |
| `vlm-baseline` | A general vision model — the comparison floor | ~$0.012 |

Basic is **4.7× cheaper than Pro**, and that gap is the single biggest lever on
your paid-tier margin. The vendor's descriptions of the two overlap almost
entirely and settle nothing, so both run and the report puts them side by side.

### The question Basic has to answer

Not just "does it work" — **at what resolution**. The domain model's `Severity`
has four levels. A vendor returning a 0-100 score fills that. A vendor returning
a yes/no presence flag gives two levels, and no amount of cheapness fixes that:
"you have a pore issue: yes" is not a report.

So the scorecard carries a **Severity levels** column, and an arm stuck at 2
fails the gate however well it does elsewhere. The prediction encoded in
`ailabBasicFields` is 4/6 coverage at two levels — but it *is* a prediction,
from documentation, and the run is what settles it. If Basic surprises us, your
paid tier gets 4.7× cheaper.

## What the gates mean

Thresholds live at the top of `analysis.go`, fixed before the data arrives so
the result cannot be rationalised afterwards. Each traces to an NFR:

| Gate | Threshold | Why |
|---|---|---|
| Latency p95 | < 8s | NFR-2, end-to-end perceived time |
| Stability | ≤ 8 pts between identical calls | FR-8 trends are noise above this |
| Skin age drift | ≤ 1 **year** between identical calls | it is a headline number now, not a subtitle |
| Coverage | ≥ 5 of 6 FR-4 concerns | fewer means empty rows on the report screen |
| Severity levels | 4 | the domain enum has four; two cannot fill it |
| Cost | ≤ $0.20 / scan | paid-tier ceiling — a Pakistani Plus user break-evens at 11.3 scans/month |
| Tone gap | ≤ 12 pts between bands | the reason this spike exists |

The cost gate applies to the **paid** tier only. The free tier runs a cheap
general model under the tiering decision, so it is not measured here.

**Skin age is held to a much tighter line than the concern scores**, and in a
different unit. The concerns are shown as a severity band, so a few points of
drift disappears inside "Mild". Skin age is a bare number the user subtracts
from their own age — and the app now celebrates a low one with its own card.
A 2-year swing on identical pixels turns "2 years younger" into "right in
step" on a re-scan. The number has nowhere to hide its drift, so it gets 1
year, and the report prints the real figure rather than only pass/fail.

An arm that returns no skin age (Basic) reads **n/a** on that gate, never
"pass" — a field the vendor never produced cannot be called stable.

A provider that passes everything except tone is marked **INCOMPLETE**, never
PASS. For this product an unmeasured tone gate is unfinished work, not approval.

## The mapping audit

The interesting output is not the scores — it is `ailabFieldMap` in
`provider_ailab.go` and how badly it fits. Two rows already look wrong on paper:

- `dryness ← moisture` is **inverted** (high moisture means low dryness)
- `redness ← sensitivity` is a **proxy**, not a synonym — "sensitivity" is a
  vendor composite, while FR-4 promises visible redness

If the spike shows those diverge from what a human sees in the photo, FR-4 needs
rewording — that is a product finding, and it is worth more than the scorecard.

## Known limits

`Provider` is deliberately the same shape as the `AIAnalysisProvider` port in
[03-DDD](../files/03-DDD-and-Onion-Architecture.md). If a real vendor cannot sit
behind it, the port is wrong — and learning that here is much cheaper than
learning it inside `internal/modules/skinanalysis`.

Perfect Corp is **not implemented**: it needs a five-step auth + upload + poll
flow (see the header comment in `provider_perfectcorp.go`). That integration
cost is itself a scored finding. Implement it only if the cheaper arms fail.

The harness measures consistency, completeness, latency, and cost. It does
**not** measure accuracy — that needs dermatologist-labelled ground truth. No
arm can be called accurate on the strength of this report, including one that
passes every gate.
