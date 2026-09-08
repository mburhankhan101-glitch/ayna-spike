package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Offline replay: rebuild the report from saved vendor responses, with no API
// calls and no cost.
//
// This exists because of what happened to ailab-basic. Its field paths were
// transcribed from documentation, the documentation was wrong, and the first
// run reported two concerns MISSING that the vendor had actually returned.
// Fixing the mapping meant paying for the whole run a second time.
//
// A vendor response is evidence and it does not change. The mapping over it is
// a guess that will be wrong at least once per vendor -- so the call and the
// interpretation should not cost the same thing twice. Pro is 4.7x the price
// of Basic, so this stops being a nicety and starts being the difference
// between one mistake and two.
//
// Deliberately NOT a cache: replay never calls anything, and a run always
// calls. Nothing silently serves stale data.

// Remapper is implemented by providers whose response mapping can be re-run
// offline. A provider that cannot do this simply is not replayable, and its
// rows are reported as such rather than quietly dropped.
type Remapper interface {
	MapRaw(raw []byte) (SkinReport, error)
}

// obsRecord is everything about one observation that cannot be recovered from
// the saved response body -- chiefly latency, which is a property of the call
// rather than of the payload.
type obsRecord struct {
	Provider  string `json:"provider"`
	Image     string `json:"image"`
	Fitz      string `json:"fitz"`
	Trial     int    `json:"trial"`
	LatencyMS int64  `json:"latency_ms"`
	RawFile   string `json:"raw_file"`
	Err       string `json:"err,omitempty"`
}

func rawFileName(provider, image string, trial int) string {
	return fmt.Sprintf("%s__%s__t%d.json", provider,
		image[:len(image)-len(filepath.Ext(image))], trial)
}

// saveObservations records the run alongside the raw bodies so it can be
// replayed later. Written after every real run, cheap, and useless until the
// first time a mapping turns out to be wrong -- at which point it is the
// difference between an edit and a purchase.
func saveObservations(outDir string, obs []Observation) error {
	recs := make([]obsRecord, 0, len(obs))
	for _, o := range obs {
		r := obsRecord{
			Provider:  o.Provider,
			Image:     o.Image,
			Fitz:      o.Fitz,
			Trial:     o.Trial,
			LatencyMS: o.Latency.Milliseconds(),
			RawFile:   rawFileName(o.Provider, o.Image, o.Trial),
		}
		if o.Err != nil {
			r.Err = o.Err.Error()
		}
		recs = append(recs, r)
	}

	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "observations.json"), b, 0o644)
}

// loadReplay rebuilds observations from disk by re-mapping the saved bodies
// through each provider's CURRENT mapping -- which is the entire point. The
// bytes are fixed; the interpretation is what is being corrected.
func loadReplay(outDir string, providers []Provider) ([]Observation, []Provider, error) {
	b, err := os.ReadFile(filepath.Join(outDir, "observations.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("%w\n\nreplay needs a previous real run in %s", err, outDir)
	}

	var recs []obsRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, nil, err
	}

	byName := map[string]Provider{}
	for _, p := range providers {
		byName[p.Name()] = p
	}

	var obs []Observation
	seen := map[string]bool{}
	var active []Provider

	for _, r := range recs {
		p, ok := byName[r.Provider]
		if !ok {
			fmt.Printf("  skip  %-14s in observations.json but not in the registry\n", r.Provider)
			continue
		}

		o := Observation{
			Provider: r.Provider, Image: r.Image, Fitz: r.Fitz, Trial: r.Trial,
			Latency: time.Duration(r.LatencyMS) * time.Millisecond,
		}

		switch {
		case r.Err != "":
			// A call that failed then still failed now. Preserved rather than
			// dropped, so the OK count in the report stays honest.
			o.Err = fmt.Errorf("%s", r.Err)
		default:
			rm, canReplay := p.(Remapper)
			if !canReplay {
				o.Err = fmt.Errorf("provider %s cannot be replayed offline", r.Provider)
				break
			}
			raw, err := os.ReadFile(filepath.Join(outDir, "raw", r.RawFile))
			if err != nil {
				o.Err = fmt.Errorf("raw body missing: %v", err)
				break
			}
			rep, err := rm.MapRaw(raw)
			if err != nil {
				o.Err = err
			} else {
				o.Report = rep
				o.RawBytes = len(raw)
			}
		}

		obs = append(obs, o)
		if !seen[r.Provider] {
			seen[r.Provider] = true
			active = append(active, p)
		}
	}

	if len(obs) == 0 {
		return nil, nil, fmt.Errorf("observations.json had no usable rows")
	}
	return obs, active, nil
}
