package app

import (
	"slices"
	"testing"
)

// Historique vide (reponse "02" apres clean_listening.cmd) : le releve aboutit
// avec 0 ecoute et date le releve, au lieu d'echouer.
func TestReleverEcoutesVide(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:60:01")
	b := nouvelleFausseBoite(t)
	if err := releverEcoutes(b.connecter(), mac); err != nil {
		t.Fatalf("historique vide pris pour une erreur : %v", err)
	}
	if h := chargerHistorique(mac); h.Releve == 0 || len(h.Ecoutes) != 0 {
		t.Errorf("historique = %+v", h)
	}
	if !slices.Contains(b.operations("get "), "get get_listening.cmd") {
		t.Error("get_listening.cmd non demande")
	}
}
