package app

// Registre des episodes Radio France et rattachement par titre (BUGS.md B6).
//
// Le registre (internal/radiofrance/registre.go) fige l'uuid de chaque episode
// a sa premiere vue. Mais a sa creation, des episodes de la boite portent deja
// un uuid calcule sur un ANCIEN nom de fichier (RF a republie le media
// depuis) : le registre leur donne l'uuid du nom actuel, et l'episode serait
// propose en double. Le rattachement par titre corrige ce cas, UNE fois par
// serie : dans le dossier de la serie sur la boite, un episode du catalogue
// absent de la boite prend l'uuid de la feuille de meme titre, si ce titre est
// unique des deux cotes et si la feuille n'est appariee a aucun autre episode.
// Les cas ambigus (ex. "Le camping 1/4" deja en double sur la boite) ne sont
// pas touches.

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/itunes"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/radiofrance"
	"github.com/louis49/merlinus/internal/store"
)

// ficRegistreRF : registre des episodes Radio France, global (toutes boites).
const ficRegistreRF = "travail/radiofrance.json"

func registreRF() *radiofrance.Registre {
	return radiofrance.NouveauRegistre(
		func() ([]byte, error) { return store.Load(ficRegistreRF) },
		func(d []byte) error { return store.Save(ficRegistreRF, d) },
	)
}

// normTitre : titre compare au rattachement (casse, espaces dont insecables,
// apostrophes typographiques).
func normTitre(s string) string {
	s = strings.NewReplacer("’", "'", "‘", "'").Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// pairesParTitre rend, pour les episodes du catalogue absents de la boite,
// l'uuid de la feuille de meme titre (id d'expression -> uuid), sous les
// conditions decrites en tete de fichier.
func pairesParTitre(feuilles []*playlist.Node, eps []itunes.Episode) map[string]string {
	surBoite := map[string]bool{}
	nbBoite := map[string]int{}
	feuilleDe := map[string]string{}
	for _, f := range feuilles {
		t := normTitre(f.Title)
		surBoite[f.UUID] = true
		nbBoite[t]++
		feuilleDe[t] = f.UUID
	}
	auCatalogue := map[string]bool{}
	nbCat := map[string]int{}
	for _, e := range eps {
		auCatalogue[e.UUID] = true
		nbCat[normTitre(e.Title)]++
	}
	out := map[string]string{}
	for _, e := range eps {
		t := normTitre(e.Title)
		if surBoite[e.UUID] || e.ID == "" || t == "" || nbBoite[t] != 1 || nbCat[t] != 1 {
			continue
		}
		if u := feuilleDe[t]; !auCatalogue[u] {
			out[e.ID] = u
		}
	}
	return out
}

// dossierSerieRF cherche, dans l'arbre connu de la boite mac, le dossier de la
// serie Radio France chemin (uuid de l'annuaire suivis.json, a defaut uuid
// catalogue de la serie).
func dossierSerieRF(mac, chemin string) *playlist.Node {
	arbre := arbreBoite(mac)
	if len(arbre) == 0 {
		return nil
	}
	cand := map[string]bool{itunes.UUIDDeterministe("rf:" + chemin): true}
	for u, s := range lireSuivis(mac) {
		if s.Src == "radiofrance" && strings.Trim(s.SrcId, "/") == chemin {
			cand[u] = true
		}
	}
	var trouve *playlist.Node
	playlist.Parcourir(arbre, func(n, _ *playlist.Node) {
		if trouve == nil && n.Children != nil && cand[n.UUID] {
			trouve = n
		}
	})
	return trouve
}

// rattacherRF applique le rattachement par titre a la serie chemin, une seule
// fois : sur la premiere boite connue qui porte la serie. Rend eps avec les
// uuid rattaches.
func (a *App) rattacherRF(chemin string, eps []itunes.Episode) []itunes.Episode {
	reg := a.rf.Reg
	chemin = strings.Trim(chemin, "/")
	if reg == nil || len(eps) == 0 || reg.Rapproche(chemin) {
		return eps
	}
	for _, m := range a.lireRegistre() {
		dossier := dossierSerieRF(m.MAC, chemin)
		if dossier == nil {
			continue
		}
		var feuilles []*playlist.Node
		playlist.Parcourir(dossier.Children, func(n, _ *playlist.Node) {
			if n.Children == nil {
				feuilles = append(feuilles, n)
			}
		})
		paires := pairesParTitre(feuilles, eps)
		for i := range eps {
			if u, ok := paires[eps[i].ID]; ok && reg.Rattacher(eps[i].ID, u) {
				log.Printf("radiofrance %s : %q rattache par titre a %s (deja sur la boite %s)", chemin, eps[i].Title, u, m.MAC)
				eps[i].UUID = u
			}
		}
		reg.MarquerRapproche(chemin, time.Now())
		if err := reg.Sauver(); err != nil {
			log.Printf("radiofrance : registre non sauvegarde (%v)", err)
		}
		break
	}
	return eps
}

// urlAudioFraiche : avant de telecharger un episode Radio France absent de la
// bibliotheque, son URL audio est resolue a nouveau (1 requete aod, comme le
// site au clic) : celle du registre a pu perimer si RF a republie le media.
// Un echec autre que "indisponible" garde l'URL connue.
func (a *App) urlAudioFraiche(uuid, url string) (string, error) {
	if a.rf.Reg == nil || (library.ExtensionAudio(uuid) != "" && store.Has(library.CheminIllustration(uuid))) {
		return url, nil
	}
	id := a.rf.Reg.IDParUUID(uuid)
	if id == "" {
		return url, nil
	}
	u, err := a.rf.URLAudio(id)
	switch {
	case err == nil:
		return u, nil
	case radiofrance.EstIndisponible(err):
		return "", err
	default:
		log.Printf("radiofrance : URL audio de %s non revalidee (%v), URL connue gardee", uuid, err)
		return url, nil
	}
}

// arbreBoite : arbre connu de la boite (dernier relu), nil si absent.
func arbreBoite(mac string) []*playlist.Node {
	var arbre []*playlist.Node
	if d, err := store.Load(ficArbre(mac)); err == nil {
		_ = json.Unmarshal(d, &arbre)
	}
	return arbre
}

// lireSuivis : annuaire des series suivies de la boite (uuid -> identite).
func lireSuivis(mac string) map[string]SuiviSerie {
	m := map[string]SuiviSerie{}
	if d, err := store.Load(ficSuivis(mac)); err == nil {
		_ = json.Unmarshal(d, &m)
	}
	return m
}
