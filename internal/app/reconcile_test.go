package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
)

// travailDepuisJSON construit l'arbre de travail comme l'IHM l'envoie : la
// presence de la cle "child" distingue conteneur et feuille.
func travailDepuisJSON(t *testing.T, s string) []wNode {
	t.Helper()
	var w []wNode
	if err := json.Unmarshal([]byte(s), &w); err != nil {
		t.Fatalf("JSON de travail invalide : %v", err)
	}
	return w
}

// dataURL encode une image unie de la taille demandee en data URL (png ou jpeg).
func dataURL(t *testing.T, format string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// trouver cherche un noeud par uuid dans un arbre.
func trouver(ns []*playlist.Node, uuid string) *playlist.Node {
	for _, n := range ns {
		if n.UUID == uuid {
			return n
		}
		if r := trouver(n.Children, uuid); r != nil {
			return r
		}
	}
	return nil
}

func TestReconcilierLocalCascadeRem(t *testing.T) {
	travail := travailDepuisJSON(t, `[
		{"uuid":"cat","title":"Histoires","child":[
			{"uuid":"serie","title":"Serie","_rem":true,"child":[
				{"uuid":"h1","title":"H1"},
				{"uuid":"sous","title":"Sous","child":[{"uuid":"h2","title":"H2"}]},
				{"uuid":"vide","title":"Vide","child":[]}
			]},
			{"uuid":"h3","title":"H3"},
			{"uuid":"h4","title":"H4","_rem":true}
		]}
	]`)
	arbre, images, retirees, nouveaux, err := reconcilierLocal(travail)
	if err != nil {
		t.Fatalf("reconcilierLocal : %v", err)
	}
	if len(images) != 0 || len(nouveaux) != 0 {
		t.Fatalf("images=%d nouveaux=%v, attendu aucun", len(images), nouveaux)
	}
	// Seules les FEUILLES sous le retrait sont recensees (pas les conteneurs).
	attendu := map[string]bool{"h1": true, "h2": true, "h4": true}
	if len(retirees) != len(attendu) {
		t.Fatalf("retirees = %v, attendu %v", retirees, attendu)
	}
	for u := range attendu {
		if !retirees[u] {
			t.Errorf("feuille %q absente de retirees (%v)", u, retirees)
		}
	}
	if len(arbre) != 1 || len(arbre[0].Children) != 1 || arbre[0].Children[0].UUID != "h3" {
		t.Fatalf("arbre apres retrait inattendu : %+v", arbre)
	}
	for _, u := range []string{"serie", "h1", "sous", "h2", "vide", "h4"} {
		if trouver(arbre, u) != nil {
			t.Errorf("noeud retire %q toujours present", u)
		}
	}
}

// Nil vs [] preserves ; un conteneur dont tous les enfants sont retires reste
// un conteneur (child:[]), pas une feuille.
func TestReconcilierLocalConteneurVsFeuille(t *testing.T) {
	travail := travailDepuisJSON(t, `[
		{"uuid":"vide","title":"Vide","child":[]},
		{"uuid":"vide2","title":"Vide apres retrait","child":[{"uuid":"x","title":"X","_rem":true}]},
		{"uuid":"f","title":"Feuille","add_time":42,"limit_time":7}
	]`)
	arbre, _, _, _, err := reconcilierLocal(travail)
	if err != nil {
		t.Fatal(err)
	}
	if len(arbre) != 3 {
		t.Fatalf("attendu 3 noeuds, obtenu %d", len(arbre))
	}
	for _, n := range arbre[:2] {
		if n.Children == nil || len(n.Children) != 0 {
			t.Errorf("%s : Children = %#v, attendu slice vide non nil", n.UUID, n.Children)
		}
	}
	f := arbre[2]
	if f.Children != nil || f.AddTimeUnix != 42 || f.LimitTime == nil || *f.LimitTime != 7 {
		t.Errorf("feuille mal convertie : %+v", f)
	}
}

func TestReconcilierLocalTitreEtNouveaux(t *testing.T) {
	travail := travailDepuisJSON(t, `[
		{"uuid":"cat","title":"Ancien","_titre":"Nouveau","child":[
			{"uuid":"n1","title":"N1","_new":true},
			{"uuid":"n2","title":"N2","_new":true,"_titre":""},
			{"uuid":"n3","title":"N3","_new":true,"_rem":true}
		]}
	]`)
	arbre, _, retirees, nouveaux, err := reconcilierLocal(travail)
	if err != nil {
		t.Fatal(err)
	}
	if arbre[0].Title != "Nouveau" {
		t.Errorf("_titre non applique : %q", arbre[0].Title)
	}
	if n2 := trouver(arbre, "n2"); n2 == nil || n2.Title != "N2" {
		t.Errorf("_titre vide doit garder le titre : %+v", n2)
	}
	// Un noeud a la fois _new et _rem n'est pas a televerser.
	if len(nouveaux) != 2 || nouveaux[0] != "n1" || nouveaux[1] != "n2" {
		t.Errorf("nouveaux = %v, attendu [n1 n2]", nouveaux)
	}
	if !retirees["n3"] {
		t.Errorf("n3 absent de retirees : %v", retirees)
	}
	// Les champs d'edition ne doivent pas fuir dans le JSON boite.
	b, err := playlist.SerialiserBoite(arbre)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"_titre", "_rem", "_new", "_img"} {
		if bytes.Contains(b, []byte(k)) {
			t.Errorf("cle d'edition %q presente dans %s", k, b)
		}
	}
}

func TestReconcilierLocalImage(t *testing.T) {
	travail := []wNode{
		{UUID: "cat", Title: "Cat", Img: dataURL(t, "png", 300, 200), Child: &[]wNode{
			{UUID: "h", Title: "H", Img: dataURL(t, "jpeg", 64, 64)},
			{UUID: "r", Title: "R", Rem: true, Img: "pas une data url"}, // retire : image ignoree
		}},
	}
	_, images, _, _, err := reconcilierLocal(travail)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 {
		t.Fatalf("attendu 2 illustrations, obtenu %d", len(images))
	}
	vus := map[string]bool{}
	for _, im := range images {
		vus[im.uuid] = true
		cfg, format, err := image.DecodeConfig(bytes.NewReader(im.data))
		if err != nil || format != "jpeg" || cfg.Width != 128 || cfg.Height != 128 {
			t.Errorf("%s : format=%s %dx%d err=%v, attendu jpeg 128x128", im.uuid, format, cfg.Width, cfg.Height, err)
		}
	}
	if !vus["cat"] || !vus["h"] {
		t.Errorf("illustrations recensees : %v", vus)
	}
}

func TestReconcilierLocalImageInvalide(t *testing.T) {
	travail := []wNode{{UUID: "cat", Title: "Cat", Child: &[]wNode{}, Img: "data:image/png;base64,!!!"}}
	if _, _, _, _, err := reconcilierLocal(travail); err == nil {
		t.Fatal("data URL invalide acceptee")
	}
}

// Les categories systeme (masquees dans l'IHM) sont ignorees, meme renommees
// vers un libelle systeme ; leurs enfants ne sont ni recenses ni convertis.
func TestReconcilierLocalCategoriesSysteme(t *testing.T) {
	travail := travailDepuisJSON(t, `[
		{"uuid":"fav","title":"Merlin_favorite","favorite":1,"child":[{"uuid":"hf","title":"HF","_new":true}]},
		{"uuid":"d1","title":"Dernier ajouts","child":[{"uuid":"hd","title":"HD","_rem":true}]},
		{"uuid":"d2","title":"Autre","_titre":"Derniers ajouts","child":[]},
		{"uuid":"cat","title":"Histoires","child":[{"uuid":"h","title":"H"}]}
	]`)
	arbre, _, retirees, nouveaux, err := reconcilierLocal(travail)
	if err != nil {
		t.Fatal(err)
	}
	if len(arbre) != 1 || arbre[0].UUID != "cat" {
		t.Fatalf("arbre = %+v, attendu la seule categorie utilisateur", arbre)
	}
	if len(retirees) != 0 || len(nouveaux) != 0 {
		t.Fatalf("retirees=%v nouveaux=%v, attendu vides", retirees, nouveaux)
	}
}

func TestSyncDrapeauxBoite(t *testing.T) {
	reel := []*playlist.Node{
		{UUID: "fav", Favorite: 1, Children: []*playlist.Node{}},
		{UUID: "radio", Webradio: 1, Children: []*playlist.Node{
			{UUID: "st", Webradio: 1},
		}},
		{UUID: "cat", Children: []*playlist.Node{{UUID: "d"}}},
	}
	publie := []*playlist.Node{
		{UUID: "radio", Children: []*playlist.Node{{UUID: "st"}}}, // drapeaux perdus localement
		{UUID: "cat", Favorite: 1, Children: []*playlist.Node{ // favori pose localement sur un noeud connu
			{UUID: "d"},
			{UUID: "neuf", Webradio: 1}, // absent de la boite : inchange
		}},
	}
	syncDrapeauxBoite(publie, reel)
	verif := func(u string, fav, wr int) {
		t.Helper()
		n := trouver(publie, u)
		if n == nil || n.Favorite != fav || n.Webradio != wr {
			t.Errorf("%s : %+v, attendu favorite=%d webradio=%d", u, n, fav, wr)
		}
	}
	verif("radio", 0, 1)
	verif("st", 0, 1)
	verif("cat", 0, 0) // la boite fait foi pour un uuid connu
	verif("d", 0, 0)
	verif("neuf", 0, 1) // absent de la boite : drapeau local garde
}

func TestFeuilles(t *testing.T) {
	arbre := []*playlist.Node{
		{UUID: "vide", Children: []*playlist.Node{}},
		{UUID: "cat", Children: []*playlist.Node{
			{UUID: "h1"},
			{UUID: "serie", Children: []*playlist.Node{{UUID: "h2"}}},
		}},
		{UUID: "h3"},
	}
	got := playlist.Feuilles(arbre)
	if len(got) != 3 || !got["h1"] || !got["h2"] || !got["h3"] {
		t.Fatalf("feuilles = %v, attendu {h1 h2 h3}", got)
	}
	if got := playlist.Feuilles(nil); got == nil || len(got) != 0 {
		t.Fatalf("playlist.Feuilles(nil) = %v, attendu ensemble vide non nil", got)
	}
}

func TestImageDepuisDataURLValide(t *testing.T) {
	for _, c := range []struct {
		format string
		w, h   int
	}{{"png", 512, 512}, {"jpeg", 300, 150}, {"png", 16, 16}} {
		data, err := library.JPEG128DepuisDataURL(dataURL(t, c.format, c.w, c.h))
		if err != nil {
			t.Fatalf("%s %dx%d : %v", c.format, c.w, c.h, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" || cfg.Width != 128 || cfg.Height != 128 {
			t.Errorf("%s %dx%d -> %s %dx%d (err %v), attendu jpeg 128x128", c.format, c.w, c.h, format, cfg.Width, cfg.Height, err)
		}
	}
}

func TestImageDepuisDataURLInvalide(t *testing.T) {
	texte := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("bonjour"))
	for _, u := range []string{
		"",
		"pas une data url",
		"data:image/png;base64",         // pas de virgule
		"http://exemple/img.png,abc",    // pas le prefixe data:
		"data:image/png;base64,@@@@",    // base64 invalide
		texte,                           // decode, mais pas une image
		"data:image/png;base64,iVBORw0", // PNG tronque
	} {
		if _, err := library.JPEG128DepuisDataURL(u); err == nil {
			t.Errorf("data URL %q acceptee", u)
		}
	}
}
