//go:build ignore

// Generates the menu bar icons, as droid-proxy draws its own:
//
//	go run gen.go
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

type vec struct{ x, y float64 }

func (a vec) sub(b vec) vec       { return vec{a.x - b.x, a.y - b.y} }
func (a vec) dot(b vec) float64   { return a.x*b.x + a.y*b.y }
func (a vec) len() float64        { return math.Hypot(a.x, a.y) }
func (a vec) scale(s float64) vec { return vec{a.x * s, a.y * s} }

// shape reports whether a point (in pixel units) is inside.
type shape func(p vec) bool

func disc(c vec, r float64) shape { return func(p vec) bool { return p.sub(c).len() <= r } }

func ring(c vec, r, w float64) shape {
	return func(p vec) bool { return math.Abs(p.sub(c).len()-r) <= w/2 }
}

func capsule(a, b vec, w float64) shape {
	return func(p vec) bool {
		ab, ap := b.sub(a), p.sub(a)
		t := math.Max(0, math.Min(1, ap.dot(ab)/ab.dot(ab)))
		return p.sub(a.add(ab.scale(t))).len() <= w/2
	}
}

func (a vec) add(b vec) vec { return vec{a.x + b.x, a.y + b.y} }

func triangle(a, b, c vec) shape {
	edge := func(p, q, r vec) float64 { return (q.x-p.x)*(r.y-p.y) - (q.y-p.y)*(r.x-p.x) }
	return func(p vec) bool {
		d1, d2, d3 := edge(a, b, p), edge(b, c, p), edge(c, a, p)
		neg := d1 < 0 || d2 < 0 || d3 < 0
		pos := d1 > 0 || d2 > 0 || d3 > 0
		return !(neg && pos)
	}
}

func union(shapes ...shape) shape {
	return func(p vec) bool {
		for _, s := range shapes {
			if s(p) {
				return true
			}
		}
		return false
	}
}

func minus(a, b shape) shape { return func(p vec) bool { return a(p) && !b(p) } }

// coverage renders a shape with 4x4 supersampling.
func coverage(size int, s shape) []float64 {
	out := make([]float64, size*size)
	const n = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			hits := 0
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					if s(vec{float64(x) + (float64(sx)+0.5)/n, float64(y) + (float64(sy)+0.5)/n}) {
						hits++
					}
				}
			}
			out[y*size+x] = float64(hits) / (n * n)
		}
	}
	return out
}

// gauge is a dial with a needle; angle is degrees clockwise from straight up.
// Units are fractions of the canvas so it scales to any size.
func gauge(size, angle, ringW, needleW, hubR float64) shape {
	c := vec{size / 2, size / 2}
	r := size * 0.31
	rad := angle * math.Pi / 180
	tip := c.add(vec{math.Sin(rad), -math.Cos(rad)}.scale(r * 0.68))
	return union(ring(c, r, ringW), capsule(c, tip, needleW), disc(c, hubR))
}

func warningTriangle(size float64) shape {
	u := size / 48
	tri := triangle(vec{24 * u, 7.5 * u}, vec{6.5 * u, 39.5 * u}, vec{41.5 * u, 39.5 * u})
	mark := union(capsule(vec{24 * u, 18 * u}, vec{24 * u, 28 * u}, 4*u), disc(vec{24 * u, 34 * u}, 2.3*u))
	return minus(tri, mark)
}

func writeTemplate(path string, s shape, size int) {
	cov := coverage(size, s)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i, c := range cov {
		img.Pix[i*4+3] = uint8(math.Round(c * 255))
	}
	writePNG(path, img)
}

// writeColored is a non-template icon in one color, for states the menu bar
// tint would hide.
func writeColored(path string, s shape, size int, c color.NRGBA) {
	cov := coverage(size, s)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i, v := range cov {
		img.Pix[i*4+0], img.Pix[i*4+1], img.Pix[i*4+2] = c.R, c.G, c.B
		img.Pix[i*4+3] = uint8(math.Round(v * 255))
	}
	writePNG(path, img)
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func main() {
	// 48px renders at 24pt (the menu bar thickness) on Retina displays.
	const size = 48
	writeTemplate("ok.png", gauge(size, 45, 3.2, 3.6, 3.4), size)
	writeTemplate("warning.png", gauge(size, -45, 3.2, 3.6, 3.4), size)
	writeTemplate("unknown.png", gauge(size, 0, 3.2, 3.6, 3.4), size)
	writeTemplate("danger.png", warningTriangle(size), size)
	// macOS systemRed.
	writeColored("expiring.png", gauge(size, 45, 3.2, 3.6, 3.4), size, color.NRGBA{0xff, 0x3b, 0x30, 0xff})
}
