package theme

import (
	"fmt"
	"math"
	"strconv"
)

// rgb8 is an sRGB color with 0-255 channels, kept as floats so conversions
// round once, at the end.
type rgb8 struct{ r, g, b float64 }

// hexRGB parses "#RRGGBB" (any case). ok is false for anything else.
func hexRGB(hex string) (rgb8, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return rgb8{}, false
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return rgb8{}, false
	}
	return rgb8{float64(v >> 16 & 0xFF), float64(v >> 8 & 0xFF), float64(v & 0xFF)}, true
}

// hex is c as "#RRGGBB", each channel clamped to 0-255 and rounded.
func (c rgb8) hex() string {
	ch := func(f float64) int { return int(math.Round(math.Max(0, math.Min(255, f)))) }
	return fmt.Sprintf("#%02X%02X%02X", ch(c.r), ch(c.g), ch(c.b))
}

// hslRGB converts hue (degrees), saturation and lightness (0-1) to sRGB.
func hslRGB(h, s, l float64) rgb8 {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	if s == 0 {
		return rgb8{l * 255, l * 255, l * 255}
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	hue := func(t float64) float64 {
		t = math.Mod(t+1, 1)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return rgb8{hue(h+1.0/3) * 255, hue(h) * 255, hue(h-1.0/3) * 255}
}

// cmykRGB is the naive CMYK to sRGB conversion (no ink profile): each
// channel is 255 × (1 − ink) × (1 − k), inks 0-1. Print colors only roughly
// match it.
func cmykRGB(c, m, y, k float64) rgb8 {
	return rgb8{255 * (1 - c) * (1 - k), 255 * (1 - m) * (1 - k), 255 * (1 - y) * (1 - k)}
}

// srgbLinear and linearSRGB convert one channel (0-1) between gamma-encoded
// sRGB and linear light.
func srgbLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// oklch is a color in OKLCH: lightness 0-1, chroma (0 to about 0.4) and hue
// in degrees.
type oklch struct{ l, c, h float64 }

// toOklch converts c to OKLCH (Björn Ottosson's OKLab, sRGB D65).
func (c rgb8) toOklch() oklch {
	r, g, b := srgbLinear(c.r/255), srgbLinear(c.g/255), srgbLinear(c.b/255)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	L := 0.2104542553*l + 0.7936177850*m - 0.0040720468*s
	A := 1.9779984951*l - 2.4285922050*m + 0.4505937099*s
	B := 0.0259040371*l + 0.7827717662*m - 0.8086757660*s
	h := math.Atan2(B, A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return oklch{L, math.Hypot(A, B), h}
}

// linear is o in linear sRGB; channels outside 0-1 are out of gamut.
func (o oklch) linear() (r, g, b float64) {
	A, B := o.c*math.Cos(o.h*math.Pi/180), o.c*math.Sin(o.h*math.Pi/180)
	l := o.l + 0.3963377774*A + 0.2158037573*B
	m := o.l - 0.1055613458*A - 0.0638541728*B
	s := o.l - 0.0894841775*A - 1.2914855480*B
	l, m, s = l*l*l, m*m*m, s*s*s
	return 4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s
}

// rgb converts o to sRGB. An out-of-gamut color keeps its lightness and hue
// and loses chroma until it fits.
func (o oklch) rgb() rgb8 {
	o.l = math.Max(0, math.Min(1, o.l))
	in := func(v float64) bool { return v >= -1e-4 && v <= 1+1e-4 }
	r, g, b := o.linear()
	for i := 0; i < 200 && !(in(r) && in(g) && in(b)); i++ {
		o.c *= 0.95
		r, g, b = o.linear()
	}
	enc := func(v float64) float64 { return 255 * linearSRGB(math.Max(0, math.Min(1, v))) }
	return rgb8{enc(r), enc(g), enc(b)}
}

// cieLab converts c to CIE L*a*b* (D65).
func (c rgb8) cieLab() (l, a, b float64) {
	r, g, bl := srgbLinear(c.r/255), srgbLinear(c.g/255), srgbLinear(c.b/255)
	x := (0.4124*r + 0.3576*g + 0.1805*bl) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*bl
	z := (0.0193*r + 0.1192*g + 0.9505*bl) / 1.08883
	f := func(t float64) float64 {
		if t > 216.0/24389 {
			return math.Cbrt(t)
		}
		return (24389.0/27*t + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

// DeltaE is the CIE76 color difference of two "#RRGGBB" colors (about 2 is
// just noticeable; TestStateHuesDistinct wants 20 between state colors). It is
// -1 when either is not a hex color.
func DeltaE(x, y string) float64 {
	c1, ok1 := hexRGB(x)
	c2, ok2 := hexRGB(y)
	if !ok1 || !ok2 {
		return -1
	}
	l1, a1, b1 := c1.cieLab()
	l2, a2, b2 := c2.cieLab()
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}
