// Command maps extracts the base64 face_maps from a saved AILab response.
//
// Written to answer one question the JSON cannot: what are these images
// actually of? Whether they are full-frame or cropped, pre-coloured or a bare
// mask, decides how the client composites them -- and whether Ayna's own
// cool-hued heatmap palette applies at all or is overridden by the vendor's.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	in := flag.String("in", "", "saved AILab JSON response")
	out := flag.String("out", "results/maps", "directory to write images into")
	flag.Parse()

	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal("read: %v", err)
	}

	var parsed struct {
		Result struct {
			FaceMaps map[string]string `json:"face_maps"`
		} `json:"result"`
		FaceMaps map[string]string `json:"face_maps"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		fatal("parse: %v", err)
	}

	faceMaps := parsed.Result.FaceMaps
	if len(faceMaps) == 0 {
		faceMaps = parsed.FaceMaps
	}
	if len(faceMaps) == 0 {
		fatal("no face_maps in %s", *in)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal("mkdir: %v", err)
	}

	for name, b64 := range faceMaps {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			fmt.Printf("  %-26s DECODE FAILED: %v\n", name, err)
			continue
		}

		// Sniffed rather than assumed: the extension has to match what the
		// bytes actually are, or every downstream viewer lies about the format.
		ext := ".bin"
		switch {
		case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8:
			ext = ".jpg"
		case len(data) > 8 && string(data[1:4]) == "PNG":
			ext = ".png"
		}

		dims := "?"
		if cfg, format, err := image.DecodeConfig(strings.NewReader(string(data))); err == nil {
			dims = fmt.Sprintf("%dx%d %s", cfg.Width, cfg.Height, format)
		}

		path := filepath.Join(*out, name+ext)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fatal("write %s: %v", path, err)
		}
		fmt.Printf("  %-26s %7d bytes  %-16s %s\n", name, len(data), dims, path)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
