package app

// Menage de la carte apres publication, fait par Merlinus (BUGS.md B2).
//
// clean_sd.cmd (le menage du firmware 2.1.23, appele par l'app officielle) ne
// reconnait comme medias d'un noeud que `<uuid>.mp3` et `<uuid>.jpg` : il
// efface donc les `<uuid>.aac` que l'arbre reference pourtant (m4a remuxes),
// que la boite sait lire. Merlinus ne l'appelle plus et supprime lui-meme,
// apres l'adoption du nouvel arbre, les seuls medias des noeuds RETIRES : uuid
// present dans l'arbre relu sur la boite en debut de publication, absent de
// l'arbre publie. Rien d'autre n'est touche (fichiers systeme, noms qui ne sont
// pas `<uuid>.<ext>`, orphelins anterieurs).
//
// En contrepartie, la publication renvoie les `.aac` de l'arbre que la boite
// n'a plus (effaces par un clean_sd anterieur) : voir aacAVerifier.

import (
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
)

// extMenage : seules extensions supprimees par le menage (medias d'un noeud).
var extMenage = []string{"mp3", "aac", "jpg"}

var reUUIDStrict = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const uuidNul = "00000000-0000-0000-0000-000000000000"

// fichiersARetirer liste, triees, les medias a supprimer de la carte : pour
// chaque uuid de l'arbre d'avant absent de l'arbre d'apres, `<uuid>.mp3`,
// `<uuid>.aac` et `<uuid>.jpg`. Un uuid encore reference ailleurs (deplace, en
// double dans "Derniers ajouts") est garde, casse ignoree ; un identifiant qui
// n'est pas un uuid canonique en minuscules (ou l'uuid nul de la racine) est
// ignore : dans le doute, on ne supprime pas.
func fichiersARetirer(avant, apres map[string]bool) []string {
	garde := map[string]bool{}
	for u := range apres {
		garde[strings.ToLower(u)] = true
	}
	var noms []string
	for u := range avant {
		if garde[u] || u == uuidNul || !reUUIDStrict.MatchString(u) {
			continue
		}
		for _, ext := range extMenage {
			noms = append(noms, u+"."+ext)
		}
	}
	sort.Strings(noms)
	return noms
}

// retirerMedias supprime noms de la carte (op15, absence verifiee par op31).
// Non bloquant : la publication est deja faite, un echec laisse un orphelin
// sur la carte. Renvoie les noms dont l'absence est confirmee.
func retirerMedias(c *device.Client, mac string, noms []string) map[string]bool {
	absents := map[string]bool{}
	echecs := 0
	for _, nom := range noms {
		if err := c.SupprimerFichierVerifie(nom); err != nil {
			echecs++
			log.Printf("publication %s : suppression de %s non faite (%v)", mac, nom, err)
			continue
		}
		absents[nom] = true
	}
	log.Printf("publication %s : menage, %d noeud(s) retire(s), %d fichier(s) verifies absents, %d echec(s)",
		mac, len(noms)/len(extMenage), len(absents), echecs)
	return absents
}

// aacAVerifier liste, triees, les histoires de l'arbre publie dont l'audio en
// bibliotheque est un `.aac`, hors contenus nouveaux (deja envoyes). Leur
// presence sur la boite est verifiee a chaque publication : un clean_sd
// anterieur (Merlinus avant B2, ou l'app officielle) a pu les effacer.
func aacAVerifier(publie []*playlist.Node, nouveaux []string) []string {
	neufs := map[string]bool{}
	for _, u := range nouveaux {
		neufs[u] = true
	}
	var out []string
	for u := range playlist.Histoires(publie) {
		if !neufs[u] && library.ExtensionAudio(u) == "aac" {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}
