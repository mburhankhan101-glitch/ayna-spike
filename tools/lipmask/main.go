// Command lipmask blanks the mouth out of a redness overlay.
//
// Lips are the reddest thing on a face and are supposed to be. AILab's
// `red_area` map dutifully marks them as the most intense region, so overlaying
// it unedited tells a user their worst redness is their mouth -- which is both
// wrong and slightly insulting, since it is the one red patch they were never
// going to treat.
//
// There is no mouth rectangle in the response, so the mouth is located from the
// two eye rectangles that ARE there. The eye line gives position, scale and
// roll all at once: the mouth sits about 1.1 interocular distances below the
// midpoint, along the perpendicular. That handles a tilted head for free.
//
// It does NOT handle yaw -- a face turned away from the camera puts the mouth
// off the perpendicular, and the mask drifts. The ellipse is deliberately
// generous for that reason: hiding a little genuine chin redness is a far
// cheaper error than leaving the lips lit up.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
)

type rect struct {
	Left, Top, Width, Height int
}

func (r rect) center() (float64, float64) {
	return float64(r.Left) + float64(r.Width)/2,
		float64(r.Top) + float64(r.Height)/2
}

func (r rect) ok() bool { return r.Width > 0 && r.Height > 0 }

func main() {
	in := flag.String("in", "", "saved AILab JSON response")
	out := flag.String("out", "results/maps/red_area_masked.jpg", "output image")
	flag.Parse()

	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal("read: %v", err)
	}

	var parsed struct {
		Result struct {
			FaceMaps map[string]string `json:"face_maps"`

			// Nested under dark_circle_mark, not on result. Found by grepping
			// the saved response rather than assuming -- the docs describe
			// field names without their parents, which has now cost a wrong
			// path three times in this codebase.
			DarkCircleMark struct {
				LeftEye  rect `json:"left_eye_rect"`
				RightEye rect `json:"right_eye_rect"`
			} `json:"dark_circle_mark"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		fatal("parse: %v", err)
	}

	b64, ok := parsed.Result.FaceMaps["red_area"]
	if !ok {
		fatal("no red_area map in %s", *in)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		fatal("decode: %v", err)
	}

	src, err := jpeg.Decode(bytesReader(data))
	if err != nil {
		fatal("decode jpeg: %v", err)
	}

	l := parsed.Result.DarkCircleMark.LeftEye
	r := parsed.Result.DarkCircleMark.RightEye
	if !l.ok() || !r.ok() {
		fatal("both eye rects are required to locate the mouth")
	}

	lx, ly := l.center()
	rx, ry := r.center()

	// The eye line: its length is the scale, its angle is the head roll.
	dx, dy := rx-lx, ry-ly
	d := math.Hypot(dx, dy)
	if d < 1 {
		fatal("eyes are coincident; cannot derive scale")
	}

	// Down the face is perpendicular to the eye line, rotated the same way the
	// head is. Using the raw vertical instead would slide the mask off the
	// mouth the moment anyone tilts their head.
	px, py := -dy/d, dx/d
	if py < 0 {
		px, py = -px, -py // keep it pointing down the image
	}

	mx := (lx+rx)/2 + px*mouthDrop*d
	my := (ly+ry)/2 + py*mouthDrop*d

	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)

	// White, because this map is drawn on white and white is what the client
	// treats as "nothing here". Filling with black would composite as a bruise.
	fillEllipse(dst, mx, my, mouthWidth*d, mouthHeight*d, color.White)

	f, err := os.Create(*out)
	if err != nil {
		fatal("create: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, dst, &jpeg.Options{Quality: 92}); err != nil {
		fatal("encode: %v", err)
	}

	fmt.Printf("eyes  L(%.0f,%.0f) R(%.0f,%.0f)  interocular %.0fpx\n", lx, ly, rx, ry, d)
	fmt.Printf("mouth (%.0f,%.0f)  mask %.0fx%.0f\n",
		mx, my, mouthWidth*d*2, mouthHeight*d*2)
	fmt.Printf("wrote %s\n", *out)
}

// Anthropometric ratios, expressed in interocular distances so they scale with
// the face rather than the image.
//
// Generous on purpose. Hiding a little genuine chin redness costs almost
// nothing; leaving the lips lit costs the credibility of the whole overlay.
const (
	mouthDrop   = 1.20 // below the eye line
	mouthWidth  = 0.55 // semi-axis
	mouthHeight = 0.38 // semi-axis
)

func fillEllipse(img *image.RGBA, cx, cy, rx, ry float64, c color.Color) {
	b := img.Bounds()
	for y := int(cy - ry); y <= int(cy+ry); y++ {
		for x := int(cx - rx); x <= int(cx+rx); x++ {
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			nx := (float64(x) - cx) / rx
			ny := (float64(y) - cy) / ry
			if nx*nx+ny*ny <= 1 {
				img.Set(x, y, c)
			}
		}
	}
}

func bytesReader(b []byte) *os.File {
	// jpeg.Decode wants an io.Reader; a temp file keeps this tool dependency
	// free without pulling bytes.Reader into the import list twice.
	f, err := os.CreateTemp("", "map*.jpg")
	if err != nil {
		fatal("temp: %v", err)
	}
	if _, err := f.Write(b); err != nil {
		fatal("temp write: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		fatal("temp seek: %v", err)
	}
	return f
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
