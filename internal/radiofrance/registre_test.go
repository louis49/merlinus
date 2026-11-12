package radiofrance

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/itunes"
)

// registreMemoire : registre persiste en memoire (contenu partage entre
// instances, comme le fichier).
type registreMemoire struct {
	mu   sync.Mutex
	data []byte
}

func (m *registreMemoire) nouveau() *Registre {
	return NouveauRegistre(
		func() ([]byte, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.data == nil {
				return nil, errAbsent
			}
			return m.data, nil
		},
		func(d []byte) error { m.mu.Lock(); m.data = d; m.mu.Unlock(); return nil },
	)
}

var errAbsent = errors.New("absent")

// serieDeux : serie de deux episodes (idEp1, idEp2) sans flux RSS.
func serieDeux(t *testing.T, f *fauxSite, chemin string) {
	t.Helper()
	f.routes[hote+"/"+chemin+"/__data.json"] = texte(pageData(t, map[string]any{"items": []any{
		episodeRF(idEp1, "Le camping 1/4", "", 600, "", "2024-01-01"),
		episodeRF(idEp2, "Le camping 2/4", "", 600, "", "2024-01-02"),
	}}))
	f.routes[hote+"/"+chemin+"/__data.json?p=2"] = texte(pageData(t, map[string]any{"items": []any{}}))
}

// Un episode deja vu ne coute plus d'aod ; republie sous un autre nom de
// fichier (cas "Le camping"), il garde son uuid ; l'URL est revue au
// telechargement (URLAudio) sans changer l'uuid.
func TestRegistreUUIDFige(t *testing.T) {
	c, f := nouveauClient(t)
	mem := &registreMemoire{}
	c.Reg = mem.nouveau()
	chemin := "francemusique/podcasts/octave"
	serieDeux(t, f, chemin)
	ancien1 := "https://media.test/p/12345-15.10.2026-ITEMA_1-2026F1E0001-NET_MFI_AAA.mp3"
	f.routes[hote+"/transistor/aod/"+idEp1] = aod(ancien1)
	f.routes[hote+"/transistor/aod/"+idEp2] = aod("https://media.test/p/ITEMA_2.mp3")

	eps, err := c.EpisodesArrierePlan(chemin)
	if err != nil || len(eps) != 2 {
		t.Fatalf("premiere lecture : %v, %d episodes", err, len(eps))
	}
	uuid1 := itunes.UUIDDeterministe("rf:file:12345-15.10.2026-ITEMA_1-2026F1E0001-NET_MFI_AAA")
	if eps[0].UUID != uuid1 || eps[0].AudioSrc != ancien1 {
		t.Fatalf("uuid calcule comme avant attendu : %+v", eps[0])
	}

	// RF republie le media : nouveau nom de fichier.
	nouveau1 := "https://media.test/p/99999-29.10.2026-ITEMA_1-2026F1E0001-NET_MFI_BBB.mp3"
	f.routes[hote+"/transistor/aod/"+idEp1] = aod(nouveau1)

	// Nouveau client, meme registre persiste : aucun aod, memes uuid.
	c2, _ := nouveauClient(t)
	c2.HTTP = c.HTTP
	c2.Reg = mem.nouveau()
	avant := nbAppelsPrefixe(f, hote+"/transistor/aod/")
	eps2, err := c2.EpisodesArrierePlan(chemin)
	if err != nil || len(eps2) != 2 {
		t.Fatalf("seconde lecture : %v", err)
	}
	if n := nbAppelsPrefixe(f, hote+"/transistor/aod/") - avant; n != 0 {
		t.Errorf("%d aod pour des episodes connus", n)
	}
	if eps2[0].UUID != uuid1 || eps2[0].AudioSrc != ancien1 {
		t.Errorf("episode connu : %+v", eps2[0])
	}

	// Au telechargement : URL revue, uuid inchange, registre a jour.
	if u, err := c2.URLAudio(idEp1); err != nil || u != nouveau1 {
		t.Fatalf("URLAudio : %q %v", u, err)
	}
	e, _ := mem.nouveau().Entree(idEp1)
	if e.UUID != uuid1 || e.URL != nouveau1 {
		t.Errorf("registre apres URLAudio : %+v", e)
	}
	if id := mem.nouveau().IDParUUID(uuid1); id != idEp1 {
		t.Errorf("IDParUUID : %q", id)
	}
}

// Un episode indisponible n'est pas redemande avant 7 jours ; s'il revient
// ensuite, il prend un uuid. Un echec bloquant n'empeche pas d'enregistrer
// les resolutions reussies.
func TestRegistreIndisponible(t *testing.T) {
	c, f := nouveauClient(t)
	c.Reg = (&registreMemoire{}).nouveau()
	chemin := "francemusique/podcasts/octave"
	serieDeux(t, f, chemin)
	f.routes[hote+"/transistor/aod/"+idEp1] = aod("https://media.test/p/ITEMA_1.mp3")
	f.routes[hote+"/transistor/aod/"+idEp2] = statut(http.StatusNotFound)

	eps, err := c.EpisodesArrierePlan(chemin)
	if err != nil || len(eps) != 1 {
		t.Fatalf("lecture : %v, %d episodes", err, len(eps))
	}
	avant := f.nbAppels(hote + "/transistor/aod/" + idEp2)
	if eps, _ = c.EpisodesArrierePlan(chemin); len(eps) != 1 || f.nbAppels(hote+"/transistor/aod/"+idEp2) != avant {
		t.Error("indisponible recent redemande")
	}
	// 8 jours plus tard (date de verification reculee) : redemande, disponible.
	c.Reg.mu.Lock()
	c.Reg.f.Episodes[idEp2].Verifie -= int64((8 * 24 * time.Hour).Seconds())
	c.Reg.mu.Unlock()
	f.routes[hote+"/transistor/aod/"+idEp2] = aod("https://media.test/p/ITEMA_2.mp3")
	if eps, err = c.EpisodesArrierePlan(chemin); err != nil || len(eps) != 2 {
		t.Errorf("apres 7 jours : %v, %d episodes", err, len(eps))
	}
}

// Revalidation : au plus revalidationsParSerie entrees de plus de 30 jours par
// lecture ; l'uuid ne change jamais.
func TestRegistreRevalidation(t *testing.T) {
	r := (&registreMemoire{}).nouveau()
	t0 := time.Now()
	var ids []string
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		ids = append(ids, id)
		r.resolu(id, "", "https://media.test/"+id+".mp3", "u-"+id, t0.Add(-40*24*time.Hour+time.Duration(i)*time.Hour))
	}
	p := r.planifier(ids, t0)
	if len(p.connus) != 8 || len(p.aResoudre) != revalidationsParSerie || p.revalidions != revalidationsParSerie {
		t.Fatalf("plan : %d connus, %d a resoudre", len(p.connus), len(p.aResoudre))
	}
	if p.aResoudre[0] != 0 { // la plus ancienne d'abord
		t.Errorf("ordre : %v", p.aResoudre)
	}
	if u := r.resolu("a", "", "https://media.test/a2.mp3", "autre", t0); u != "u-a" {
		t.Errorf("uuid change a la revalidation : %q", u)
	}
	// Sans registre : tout est a resoudre.
	var nul *Registre
	if p := nul.planifier(ids, t0); len(p.aResoudre) != 8 {
		t.Errorf("sans registre : %d", len(p.aResoudre))
	}
}

// Rattacher : refuse un uuid deja pris par un autre episode ; registre
// illisible : repart vide sans erreur.
func TestRegistreRattacher(t *testing.T) {
	r := (&registreMemoire{}).nouveau()
	t0 := time.Now()
	r.resolu("a", "", "https://m/a.mp3", "ua", t0)
	r.resolu("b", "", "https://m/b.mp3", "ub", t0)
	if r.Rattacher("a", "ub") || r.Rattacher("inconnu", "ux") {
		t.Error("rattachement interdit accepte")
	}
	if !r.Rattacher("a", "vieux") || r.IDParUUID("vieux") != "a" || r.IDParUUID("ua") != "" {
		t.Error("rattachement")
	}
	if e, _ := r.Entree("a"); e.Origine != "titre" {
		t.Errorf("origine : %+v", e)
	}
	illisible := &registreMemoire{data: []byte("{pas du json")}
	if _, ok := illisible.nouveau().Entree("a"); ok {
		t.Error("registre illisible")
	}
}
