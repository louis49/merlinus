package app

import (
	"reflect"
	"testing"

	"github.com/louis49/merlinus/internal/playlist"
)

func feuille(u, t string) *playlist.Node { return &playlist.Node{UUID: u, Title: t} }

func uuidsDe(ns []*playlist.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.UUID
	}
	return out
}

// arbreTest : categorie A,B,C ; radio (station S) ; etagere avec C (present
// ailleurs) et X (orpheline).
func arbreTest() ([]*playlist.Node, *playlist.Node) {
	etagere := &playlist.Node{UUID: "da", Title: "Dernier ajouts", Children: []*playlist.Node{
		feuille("C", "C"), feuille("X", "Orpheline"),
	}}
	return []*playlist.Node{
		{UUID: "h", Title: "Histoires", Children: []*playlist.Node{feuille("A", "A"), feuille("B", "B"), feuille("C", "C")}},
		{UUID: "r", Title: "Radio", Webradio: 1, Children: []*playlist.Node{feuille("S", "Station")}},
		etagere,
	}, etagere
}

func TestDerniersAjoutsAvecAjouts(t *testing.T) {
	arbre, etagere := arbreTest()
	// B et A ajoutes par cette synchro ; S (station) et Z (inconnu) ignores.
	regenererDerniersAjouts(arbre, []string{"B", "S", "A", "Z"}, nil, 20)
	if got, want := uuidsDe(etagere.Children), []string{"B", "A", "X"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("etagere = %v, attendu %v (ajouts seuls + orpheline gardee)", got, want)
	}
	if etagere.Children[0].Children != nil || etagere.Children[0].Title != "B" {
		t.Fatalf("reference mal formee : %+v", etagere.Children[0])
	}
}

func TestDerniersAjoutsSansAjout(t *testing.T) {
	arbre, etagere := arbreTest()
	regenererDerniersAjouts(arbre, nil, nil, 20)
	if got, want := uuidsDe(etagere.Children), []string{"C", "X"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("etagere = %v, attendu %v (conservee telle quelle)", got, want)
	}
}

func TestDerniersAjoutsRetraits(t *testing.T) {
	arbre, etagere := arbreTest()
	// C et l'orpheline X retires volontairement : ils quittent l'etagere.
	regenererDerniersAjouts(arbre, nil, map[string]bool{"C": true, "X": true}, 20)
	if etagere.Children == nil || len(etagere.Children) != 0 {
		t.Fatalf("etagere vide (non nil) attendue, obtenu %v", uuidsDe(etagere.Children))
	}
}

func TestDerniersAjoutsPlafondEtAbsente(t *testing.T) {
	arbre, etagere := arbreTest()
	regenererDerniersAjouts(arbre, []string{"A", "B", "C"}, nil, 2)
	if got, want := uuidsDe(etagere.Children), []string{"A", "B", "X"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("etagere = %v, attendu %v (plafond 2 + orpheline)", got, want)
	}
	// Sans etagere : aucun effet, pas de panique.
	regenererDerniersAjouts([]*playlist.Node{{UUID: "h", Title: "Histoires", Children: []*playlist.Node{}}}, []string{"A"}, nil, 20)
}
