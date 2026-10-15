package radiofrance

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
)

const (
	idEp1 = "0a1b2c3d-0000-4000-8000-000000000001"
	idEp2 = "0a1b2c3d-0000-4000-8000-000000000002"
	idEp3 = "0a1b2c3d-0000-4000-8000-000000000003"
)

// episodeRF construit un objet episode tel que le porte le __data.json.
func episodeRF(id, titre, texte string, duree float64, visuel, date string) map[string]any {
	o := map[string]any{
		"type":       "episode",
		"id":         id,
		"titleProps": map[string]any{"title": titre, "text": texte},
	}
	if duree > 0 {
		o["manifestation"] = map[string]any{"duration": duree}
	}
	if visuel != "" {
		o["visual"] = map[string]any{"id": visuel}
	}
	if date != "" {
		o["publishedDate"] = date
	}
	return o
}

func TestParserPageSerie(t *testing.T) {
	page := pageData(t, map[string]any{
		"concept": map[string]any{
			"expressionCount": 3.0,
			"expressions": map[string]any{
				"items": []any{
					episodeRF(idEp1, "Episode 1 &amp; demi", "Il &eacute;tait une fois", 900, "vis-1", "2024-01-01"),
					episodeRF(idEp2, "Episode 2", "Suite", 0, "", ""),
					// Doublon d'id : ignore.
					episodeRF(idEp1, "Doublon", "", 900, "", ""),
					// Id non UUID : ignore.
					episodeRF("pas-un-uuid", "Invalide", "", 900, "", ""),
					// Type different : ignore.
					map[string]any{"type": "concept", "id": idEp3, "titleProps": map[string]any{"title": "Serie"}},
					// Bande-annonce : ecartee par estPromoRF.
					episodeRF(idEp3, "Bande-annonce de la saison", "", 60, "", ""),
				},
			},
		},
	})
	vus := map[string]bool{}
	eps := parserPageSerie(page, nil, vus)
	attendu := []epSerie{
		{id: idEp1, title: "Episode 1 & demi", desc: "Il \u00e9tait une fois", visual: "vis-1", pub: "2024-01-01", dur: 900},
		{id: idEp2, title: "Episode 2", desc: "Suite"},
	}
	if !reflect.DeepEqual(eps, attendu) {
		t.Fatalf("episodes :\n%+v\nattendu :\n%+v", eps, attendu)
	}
	// L'id d'une promo ecartee est quand meme marque vu (vus est positionne
	// avant le filtre) : une page suivante ne la reproposera pas.
	if !vus[idEp3] {
		t.Errorf("id de la promo non marque vu")
	}

	// Page suivante qui repete les memes episodes : rien de nouveau (fin de
	// pagination detectee par serieEpisodes).
	if eps2 := parserPageSerie(page, eps, vus); len(eps2) != len(eps) {
		t.Errorf("page repetee : %d episodes, attendu %d", len(eps2), len(eps))
	}

	// JSON invalide, noeuds null ou data non tableau : eps inchange, sans panique.
	for _, raw := range []string{
		"", "pas du json", `{"nodes":null}`, `{"nodes":[null,{"data":null},{"data":{"a":1}},{"data":"x"}]}`,
	} {
		if got := parserPageSerie(raw, eps, map[string]bool{}); !reflect.DeepEqual(got, eps) {
			t.Errorf("%q : eps modifie : %+v", raw, got)
		}
	}
}

func TestEstPromoRF(t *testing.T) {
	cas := []struct {
		titre, desc string
		dur         int
		promo       bool
	}{
		{"Bande annonce", "", 600, true},
		{"Episode 1", "Le TEASER de la saison", 0, true},
		{"Trailer", "", 0, true},
		{"Info", "Retrouvez tous les \u00e9pisodes sur l\u2019appli Radio France", 0, true},
		{"D\u00e9couvrez le nouveau podcast", "", 50, true},
		{"Decouvrez", "", 119, true},
		{"D\u00e9couvrez la mer", "", 120, false}, // pas assez court
		{"D\u00e9couvrez la mer", "", 0, false},   // duree inconnue
		{"Episode court", "", 30, false},          // court sans mot-cle
		{"Le loup", "Une histoire", 900, false},
	}
	for _, c := range cas {
		if got := estPromoRF(c.titre, c.desc, c.dur); got != c.promo {
			t.Errorf("estPromoRF(%q, %q, %d) = %v, attendu %v", c.titre, c.desc, c.dur, got, c.promo)
		}
	}
}

func TestCleEpisodeRF(t *testing.T) {
	cas := map[[2]string]string{
		{"https://media.radiofrance-podcast.net/podcast09/ITEMA_1-2022M1E0002-21.mp3?id=x", "d"}: "rf:file:ITEMA_1-2022M1E0002-21",
		{"https://cdn.test/a/b/_RSCE-21-abc.m4a", "d"}:                                           "rf:file:_RSCE-21-abc",
		{"fichier.mp3", "d"}:                    "rf:file:fichier",
		{"", "id-aod"}:                          "rf:aod:id-aod",
		{"https://cdn.test/dossier/", "id-aod"}: "rf:aod:id-aod",
	}
	for in, attendu := range cas {
		if got := cleEpisodeRF(in[0], in[1]); got != attendu {
			t.Errorf("cleEpisodeRF(%q, %q) = %q, attendu %q", in[0], in[1], got, attendu)
		}
	}
}

func TestEditeurEtImageCarte(t *testing.T) {
	if editeur("franceinter") != "France Inter" || editeur("mouv") != "Mouv'" || editeur("inconnu") != "Radio France" || editeur(nil) != "Radio France" {
		t.Error("editeur : libelles inattendus")
	}
	if got := imageCarte(map[string]any{"visual": map[string]any{"id": "v"}, "poster": map[string]any{"id": "p"}}); got != "https://www.radiofrance.fr/pikapi/images/v/400x400?webp=true" {
		t.Errorf("imageCarte (repli visual) = %q", got)
	}
	if got := imageCarte(map[string]any{"square": map[string]any{"id": ""}, "portrait": map[string]any{"id": "p"}}); got != "https://www.radiofrance.fr/pikapi/images/p/400x400?webp=true" {
		t.Errorf("imageCarte (id vide ignore) = %q", got)
	}
	if imageCarte(map[string]any{"square": "pas un objet"}) != "" {
		t.Error("imageCarte : vide attendu")
	}
}

func TestTexteRechercheEtPayload(t *testing.T) {
	if got := texteRecherche("<em>Les</em>&nbsp;histoires  de   <b>P&#39;tit</b> loup "); got != "Les histoires de P'tit loup" {
		t.Errorf("texteRecherche = %q", got)
	}
	if texteRecherche(nil) != "" {
		t.Error("texteRecherche(nil) : vide attendu")
	}
	// Le payload decode est un tableau devalue dont l'index 7 porte le terme.
	b, err := base64.StdEncoding.DecodeString(payloadRecherche(`loup "gris"`))
	if err != nil {
		t.Fatal(err)
	}
	var arr []any
	if err := json.Unmarshal(b, &arr); err != nil {
		t.Fatalf("payload non JSON : %v", err)
	}
	racine, _ := resoudre(arr, 1, map[int]any{}, map[int]bool{}).(map[string]any)
	if racine["term"] != `loup "gris"` || racine["limitConcepts"] != 6.0 {
		t.Errorf("payload resolu = %#v", racine)
	}
}

func TestTronquerPromo(t *testing.T) {
	if got := tronquerPromo("  Une serie. Vous aimez ce podcast ? Pour ecouter..."); got != "Une serie." {
		t.Errorf("tronquerPromo = %q", got)
	}
	if got := tronquerPromo(" Rien a couper "); got != "Rien a couper" {
		t.Errorf("tronquerPromo = %q", got)
	}
}
