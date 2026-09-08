package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type manifestEntry struct {
	File  string
	Fitz  string // I..VI -- self-reported or labelled Fitzpatrick type
	Notes string
}

func loadManifest(dir string) ([]manifestEntry, error) {
	f, err := os.Open(filepath.Join(dir, "manifest.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	var out []manifestEntry
	for i, row := range rows {
		if i == 0 || len(row) < 2 || strings.HasPrefix(strings.TrimSpace(row[0]), "#") {
			continue // header or comment
		}
		e := manifestEntry{File: strings.TrimSpace(row[0]), Fitz: strings.TrimSpace(row[1])}
		if len(row) > 2 {
			e.Notes = strings.TrimSpace(row[2])
		}
		out = append(out, e)
	}
	return out, nil
}

func main() {
	var (
		imgDir  = flag.String("images", "images", "directory holding test JPEGs + manifest.csv")
		outDir  = flag.String("out", "results", "directory for the report and raw responses")
		trials  = flag.Int("trials", 2, "calls per image per provider (>=2 measures stability)")
		timeout = flag.Duration("timeout", 90*time.Second, "per-call timeout")
		saveRaw = flag.Bool("raw", true, "write each raw vendor response to results/raw/")
		demo    = flag.Bool("demo", false, "run synthetic providers instead of real ones (no API keys, no cost)")
		only    = flag.String("only", "", "comma-separated arm names to run, e.g. ailab-basic (default: every configured arm)")
		replay  = flag.Bool("replay", false, "rebuild the report from saved responses in -out; no API calls, no cost")
		maps    = flag.Bool("maps", false, "request FR-4 heatmap overlays from ailab-pro (much larger responses)")
	)
	flag.Parse()

	entries, err := loadManifest(*imgDir)
	if err != nil {
		fatal("read manifest: %v\n\nExpected %s with rows: filename,fitzpatrick,notes\nSee README.md.", err, filepath.Join(*imgDir, "manifest.csv"))
	}
	if len(entries) == 0 {
		fatal("manifest has no image rows")
	}

	providers := Registry()
	if *maps {
		// Opt-in: maps arrive as base64 inside the JSON and inflate the response
		// by orders of magnitude, so a run that does not need them should not pay
		// the bandwidth or the disk.
		for i, p := range providers {
			if a, ok := p.(*AILab); ok && a.Name() == "ailab-pro" {
				providers[i] = a.WithMaps()
			}
		}
	}
	if *demo {
		providers = demoRegistry()
		fmt.Println("\n  -demo: SYNTHETIC providers. Numbers below are fabricated.")
	}
	// Replay short-circuits the credential and arm filtering below: it makes no
	// calls, so it needs no API key. Placed here deliberately -- when it sat
	// after the "no providers configured" check it refused to run without a key
	// it never uses, which is the opposite of the point.
	if *replay {
		obs, active, err := loadReplay(*outDir, providers)
		if err != nil {
			fatal("replay: %v", err)
		}
		fmt.Printf("\n  REPLAY: %d saved observation(s), no API calls, no cost\n\n", len(obs))
		for _, o := range obs {
			status := "ok"
			if !o.OK() {
				status = "ERR"
			}
			fmt.Printf("  %-4s %-14s %-22s fitz=%-4s t%d  %6dms  %s\n",
				status, o.Provider, o.Image, o.Fitz, o.Trial, o.Latency.Milliseconds(), summarise(o))
		}
		path := filepath.Join(*outDir, "report.md")
		if err := writeReport(path, obs, active, *trials); err != nil {
			fatal("write report: %v", err)
		}
		fmt.Printf("\nwrote %s\n", path)
		return
	}

	// -only exists because the two AILab arms share one API key, so having the
	// key necessarily configures both. Pro costs 4.7x Basic per call, and the
	// question Basic has to answer first -- does it return four severity levels
	// or two -- is answerable without spending anything on Pro. If Basic fails
	// that, Pro's numbers are the only ones that matter and you run it then.
	//
	// A flag rather than commenting out a registry line: the arms in a run are
	// part of the result, and an edit to source to change them leaves no trace
	// in the report.
	wanted := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			wanted[n] = true
		}
	}

	var active []Provider
	for _, p := range providers {
		switch {
		case len(wanted) > 0 && !wanted[p.Name()]:
			fmt.Printf("  skip  %-14s not in -only\n", p.Name())
		case !p.Configured():
			fmt.Printf("  skip  %-14s no credentials in env\n", p.Name())
		default:
			active = append(active, p)
		}
	}

	for n := range wanted {
		found := false
		for _, p := range providers {
			if p.Name() == n {
				found = true
			}
		}
		if !found {
			fatal("-only names unknown arm %q; known arms: %s", n, providerNames(providers))
		}
	}
	if len(active) == 0 {
		fatal("no providers configured -- set at least one of AILAB_API_KEY, PERFECTCORP_API_KEY+PERFECTCORP_SECRET, ANTHROPIC_API_KEY")
	}

	if err := os.MkdirAll(filepath.Join(*outDir, "raw"), 0o755); err != nil {
		fatal("mkdir: %v", err)
	}

	fmt.Printf("\n%d image(s) x %d provider(s) x %d trial(s) = %d calls\n\n",
		len(entries), len(active), *trials, len(entries)*len(active)**trials)

	var obs []Observation
	for _, e := range entries {
		jpeg, err := os.ReadFile(filepath.Join(*imgDir, e.File))
		if err != nil {
			fmt.Printf("  !!    %s: %v\n", e.File, err)
			continue
		}
		for _, p := range active {
			for t := 1; t <= *trials; t++ {
				demoFitz = e.Fitz
				o := runOne(p, e, jpeg, t, *timeout, *outDir, *saveRaw)
				obs = append(obs, o)

				status := "ok"
				if !o.OK() {
					status = "ERR"
				}
				fmt.Printf("  %-4s %-14s %-22s fitz=%-4s t%d  %6dms  %s\n",
					status, p.Name(), e.File, e.Fitz, t, o.Latency.Milliseconds(), summarise(o))
			}
		}
	}

	// Written before the report: if report generation panics on a shape nobody
	// anticipated, the paid-for responses are still replayable.
	if err := saveObservations(*outDir, obs); err != nil {
		fmt.Printf("  !!    could not save observations.json (replay unavailable): %v\n", err)
	}

	path := filepath.Join(*outDir, "report.md")
	if err := writeReport(path, obs, active, *trials); err != nil {
		fatal("write report: %v", err)
	}
	fmt.Printf("\nwrote %s\n", path)
}

func runOne(p Provider, e manifestEntry, jpeg []byte, trial int, timeout time.Duration, outDir string, saveRaw bool) Observation {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	rep, raw, err := p.Analyze(ctx, jpeg)
	elapsed := time.Since(start)

	if saveRaw && len(raw) > 0 {
		name := fmt.Sprintf("%s__%s__t%d.json", p.Name(), strings.TrimSuffix(e.File, filepath.Ext(e.File)), trial)
		_ = os.WriteFile(filepath.Join(outDir, "raw", name), raw, 0o644)
	}

	return Observation{
		Provider: p.Name(), Image: e.File, Fitz: e.Fitz, Trial: trial,
		Report: rep, Latency: elapsed, Err: err,
		RawBytes: len(raw), ReceivedAt: time.Now(),
	}
}

func summarise(o Observation) string {
	if !o.OK() {
		if errors.Is(o.Err, ErrNotConfigured) {
			return "not configured"
		}
		return truncate([]byte(o.Err.Error()), 90)
	}
	got, missing := o.Report.Coverage()
	s := fmt.Sprintf("score=%3d age=%3d cover=%d/6", o.Report.OverallScore, o.Report.SkinAge, got)
	if len(missing) > 0 {
		strs := make([]string, len(missing))
		for i, m := range missing {
			strs[i] = string(m)
		}
		s += "  MISSING: " + strings.Join(strs, ",")
	}
	return s
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nerror: "+format+"\n", args...)
	os.Exit(1)
}

func p95(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted)*95)/100 - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// fitzBand collapses Fitzpatrick types into the two groups the calibration
// question is actually about for this product.
func fitzBand(f string) string {
	switch strings.ToUpper(strings.TrimSpace(f)) {
	case "I", "II", "III":
		return "I-III"
	case "IV", "V", "VI":
		return "IV-VI"
	}
	return "unlabelled"
}
