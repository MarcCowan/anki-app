// Command genicons draws the app icons (two offset index cards, the front one
// with a red rule) as PNGs, using only the standard library.
//
//	cd server && go run ./cmd/genicons            # writes ../web/public/icons
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

var (
	desk     = color.RGBA{0x14, 0x1a, 0x21, 0xff}
	backCard = color.RGBA{0x86, 0xa8, 0xf2, 0xff}
	card     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ruleRed  = color.RGBA{0xd2, 0x46, 0x3f, 0xff}
	lineBlue = color.RGBA{0xc9, 0xd6, 0xe8, 0xff}
)

// rect is in unit coordinates (0..1 of the icon size).
type rect struct{ x0, y0, x1, y1, r float64 }

func (q rect) contains(x, y float64) bool {
	if x < q.x0 || x > q.x1 || y < q.y0 || y > q.y1 {
		return false
	}
	cx := min(max(x, q.x0+q.r), q.x1-q.r)
	cy := min(max(y, q.y0+q.r), q.y1-q.r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= q.r*q.r
}

type layer struct {
	shape rect
	c     color.RGBA
}

// render draws the icon at size×size with 4×4 supersampling. Everything sits
// inside the central 70% so the 512 icon is safe as a maskable icon.
func render(size int) *image.RGBA {
	layers := []layer{
		{rect{0.24, 0.20, 0.80, 0.62, 0.035}, backCard},
		{rect{0.18, 0.34, 0.76, 0.78, 0.035}, card},
		{rect{0.18, 0.425, 0.76, 0.437, 0}, ruleRed},
		{rect{0.24, 0.52, 0.70, 0.528, 0}, lineBlue},
		{rect{0.24, 0.60, 0.70, 0.608, 0}, lineBlue},
		{rect{0.24, 0.68, 0.58, 0.688, 0}, lineBlue},
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := range size {
		for px := range size {
			var r, g, b float64
			for sy := range ss {
				for sx := range ss {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					c := desk
					for _, l := range layers {
						if l.shape.contains(x, y) {
							c = l.c
						}
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
				}
			}
			n := float64(ss * ss)
			img.SetRGBA(px, py, color.RGBA{uint8(r/n + 0.5), uint8(g/n + 0.5), uint8(b/n + 0.5), 0xff})
		}
	}
	return img
}

func main() {
	out := flag.String("out", filepath.Join("..", "web", "public", "icons"), "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for name, size := range map[string]int{"icon-192.png": 192, "icon-512.png": 512, "apple-touch-icon.png": 180} {
		if err := write(filepath.Join(*out, name), render(size)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", filepath.Join(*out, name))
	}
}

func write(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
