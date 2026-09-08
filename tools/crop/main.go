// Command crop cuts a square around a face and writes it back out as JPEG.
//
// Exists to test one hypothesis cheaply: whether AILab's acne_score is blind to
// moderate acne, or merely blind to acne that occupies 4% of the pixels it was
// handed. On face5 the vendor's own face_rectangle was 213x214 inside an
// 826x1280 frame. If the service downsamples before analysing, a lesion a few
// pixels across is gone before the model sees it.
//
// Cropping to the vendor's reported box rather than by eye keeps the test
// reproducible and removes "you framed it differently" as an explanation for
// whatever comes back.
//
//	go run ./tools/crop -in images-acne/face5.jpeg -out images-acne-tight/face5.jpeg \
//	    -left 333 -top 649 -w 213 -h 214 -scale 2.4
package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
)

func main() {
	var (
		in    = flag.String("in", "", "source JPEG")
		out   = flag.String("out", "", "destination JPEG")
		left  = flag.Int("left", 0, "face_rectangle.left")
		top   = flag.Int("top", 0, "face_rectangle.top")
		w     = flag.Int("w", 0, "face_rectangle.width")
		h     = flag.Int("h", 0, "face_rectangle.height")
		scale = flag.Float64("scale", 2.4, "how many face-widths the crop should span")
	)
	flag.Parse()

	src, err := os.Open(*in)
	if err != nil {
		fatal("open: %v", err)
	}
	defer src.Close()

	img, err := jpeg.Decode(src)
	if err != nil {
		fatal("decode: %v", err)
	}

	b := img.Bounds()

	// Square, centred on the face. A square keeps the aspect ratio the model
	// saw during training closer to what a portrait crop produces, and avoids
	// introducing a second variable alongside the zoom.
	side := int(float64(max(*w, *h)) * *scale)
	cx := *left + *w/2
	cy := *top + *h/2

	x0, y0 := cx-side/2, cy-side/2
	x1, y1 := x0+side, y0+side

	// Clamp to the image rather than padding. Padding would add invented
	// pixels to a test about what the model can see.
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		fatal("this JPEG decodes to a type that cannot be sub-imaged")
	}
	cropped := si.SubImage(image.Rect(x0, y0, x1, y1))

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal("mkdir: %v", err)
	}
	dst, err := os.Create(*out)
	if err != nil {
		fatal("create: %v", err)
	}
	defer dst.Close()

	// Quality 95: high enough that compression artefacts cannot be blamed for
	// a changed score, which is the whole point of the exercise.
	if err := jpeg.Encode(dst, cropped, &jpeg.Options{Quality: 95}); err != nil {
		fatal("encode: %v", err)
	}

	srcPx := b.Dx() * b.Dy()
	facePx := *w * *h
	outPx := (x1 - x0) * (y1 - y0)

	fmt.Printf("in   %dx%d, face is %.1f%% of pixels\n",
		b.Dx(), b.Dy(), 100*float64(facePx)/float64(srcPx))
	fmt.Printf("out  %dx%d, face is %.1f%% of pixels\n",
		x1-x0, y1-y0, 100*float64(facePx)/float64(outPx))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
