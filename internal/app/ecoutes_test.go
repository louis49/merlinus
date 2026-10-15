package app

import (
	"testing"

	"github.com/louis49/merlinus/internal/playlist"
)

func TestFusionnerEcoutes(t *testing.T) {
	titres := map[string][2]string{"a": {"Titre A", "Serie 1"}, "b": {"Titre B", "Serie 2"}}
	// 1er releve.
	h := fusionnerEcoutes([]EcouteGardee{}, []playlist.Ecoute{
		{UUID: "a", Debut: 100, Minutes: 2},
		{UUID: "b", Debut: 200, Minutes: 5},
	}, titres)
	if len(h) != 2 || h[0].UUID != "b" || h[1].Titre != "Titre A" {
		t.Fatalf("1er releve : %+v", h)
	}
	// 2e releve : "a" deja connue (ecoute continuee), "a" reecoutee plus tard,
	// "b" purgee de la boite (absente du releve) mais gardee dans l'historique.
	h = fusionnerEcoutes(h, []playlist.Ecoute{
		{UUID: "a", Debut: 100, Minutes: 4},
		{UUID: "a", Debut: 300, Minutes: 1},
	}, map[string][2]string{})
	if len(h) != 3 {
		t.Fatalf("2e releve : %d ecoutes, attendu 3", len(h))
	}
	if h[0].Debut != 300 || h[1].Debut != 200 || h[2].Debut != 100 {
		t.Fatalf("ordre : %+v", h)
	}
	if h[2].Minutes != 4 {
		t.Errorf("minutes de l'ecoute continuee = %d, attendu 4", h[2].Minutes)
	}
	if h[2].Titre != "Titre A" {
		t.Errorf("titre fige au 1er releve perdu : %q", h[2].Titre)
	}
}

func TestFusionnerEcoutesBorne(t *testing.T) {
	var rel []playlist.Ecoute
	for i := 0; i < maxEcoutesGardees+10; i++ {
		rel = append(rel, playlist.Ecoute{UUID: "x", Debut: uint32(i + 1)})
	}
	h := fusionnerEcoutes(nil, rel, nil)
	if len(h) != maxEcoutesGardees || h[0].Debut != uint32(maxEcoutesGardees+10) {
		t.Fatalf("%d ecoutes, la plus recente = %d", len(h), h[0].Debut)
	}
}

// Historique limite aux 7 derniers jours (repere : heure murale "comme UTC").
func TestRecentes(t *testing.T) {
	const maintenant = 1_800_000_000
	h := []EcouteGardee{
		{UUID: "hier", Debut: maintenant - 86400},
		{UUID: "limite", Debut: maintenant - dureeHistorique},
		{UUID: "vieille", Debut: maintenant - dureeHistorique - 1},
	}
	got := recentes(h, maintenant)
	if len(got) != 2 || got[0].UUID != "hier" || got[1].UUID != "limite" {
		t.Errorf("recentes = %+v", got)
	}
	if len(h) != 3 {
		t.Error("l'historique d'origine ne doit pas etre modifie")
	}
}
