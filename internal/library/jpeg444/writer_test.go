package jpeg444

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

// sof0 renvoie les facteurs d'echantillonnage (h<<4|v) de chaque composante
// du marqueur SOF0 ; echoue si le fichier n'est pas en baseline.
func sof0(t *testing.T, d []byte) []byte {
	t.Helper()
	for i := 2; i+4 <= len(d); {
		if d[i] != 0xff {
			t.Fatalf("marqueur attendu a l'octet %d", i)
		}
		m, l := d[i+1], int(binary.BigEndian.Uint16(d[i+2:]))
		switch m {
		case 0xc0:
			n := int(d[i+9])
			f := make([]byte, n)
			for k := 0; k < n; k++ {
				f[k] = d[i+11+3*k]
			}
			return f
		case 0xc1, 0xc2, 0xda:
			t.Fatalf("pas de SOF0 baseline (marqueur %#x)", m)
		}
		i += 2 + l
	}
	t.Fatal("SOF0 absent")
	return nil
}

// mire : aplat orange traverse de traits violets d'un pixel, le cas ou le 4:2:0
// bave (franges de couleur).
func mire(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0xff, 0x70, 0x40, 0xff}
			if x%5 == 0 || y%7 == 0 {
				c = color.RGBA{0x50, 0x20, 0xf0, 0xff}
			}
			m.Set(x, y, c)
		}
	}
	return m
}

// psnr entre deux images de meme taille (canaux R, G, B).
func psnr(a, b image.Image) float64 {
	var se float64
	n := 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			for _, d := range []float64{float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8), float64(b1>>8) - float64(b2>>8)} {
				se += d * d
				n++
			}
		}
	}
	return 10 * math.Log10(255*255/(se/float64(n)))
}

// Baseline 4:4:4, relu par le decodeur standard, nettement plus fidele que le
// 4:2:0 de image/jpeg a qualite egale ; tailles non multiples de 8 comprises.
func TestEncode444(t *testing.T) {
	for _, sz := range []image.Point{{128, 128}, {13, 21}, {1, 1}} {
		src := mire(sz.X, sz.Y)
		var b444, b420 bytes.Buffer
		if err := Encode(&b444, src, &Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(&b420, src, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		if f := sof0(t, b444.Bytes()); !bytes.Equal(f, []byte{0x11, 0x11, 0x11}) {
			t.Fatalf("%v : echantillonnage %x, 4:4:4 attendu", sz, f)
		}
		d444, err := jpeg.Decode(bytes.NewReader(b444.Bytes()))
		if err != nil {
			t.Fatalf("%v : illisible par image/jpeg : %v", sz, err)
		}
		if d444.Bounds().Size() != sz {
			t.Fatalf("%v : taille relue %v", sz, d444.Bounds().Size())
		}
		d420, _ := jpeg.Decode(bytes.NewReader(b420.Bytes()))
		p444, p420 := psnr(src, d444), psnr(src, d420)
		if p444 < 30 || p444 < p420+5 {
			t.Errorf("%v : PSNR 4:4:4 %.1f dB, 4:2:0 %.1f dB", sz, p444, p420)
		}
	}
}

// Niveaux de gris : chemin inchange (une composante, 1x1).
func TestEncodeGris(t *testing.T) {
	g := image.NewGray(image.Rect(0, 0, 16, 16))
	var b bytes.Buffer
	if err := Encode(&b, g, nil); err != nil {
		t.Fatal(err)
	}
	if f := sof0(t, b.Bytes()); !bytes.Equal(f, []byte{0x11}) {
		t.Fatalf("echantillonnage %x", f)
	}
}
