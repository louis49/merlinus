package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/louis49/merlinus/internal/itunes"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

func feuilleRF(uuid, titre string) *playlist.Node { return &playlist.Node{UUID: uuid, Title: titre} }

// Cas reels du 2026-11-11 : "jungle" (uuid de la boite calcule sur un ancien
// nom de fichier) est rattache ; "Le camping 1/4", deja en double sur la
// boite, ne l'est pas ; un episode vraiment nouveau non plus.
func TestPairesParTitre(t *testing.T) {
	feuilles := []*playlist.Node{
		feuilleRF("93b28519", "Cache-cache dans la jungle"),
		feuilleRF("22154a47", "Cache-cache dans le ciel"),
		feuilleRF("066bc8a2", "Le camping 1/4 · L'installation au camping"),
		feuilleRF("f5ef2b24", "Le camping 1/4 · L'installation au camping"),
		feuilleRF("aaaa0001", "Episode retire du catalogue"),
	}
	eps := []itunes.Episode{
		{ID: "id-jungle", UUID: "nouveau-jungle", Title: "Cache-cache  dans la Jungle"},
		{ID: "id-ciel", UUID: "22154a47", Title: "Cache-cache dans le ciel"},
		{ID: "id-camping", UUID: "f5ef2b24", Title: "Le camping 1/4 · L’installation au camping"},
		{ID: "id-neuf", UUID: "neuf", Title: "Cache-cache au zoo"},
	}
	got := pairesParTitre(feuilles, eps)
	if want := map[string]string{"id-jungle": "93b28519"}; !reflect.DeepEqual(got, want) {
		t.Errorf("paires = %v, attendu %v", got, want)
	}
	// Titre en double cote catalogue : pas de rattachement.
	eps = append(eps, itunes.Episode{ID: "id-jungle2", UUID: "autre", Title: "Cache-cache dans la jungle"})
	if got := pairesParTitre(feuilles, eps); len(got) != 0 {
		t.Errorf("titre ambigu rattache : %v", got)
	}
}

// rattacherRF : une seule fois par serie, sur la boite qui porte la serie ;
// le registre garde l'uuid rattache.
func TestRattacherRF(t *testing.T) {
	mac := "aa:bb:cc:00:11:22"
	chemin := "franceinter/podcasts/serie-cache-cache"
	serie := "515fa4d2-7379-56aa-8bd3-426cf2499cc4"
	ecrire := func(f string, v any) {
		d, _ := json.Marshal(v)
		if err := store.Save(f, d); err != nil {
			t.Fatal(err)
		}
	}
	ecrire(ficRegistre, []BoiteMeta{{MAC: mac}})
	ecrire(ficSuivis(mac), map[string]SuiviSerie{serie: {Src: "radiofrance", SrcId: chemin, Title: "Cache-cache"}})
	ecrire(ficArbre(mac), []*playlist.Node{{UUID: "racine", Title: "Histoires", Children: []*playlist.Node{
		{UUID: serie, Title: "Cache-cache", Children: []*playlist.Node{feuilleRF("93b28519", "Cache-cache dans la jungle")}},
	}}})
	t.Cleanup(func() {
		store.Remove(ficRegistre)
		store.Remove(ficRegistreRF)
		store.RemoveDir(dirTravailBoite(mac))
		store.RemoveDir(dirCacheBoite(mac))
	})

	// Episodes connus du registre (vus par une lecture de serie).
	ecrire(ficRegistreRF, map[string]any{"version": 1, "episodes": map[string]any{
		"id-jungle": map[string]any{"uuid": "nouveau-jungle", "url": "https://m/j.mp3", "statut": "ok"},
		"id-autre":  map[string]any{"uuid": "nouveau-autre", "url": "https://m/a.mp3", "statut": "ok"},
	}})
	a := NewApp()
	eps := a.rattacherRF(chemin, []itunes.Episode{{ID: "id-jungle", UUID: "nouveau-jungle", Title: "Cache-cache dans la jungle"}})
	if eps[0].UUID != "93b28519" {
		t.Fatalf("non rattache : %+v", eps[0])
	}
	if id := registreRF().IDParUUID("93b28519"); id != "id-jungle" {
		t.Errorf("registre persiste : %q", id)
	}
	// Deuxieme passage : plus de rattachement (deja fait pour la serie).
	ecrire(ficArbre(mac), []*playlist.Node{{UUID: "racine", Title: "Histoires", Children: []*playlist.Node{
		{UUID: serie, Title: "Cache-cache", Children: []*playlist.Node{feuilleRF("bbbb0002", "Autre")}},
	}}})
	eps = a.rattacherRF(chemin, []itunes.Episode{{ID: "id-autre", UUID: "nouveau-autre", Title: "Autre"}})
	if eps[0].UUID != "nouveau-autre" {
		t.Errorf("second rattachement : %+v", eps[0])
	}
}
