package app

import (
	"encoding/json"
	"testing"

	"github.com/louis49/merlinus/internal/store"
)

// Le bouton Synchroniser ne s'active que si une publication changerait quelque
// chose : un etat d'affichage (_open) ne compte pas, une vraie edition si.
func TestEditionsEnAttente(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5f:01")
	reel, _ := json.Marshal(arbreReel())
	if err := store.Save(ficArbre(mac), reel); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	cas := []struct {
		nom     string
		travail string // "" : pas de travail
		attendu bool
	}{
		{"aucun travail", "", false},
		{"travail identique, noeud ouvert", `[{"uuid":"` + pCat + `","title":"Cat","_open":true,"child":[
			{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2"}]}]`, false},
		{"renommage", `[{"uuid":"` + pCat + `","title":"Cat renommee","child":[
			{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2"}]}]`, true},
		{"ordre", `[{"uuid":"` + pCat + `","title":"Cat","child":[
			{"uuid":"` + pH2 + `","title":"H2"},{"uuid":"` + pH1 + `","title":"H1"}]}]`, true},
		{"retrait", `[{"uuid":"` + pCat + `","title":"Cat","child":[
			{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2","_rem":true}]}]`, true},
		{"ajout", `[{"uuid":"` + pCat + `","title":"Cat","child":[
			{"uuid":"` + pH1 + `","title":"H1"},{"uuid":"` + pH2 + `","title":"H2"},{"uuid":"` + pH3 + `","title":"H3","_new":true}]}]`, true},
		{"preparation refusee (anti-wipe) : la publication dira pourquoi", `[{"uuid":"` + pCat + `","title":"Cat","child":[
			{"uuid":"` + pH1 + `","title":"H1"}]}]`, true},
	}
	for _, c := range cas {
		if c.travail == "" {
			_ = store.Remove(ficTravail(mac))
		} else if err := store.Save(ficTravail(mac), []byte(c.travail)); err != nil {
			t.Fatal(err)
		}
		if got := a.EditionsEnAttente(mac); got != c.attendu {
			t.Errorf("%s : %v, attendu %v", c.nom, got, c.attendu)
		}
	}
}
