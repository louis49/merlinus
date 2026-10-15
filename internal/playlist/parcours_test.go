package playlist

import (
	"reflect"
	"testing"
)

func TestParcourir(t *testing.T) {
	arbre := []*Node{
		{UUID: "c1", Title: "Cat", Children: []*Node{
			{UUID: "s1", Title: "Serie", Children: []*Node{{UUID: "h1", Title: "H1"}, nil, {UUID: "h2", Title: ""}}},
			{UUID: "v", Title: "Vide", Children: []*Node{}},
		}},
		{UUID: "", Title: "Sans uuid"},
	}
	var ordre []string
	Parcourir(arbre, func(n, p *Node) {
		pt := "-"
		if p != nil {
			pt = p.UUID
		}
		ordre = append(ordre, n.UUID+"<"+pt)
	})
	want := []string{"c1<-", "s1<c1", "h1<s1", "h2<s1", "v<c1", "<-"}
	if !reflect.DeepEqual(ordre, want) {
		t.Errorf("ordre = %v, attendu %v", ordre, want)
	}
	if got := IndexTitres(arbre); !reflect.DeepEqual(got, map[string]string{"c1": "Cat", "s1": "Serie", "h1": "H1", "v": "Vide"}) {
		t.Errorf("IndexTitres = %v", got)
	}
	if got := UUIDs(arbre); len(got) != 5 || got[""] {
		t.Errorf("UUIDs = %v", got)
	}
	// Feuilles : noeuds sans child (le conteneur vide n'en est pas une).
	if got := Feuilles(arbre); !reflect.DeepEqual(got, map[string]bool{"h1": true, "h2": true, "": true}) {
		t.Errorf("Feuilles = %v", got)
	}
}

// Histoires : feuilles hors webradio ; une station (feuille sous un conteneur
// webradio, a toute profondeur) n'en est pas une.
func TestHistoires(t *testing.T) {
	arbre := []*Node{
		{UUID: "c", Children: []*Node{{UUID: "h1"}, {UUID: "vide", Children: []*Node{}}}},
		{UUID: "r", Webradio: 1, Children: []*Node{{UUID: "st1"}, {UUID: "g", Children: []*Node{{UUID: "st2"}}}}},
		{UUID: "h2"},
	}
	if got := Histoires(arbre); !reflect.DeepEqual(got, map[string]bool{"h1": true, "h2": true}) {
		t.Errorf("Histoires = %v", got)
	}
}

func TestCategoriesSysteme(t *testing.T) {
	for titre, sys := range map[string]bool{"Merlin_favorite": true, "Dernier ajouts": true, "Derniers ajouts": true, "Histoires": false, "": false} {
		if EstCategorieSysteme(titre) != sys {
			t.Errorf("EstCategorieSysteme(%q) != %v", titre, sys)
		}
	}
	if !EstDerniersAjouts("Dernier ajouts") || EstDerniersAjouts("Merlin_favorite") {
		t.Error("EstDerniersAjouts")
	}
}
