package app

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

// Historique d'ecoute : la boite ne garde que les ecoutes non encore remontees
// au backend Merlin (purge apres envoi via l'app officielle). Merlinus CUMULE
// donc chaque releve dans le dossier de travail, pour afficher un historique
// qui survit a ces purges, limite aux 7 derniers jours (recentes).
func ficEcoutes(mac string) string { return dirTravailBoite(mac) + "/ecoutes.json" }

// maxEcoutesGardees borne l'historique cumule (les plus recentes), en plus de
// la limite de duree (dureeHistorique).
const maxEcoutesGardees = 200

// dureeHistorique : les ecoutes plus anciennes (en secondes) sont oubliees.
const dureeHistorique = 7 * 24 * 3600

// maintenantMural : l'instant t en heure murale locale encodee "comme UTC",
// le repere de Debut (horloge de la boite).
func maintenantMural(t time.Time) int64 {
	_, decalage := t.Zone()
	return t.Unix() + int64(decalage)
}

// recentes ne garde que les ecoutes des dureeHistorique dernieres secondes.
func recentes(h []EcouteGardee, maintenant int64) []EcouteGardee {
	out := h[:0:0]
	for _, e := range h {
		if int64(e.Debut) >= maintenant-dureeHistorique {
			out = append(out, e)
		}
	}
	return out
}

// EcouteGardee : une ecoute cumulee, avec titre et serie figes au releve (le
// contenu peut avoir quitte l'arbre depuis).
type EcouteGardee struct {
	UUID    string `json:"uuid"`
	Debut   uint32 `json:"debut"` // heure murale locale de la boite "comme UTC" (cf. playlist.Ecoute)
	Minutes int    `json:"minutes"`
	Titre   string `json:"titre"`
	Serie   string `json:"serie"`
}

type historiqueEcoutes struct {
	Releve  int64          `json:"releve"` // date reelle (Unix) du dernier releve reussi
	Ecoutes []EcouteGardee `json:"ecoutes"`
}

func chargerHistorique(mac string) historiqueEcoutes {
	h := historiqueEcoutes{Ecoutes: []EcouteGardee{}}
	if d, err := store.Load(ficEcoutes(mac)); err == nil {
		_ = json.Unmarshal(d, &h)
	}
	if h.Ecoutes == nil {
		h.Ecoutes = []EcouteGardee{}
	}
	return h
}

// fusionnerEcoutes ajoute les ecoutes relevees a l'historique, sans doublon
// (cle = UUID + debut). Une ecoute deja connue garde le plus grand nombre de
// minutes (une ecoute en cours au releve precedent a pu continuer). Resultat
// trie du plus recent au plus ancien, borne a maxEcoutesGardees.
func fusionnerEcoutes(h []EcouteGardee, rel []playlist.Ecoute, titres map[string][2]string) []EcouteGardee {
	idx := map[string]int{}
	for i, e := range h {
		idx[e.UUID+"|"+strconv.Itoa(int(e.Debut))] = i
	}
	for _, e := range rel {
		cle := e.UUID + "|" + strconv.Itoa(int(e.Debut))
		if i, ok := idx[cle]; ok {
			if e.Minutes > h[i].Minutes {
				h[i].Minutes = e.Minutes
			}
			continue
		}
		t := titres[e.UUID]
		h = append(h, EcouteGardee{UUID: e.UUID, Debut: e.Debut, Minutes: e.Minutes, Titre: t[0], Serie: t[1]})
		idx[cle] = len(h) - 1
	}
	sort.SliceStable(h, func(i, j int) bool { return h[i].Debut > h[j].Debut })
	if len(h) > maxEcoutesGardees {
		h = h[:maxEcoutesGardees]
	}
	return h
}

// titresArbre : uuid -> {titre, titre du parent}, depuis l'arbre de la boite en
// cache. Premier parent rencontre (parcours en profondeur) : la categorie
// "Derniers ajouts", qui ne fait que referencer, vient apres les vraies series.
func titresArbre(mac string) map[string][2]string {
	out := map[string][2]string{}
	d, err := store.Load(ficArbre(mac))
	if err != nil {
		return out
	}
	var top []*playlist.Node
	if json.Unmarshal(d, &top) != nil {
		return out
	}
	playlist.Parcourir(top, func(n, parent *playlist.Node) {
		if _, ok := out[n.UUID]; ok {
			return
		}
		serie := ""
		if parent != nil {
			serie = parent.Title
		}
		out[n.UUID] = [2]string{n.Title, serie}
	})
	return out
}

// LireEcoutes releve l'historique d'ecoute de la boite connectee et le cumule
// dans le dossier de travail. Lecture seule cote boite (jamais clean_listening).
// Renvoie false si la connexion est occupee ou la boite injoignable (le front
// reessaie), true si le releve a abouti.
func (a *App) LireEcoutes(mac string) (bool, error) {
	err := a.sonderBoite(mac, func(c *device.Client) error { return releverEcoutes(c, mac) })
	return err == nil, nil
}

// releverEcoutes lit l'historique de la boite connectee (get_listening.cmd,
// lecture seule) et le cumule dans le dossier de travail. Titres et series
// pris dans l'arbre en cache : a appeler apres sa persistance.
func releverEcoutes(c *device.Client, mac string) error {
	brut, err := c.GetFileBytes("get_listening.cmd")
	var refus *device.ErrFichierRefuse
	if errors.As(err, &refus) {
		// Historique vide : la boite refuse la commande (statut seul, "02",
		// constate apres clean_listening.cmd). Releve abouti, 0 ecoute.
		brut, err = nil, nil
	}
	if err != nil {
		return err
	}
	h := chargerHistorique(mac)
	h.Ecoutes = recentes(fusionnerEcoutes(h.Ecoutes, playlist.DecoderEcoutes(brut), titresArbre(mac)), maintenantMural(time.Now()))
	h.Releve = time.Now().Unix()
	d, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return store.Save(ficEcoutes(mac), d)
}

// EcouteVue : une ecoute telle qu'affichee sur l'accueil.
type EcouteVue struct {
	UUID    string `json:"uuid"`
	Titre   string `json:"titre"`
	Serie   string `json:"serie"`
	Debut   int64  `json:"debut"` // a formater en UTC (heure murale de la boite)
	Minutes int    `json:"minutes"`
	DureeS  int    `json:"duree_s"` // 0 si inconnue (pas d'audio local)
}

// DernieresEcoutes : les n ecoutes les plus recentes de l'historique cumule,
// et la date reelle (Unix) du dernier releve (0 : jamais releve).
type DernieresEcoutes struct {
	Releve  int64       `json:"releve"`
	Ecoutes []EcouteVue `json:"ecoutes"`
}

func (a *App) DernieresEcoutes(mac string, n int) DernieresEcoutes {
	h := chargerHistorique(mac)
	out := DernieresEcoutes{Releve: h.Releve, Ecoutes: []EcouteVue{}}
	titres := titresArbre(mac)
	for _, e := range recentes(h.Ecoutes, maintenantMural(time.Now())) {
		if len(out.Ecoutes) >= n {
			break
		}
		titre, serie := e.Titre, e.Serie
		if t, ok := titres[e.UUID]; ok && titre == "" { // releve fait avant la lecture de l'arbre
			titre, serie = t[0], t[1]
		}
		out.Ecoutes = append(out.Ecoutes, EcouteVue{
			UUID: e.UUID, Titre: titre, Serie: serie, Debut: int64(e.Debut),
			Minutes: e.Minutes, DureeS: dureeEcoute(mac, e.UUID),
		})
	}
	return out
}

// dureeEcoute : duree (secondes) d'une histoire, d'apres son MP3 en
// bibliotheque (contenus ajoutes par Merlinus) ou, a defaut, sa copie dans la
// sauvegarde de la boite (contenus d'origine). 0 si inconnue.
func dureeEcoute(mac, uuid string) int {
	if d := library.DureeAudio(uuid); d > 0 {
		return d
	}
	if uuidMediaValide(uuid) != nil {
		return 0
	}
	return library.DureeMP3(dirSauvegarde(mac) + "/" + uuid + ".mp3")
}
