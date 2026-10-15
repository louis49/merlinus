package playlist

// Serialisation de l'arbre en JSON (format du chemin op6 herite ; ne sert plus
// qu'a l'export de l'arbre avant publication). Les titres y sont bornes a 63
// octets (contrainte firmware, statut 13
// TITLE_TOO_LARGE) : cette limite ne vaut QUE pour la boite ; l'arbre local
// garde les titres complets. On tronque donc sur une copie, sans muter l'entree.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MaxTitreOctets : longueur maximale d'un titre pousse a la boite.
const MaxTitreOctets = 63

// MarshalJSON serialise un noeud pour l'enceinte. Point crucial : la boite
// distingue une CATEGORIE d'une FEUILLE par la simple PRESENCE de la cle
// `child`. Un `child:[]` vide (categorie vide, ex. Merlin_favorite) doit donc
// rester present ; l'omettre transformerait la categorie en feuille, et la boite
// exigerait alors add_time/limit_time -> rejet MISSING_FIELD (statut 9). On emet
// donc `child` des que la liste n'est pas nil (une feuille a Children == nil).
// Les drapeaux (favorite/webradio) se lisent a la presence : on ne les
// emet que non nuls, jamais a zero.
// Receveur VALEUR : appele aussi pour un Node serialise par valeur ([]Node,
// map) ; avec un receveur pointeur, le tag `child,omitempty` reprendrait la main
// et ferait disparaitre le `child: []` d'une categorie vide.
func (n Node) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, 8)
	m["uuid"] = n.UUID
	m["title"] = n.Title
	if n.Children != nil {
		m["child"] = n.Children
	}
	if n.AddTimeUnix != 0 {
		m["add_time"] = n.AddTimeUnix
	}
	if n.LimitTime != nil {
		m["limit_time"] = *n.LimitTime
	}
	if n.Favorite != 0 {
		m["favorite"] = n.Favorite
	}
	if n.Webradio != 0 {
		m["webradio"] = n.Webradio
	}
	return json.Marshal(m)
}

// runeEmoji indique si une rune est un emoji / pictogramme / symbole que le
// firmware de la boite ne sait pas afficher (il rend des caracteres parasites).
// On couvre les blocs emoji usuels + les modificateurs (teinte, ZWJ, selecteurs
// de variation, keycap, indicateurs regionaux).
func runeEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // emoji, pictogrammes, teintes, indicateurs regionaux
		return true
	case r >= 0x2600 && r <= 0x27BF: // symboles divers + dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // symboles et fleches divers
		return true
	case r >= 0x2300 && r <= 0x23FF: // symboles techniques (⏰, ⏳...)
		return true
	case r == 0x200D || r == 0x20E3 || r == 0xFE0F || r == 0xFE0E: // ZWJ, keycap, VS15/16
		return true
	default:
		return false
	}
}

// TitreParDefaut : titre pousse a la boite quand le titre nettoye est vide
// (titre fait uniquement d'emojis, ou absent du flux) ; comportement du firmware
// sur un titre vide inconnu.
const TitreParDefaut = "Sans titre"

// nettoyerTitreBoite retire les emojis d'un titre et normalise les espaces (les
// suppressions peuvent laisser des espaces doubles ou en bord). L'arbre local
// garde le titre complet ; ce nettoyage ne vaut que pour ce qui est pousse.
// Jamais vide : repli sur TitreParDefaut.
func nettoyerTitreBoite(t string) string {
	var b strings.Builder
	b.Grow(len(t))
	for _, r := range t {
		if !runeEmoji(r) {
			b.WriteRune(r)
		}
	}
	if propre := strings.Join(strings.Fields(b.String()), " "); propre != "" {
		return propre
	}
	return TitreParDefaut
}

// tronquerTitre borne un titre a 63 octets sur une frontiere de rune, avec une
// ellipse (comme le firmware).
func tronquerTitre(t string) string {
	if len(t) <= MaxTitreOctets {
		return t
	}
	const budget = MaxTitreOctets - len("…") // place pour l'ellipse (3 octets)
	n := 0
	for i := range t { // i = index de debut de chaque rune
		if i > budget {
			break
		}
		n = i
	}
	return strings.TrimRight(t[:n], " ") + "…"
}

// copieTronquee recopie l'arbre en bornant chaque titre, sans muter l'entree.
// IMPORTANT : preserver nil (feuille) vs slice vide (categorie vide). Renvoyer
// une slice non-nil pour une feuille (Children == nil) ferait emettre child:[]
// par MarshalJSON, transformant toute feuille en categorie vide (arbre sans
// contenu jouable). D'ou le retour nil quand l'entree est nil.
func copieTronquee(ns []*Node) []*Node {
	if ns == nil {
		return nil
	}
	out := make([]*Node, len(ns))
	for i, n := range ns {
		c := *n // copie superficielle : partage add_time/limit_time (non modifies)
		c.Title = tronquerTitre(nettoyerTitreBoite(n.Title))
		c.Children = copieTronquee(n.Children)
		out[i] = &c
	}
	return out
}

// SerialiserBoite rend le JSON compact attendu par l'enceinte (titres tronques).
func SerialiserBoite(arbre []*Node) ([]byte, error) {
	return json.Marshal(copieTronquee(arbre))
}

// CompterFeuilles compte les feuilles (noeuds sans child). Un arbre Merlin sain
// en a forcement (les pistes audio). Zero feuille = arbre degenere (typiquement
// toutes les feuilles transformees en categories vides) : a refuser avant publication.
func CompterFeuilles(arbre []*Node) int {
	n := 0
	Parcourir(arbre, func(x, _ *Node) {
		if x.Children == nil {
			n++
		}
	})
	return n
}

// reUUIDCanonique : UUID 8-4-4-4-12 en hexadecimal, seule forme que
// uuidVersBinaire sait encoder (toute autre chaine y devient 16 octets nuls,
// c'est-a-dire l'UUID de la racine).
var reUUIDCanonique = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// VerifierUUIDs refuse un arbre dont un noeud n'a pas d'UUID canonique, ou porte
// celui de la racine : il serait encode comme une seconde racine dans pl_enc.bin,
// et ses medias (<uuid>.jpg/.mp3) ne seraient references par aucun noeud.
func VerifierUUIDs(arbre []*Node) error {
	var err error
	Parcourir(arbre, func(x, _ *Node) {
		if err == nil && (!reUUIDCanonique.MatchString(x.UUID) || x.UUID == UUIDRacine) {
			err = fmt.Errorf("identifiant invalide %q pour %q", x.UUID, x.Title)
		}
	})
	return err
}

// CompterFavoris compte les noeuds portant la cle favorite. Invariant du
// protocole : il doit y en avoir exactement un.
func CompterFavoris(arbre []*Node) int {
	n := 0
	Parcourir(arbre, func(x, _ *Node) {
		if x.Favorite != 0 {
			n++
		}
	})
	return n
}
