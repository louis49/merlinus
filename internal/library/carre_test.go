package library

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func uni(w, h int, c color.RGBA) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetRGBA(x, y, c)
		}
	}
	return im
}

// Image carree : simple redimensionnement, couleurs intactes (pas de voile).
func TestCarre128Carre(t *testing.T) {
	out := carre128(uni(300, 300, color.RGBA{0, 0, 255, 255}))
	if p := out.RGBAAt(0, 0); p.B < 250 || p.R > 5 {
		t.Fatalf("coin = %v, attendu bleu pur", p)
	}
}

// Image 16:9 : posee entiere au centre (bande de 72 px de haut), fond flou
// assombri au-dessus et au-dessous, aucune deformation.
func TestCarre128Paysage(t *testing.T) {
	vert := color.RGBA{0, 200, 0, 255}
	out := carre128(uni(560, 315, vert))
	if b := out.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
		t.Fatalf("taille %v", b)
	}
	centre, fond := out.RGBAAt(64, 64), out.RGBAAt(64, 10)
	if centre.G < 195 {
		t.Errorf("centre = %v, attendu le vert d'origine", centre)
	}
	if fond.G >= centre.G || fond.G < 120 {
		t.Errorf("fond = %v, attendu le meme vert, assombri (centre %v)", fond, centre)
	}
}

// R7 : une zone transparente devient blanche (et non noire) dans le JPEG.
func TestJPEG128DepuisDataURLTransparence(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64)) // entierement transparente
	for x := 24; x < 40; x++ {
		for y := 24; y < 40; y++ {
			img.Set(x, y, color.RGBA{200, 0, 0, 255}) // carre rouge opaque au centre
		}
	}
	var png_ bytes.Buffer
	if err := png.Encode(&png_, img); err != nil {
		t.Fatal(err)
	}
	u := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png_.Bytes())
	jpg, err := JPEG128DepuisDataURL(u)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatal(err)
	}
	if b := dec.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
		t.Fatalf("taille %v", b)
	}
	r, g, bl, _ := dec.At(4, 4).RGBA()
	if r>>8 < 240 || g>>8 < 240 || bl>>8 < 240 {
		t.Errorf("coin transparent rendu (%d,%d,%d), attendu blanc", r>>8, g>>8, bl>>8)
	}
	if _, err := JPEG128DepuisDataURL("pas une data url"); err == nil {
		t.Error("data URL invalide acceptee")
	}
}
