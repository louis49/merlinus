package app

import (
	"testing"

	"github.com/louis49/merlinus/internal/playlist"
)

// UUID canoniques de test : categorie, histoires, favori.
const (
	pCat = "a0000000-0000-4000-8000-000000000001"
	pH1  = "a0000000-0000-4000-8000-000000000002"
	pH2  = "a0000000-0000-4000-8000-000000000003"
	pH3  = "a0000000-0000-4000-8000-000000000004"
	pFav = "a0000000-0000-4000-8000-000000000005"
)

// arbreReel : ce que la boite porte (categorie Cat avec H1 et H2, et la
// categorie systeme des favoris, seule a porter `favorite`).
func arbreReel() []*playlist.Node {
	return []*playlist.Node{
		{UUID: pCat, Title: "Cat", Children: []*playlist.Node{{UUID: pH1, Title: "H1"}, {UUID: pH2, Title: "H2"}}},
		{UUID: pFav, Title: "Merlin_favorite", Favorite: 1, Children: []*playlist.Node{}},
	}
}

func TestPreparerArbreSansEditions(t *testing.T) {
	p, err := preparerArbre(arbreReel(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.publie) != 2 || len(p.nouveaux) != 0 || len(p.images) != 0 {
		t.Errorf("republication a l'identique attendue : %+v", p)
	}
}

func TestPreparerArbreAjoutEtRetrait(t *testing.T) {
	// Le travail ne porte pas la categorie systeme (masquee dans l'IHM) : elle
	// est reinjectee depuis la boite. H2 est retiree, H3 ajoutee.
	travail := `[{"uuid":"` + pCat + `","title":"Cat","child":[
		{"uuid":"` + pH1 + `","title":"H1"},
		{"uuid":"` + pH2 + `","title":"H2","_rem":true},
		{"uuid":"` + pH3 + `","title":"H3","_new":true}]}]`
	p, err := preparerArbre(arbreReel(), []byte(travail))
	if err != nil {
		t.Fatal(err)
	}
	f := playlist.Feuilles(p.publie)
	if !f[pH1] || f[pH2] || !f[pH3] {
		t.Errorf("feuilles publiees = %v", f)
	}
	if playlist.CompterFavoris(p.publie) != 1 {
		t.Error("categorie favoris non reinjectee")
	}
	if len(p.nouveaux) != 1 || p.nouveaux[0] != pH3 {
		t.Errorf("nouveaux = %v", p.nouveaux)
	}
}

// Anti-wipe : une histoire de la boite absente du travail SANS retrait explicite
// (ajoutee par l'app officielle) bloque la publication.
func TestPreparerArbreAntiWipe(t *testing.T) {
	travail := `[{"uuid":"` + pCat + `","title":"Cat","child":[{"uuid":"` + pH1 + `","title":"H1"}]}]`
	_, err := preparerArbre(arbreReel(), []byte(travail))
	if cleErreur(err) != "err_anti_wipe" {
		t.Fatalf("anti-wipe attendu, obtenu : %v", err)
	}
	// Retrait en cascade de la categorie : ses feuilles sont des retraits voulus.
	travail = `[{"uuid":"` + pCat + `","title":"Cat","_rem":true,"child":[
		{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2"}]},
		{"uuid":"` + pH3 + `","title":"Autre","child":[{"uuid":"a0000000-0000-4000-8000-000000000009","title":"X","_new":true}]}]`
	if _, err := preparerArbre(arbreReel(), []byte(travail)); err != nil {
		t.Fatalf("retrait en cascade refuse : %v", err)
	}
}

func TestPreparerArbreGardeFous(t *testing.T) {
	// Aucune feuille restante : arbre degenere.
	travail := `[{"uuid":"` + pCat + `","title":"Cat","child":[
		{"uuid":"` + pH1 + `","title":"H1","_rem":true},{"uuid":"` + pH2 + `","title":"H2","_rem":true}]}]`
	if _, err := preparerArbre(arbreReel(), []byte(travail)); cleErreur(err) != "err_arbre_degenere" {
		t.Errorf("arbre sans feuille : %v", err)
	}
	// UUID non canonique ajoute (repli front ancien) : refuse.
	travail = `[{"uuid":"` + pCat + `","title":"Cat","child":[
		{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2"},
		{"uuid":"new-1728900000000-42","title":"Nouveau","_new":true}]}]`
	if _, err := preparerArbre(arbreReel(), []byte(travail)); cleErreur(err) != "err_uuid_invalide" {
		t.Errorf("uuid non canonique : %v", err)
	}
	// Boite sans categorie favoris : zero favori, refuse.
	reel := arbreReel()[:1]
	if _, err := preparerArbre(reel, nil); cleErreur(err) != "err_favoris" {
		t.Errorf("zero favori : %v", err)
	}
	// Travail illisible.
	if _, err := preparerArbre(arbreReel(), []byte("{pas du json")); err == nil {
		t.Error("travail illisible accepte")
	}
}
