package app

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/store"
)

// Menage local au demarrage (sur le disque de l'utilisateur, jamais sur la
// boite) :
//   - bibliotheque : suppression des medias qu'AUCUN fichier de reference ne
//     cite plus (episodes retires avant synchro, contenus d'une boite retiree ou
//     restauree) ;
//   - cache des images du catalogue : plafonne, les plus anciennes d'abord.
//
// Un media est garde des que son uuid apparait dans un fichier VIVANT
// (fichiersVivants) : arbre de chaque boite, arbre de travail, perso.json,
// sauvegardes et archives de restauration. Les fichiers d'historique ne
// comptent pas : exports backups/ (jamais relus par l'app), suivis.json
// (annuaire d'identites catalogue, qui ne fait que grandir), ecoutes.json ;
// sinon un contenu retire resterait des jours, voire pour toujours. La
// recherche est textuelle (pas de decodage JSON) : un fichier mal forme protege
// quand meme ce qu'il cite. Un fichier de reference present mais ILLISIBLE
// annule toute la purge de la bibliotheque.

// plafondCacheCatalogue : taille maximale de cache/catalogue (images
// re-telechargeables).
const plafondCacheCatalogue = 100 << 20

var reUUIDTexte = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// menageDemarrage lance les deux menages ; un echec est journalise, jamais
// bloquant.
func (a *App) menageDemarrage() {
	if n, o, err := purgerBibliotheque(); err != nil {
		log.Printf("menage : bibliotheque non purgee (%v)", err)
	} else if n > 0 {
		log.Printf("menage : %d media(s) non reference(s) supprime(s) de la bibliotheque (%s)", n, moLisible(o))
	}
	if n, o := plafonnerCacheCatalogue(); n > 0 {
		log.Printf("menage : %d image(s) du cache catalogue supprimee(s) (%s)", n, moLisible(o))
	}
}

// fichiersJSON liste recursivement les .json sous rel (chemins relatifs au store).
func fichiersJSON(rel string) []string {
	var out []string
	for _, n := range store.ListerDir(rel) {
		if strings.HasSuffix(strings.ToLower(n), ".json") {
			out = append(out, rel+"/"+n)
		}
	}
	for _, d := range store.SousDossiers(rel) {
		if rel+"/"+d == library.DirBibliotheque {
			continue
		}
		out = append(out, fichiersJSON(rel+"/"+d)...)
	}
	return out
}

// fichiersVivants : les fichiers qui decrivent ce qui est, ou sera, sur une
// boite (chemins relatifs au store, existants ou non).
func fichiersVivants() []string {
	out := []string{ficPerso}
	for _, k := range store.SousDossiers("travail/boites") {
		d := "travail/boites/" + k
		out = append(out, d+"/travail.json")
		for _, s := range store.SousDossiers(d) {
			if conserveAuRetrait(s) { // sauvegarde, archives de restauration
				out = append(out, fichiersJSON(d+"/"+s)...)
			}
		}
	}
	for _, k := range store.SousDossiers("cache/boites") {
		out = append(out, "cache/boites/"+k+"/arbre.json")
	}
	return out
}

// referencesBibliotheque : ensemble (minuscules) des uuid cites par les fichiers
// vivants. Un fichier absent ne cite rien ; erreur des qu'un fichier present ne
// se lit pas.
func referencesBibliotheque() (map[string]bool, error) {
	refs := map[string]bool{}
	for _, f := range fichiersVivants() {
		if !store.Has(f) {
			continue
		}
		data, err := store.Load(f)
		if err != nil {
			return nil, fmt.Errorf("%s illisible : %w", f, err)
		}
		for _, u := range reUUIDTexte.FindAll(data, -1) {
			refs[strings.ToLower(string(u))] = true
		}
	}
	return refs, nil
}

// mediasOrphelins : noms de fichiers de la bibliotheque dont l'uuid (nom sans
// extension) n'est pas reference. Un nom qui n'est pas "<uuid>.<ext>" est garde.
func mediasOrphelins(noms []string, refs map[string]bool) []string {
	var out []string
	for _, n := range noms {
		u, ok := library.UUIDDepuisNom(n)
		if !ok || len(u) != 36 || !reUUIDTexte.MatchString(u) {
			continue
		}
		if !refs[strings.ToLower(u)] {
			out = append(out, n)
		}
	}
	return out
}

// purgerBibliotheque supprime les medias orphelins ; renvoie leur nombre et
// leur taille.
func purgerBibliotheque() (int, int64, error) {
	refs, err := referencesBibliotheque()
	if err != nil {
		return 0, 0, err
	}
	var octets int64
	orphelins := mediasOrphelins(store.ListerDir(library.DirBibliotheque), refs)
	for _, n := range orphelins {
		chemin := library.DirBibliotheque + "/" + n
		if s, e := store.Size(chemin); e == nil {
			octets += s
		}
		store.Remove(chemin)
	}
	return len(orphelins), octets, nil
}

// fichierCache : entree du cache pour le plafonnement.
type fichierCache struct {
	nom    string
	taille int64
	date   time.Time
}

// aEvincer choisit, du plus ancien au plus recent, les fichiers a supprimer pour
// revenir sous le plafond.
func aEvincer(fichiers []fichierCache, plafond int64) []string {
	var total int64
	for _, f := range fichiers {
		total += f.taille
	}
	if total <= plafond {
		return nil
	}
	tries := append([]fichierCache(nil), fichiers...)
	sort.Slice(tries, func(i, j int) bool { return tries[i].date.Before(tries[j].date) })
	var out []string
	for _, f := range tries {
		if total <= plafond {
			break
		}
		out = append(out, f.nom)
		total -= f.taille
	}
	return out
}

// plafonnerCacheCatalogue ramene cache/catalogue sous plafondCacheCatalogue.
func plafonnerCacheCatalogue() (int, int64) {
	var fichiers []fichierCache
	for _, n := range store.ListerDir(dirCacheCatalogue) {
		c := dirCacheCatalogue + "/" + n
		s, e1 := store.Size(c)
		d, e2 := store.ModTime(c)
		if e1 == nil && e2 == nil {
			fichiers = append(fichiers, fichierCache{nom: n, taille: s, date: d})
		}
	}
	tailles := map[string]int64{}
	for _, f := range fichiers {
		tailles[f.nom] = f.taille
	}
	var octets int64
	evinces := aEvincer(fichiers, plafondCacheCatalogue)
	for _, n := range evinces {
		octets += tailles[n]
		store.Remove(dirCacheCatalogue + "/" + n)
	}
	return len(evinces), octets
}
