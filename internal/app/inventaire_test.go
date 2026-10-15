package app

import "testing"

func TestFusionInventaire(t *testing.T) {
	inv := map[string]uint32{
		"aaa.mp3":      100, // reference -> conserve
		"aaa.jpg":      10,
		"bbb.mp3":      200, // non reference -> retire (menage)
		"bbb.jpg":      20,
		"playlist.bin": 999, // systeme -> preserve meme si non reference
		"fav.bin":      5,
	}
	ref := map[string]bool{"aaa": true, "ccc": true}
	televerses := map[string]int64{
		"aaa.mp3":                150, // taille mise a jour
		"ccc.mp3":                300, // nouveau media
		"ccc.jpg":                30,
		"playlist-merlinus.json": 4242, // non-media -> ignore
	}

	out := fusionInventaire(inv, ref, televerses)

	if out["aaa.mp3"] != 150 {
		t.Errorf("aaa.mp3 = %d, attendu 150 (taille televersee)", out["aaa.mp3"])
	}
	if _, ok := out["bbb.mp3"]; ok {
		t.Errorf("bbb.mp3 aurait du etre retire (uuid non reference)")
	}
	if _, ok := out["bbb.jpg"]; ok {
		t.Errorf("bbb.jpg aurait du etre retire (uuid non reference)")
	}
	if out["ccc.mp3"] != 300 || out["ccc.jpg"] != 30 {
		t.Errorf("nouveaux medias ccc non poses : %v / %v", out["ccc.mp3"], out["ccc.jpg"])
	}
	if out["playlist.bin"] != 999 || out["fav.bin"] != 5 {
		t.Errorf("fichiers systeme non preserves : %v / %v", out["playlist.bin"], out["fav.bin"])
	}
	if _, ok := out["playlist-merlinus.json"]; ok {
		t.Errorf("le JSON de playlist (non-media) n'aurait pas du entrer dans l'inventaire")
	}
}

func TestFusionInventaireNil(t *testing.T) {
	out := fusionInventaire(nil, map[string]bool{"x": true}, map[string]int64{"x.mp3": 42})
	if out["x.mp3"] != 42 {
		t.Errorf("inventaire nil : x.mp3 = %d, attendu 42", out["x.mp3"])
	}
}
