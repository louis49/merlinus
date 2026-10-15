package app

import (
	"encoding/json"
	"log"

	"github.com/louis49/merlinus/internal/playlist"
)

// arbrePrepare : l'arbre a publier et ce qu'il faut televerser pour lui.
type arbrePrepare struct {
	publie   []*playlist.Node
	images   []imgUpload // illustrations issues de l'arbre de travail (_img)
	nouveaux []string    // uuid des contenus ajoutes (medias en bibliotheque)
}

// preparerArbre construit l'arbre a publier, SANS reseau ni disque : a partir
// de l'arbre reel relu sur la boite et de l'arbre de travail (JSON, vide = pas
// d'edition, republication a l'identique). Applique les editions, reinjecte les
// categories systeme de la boite, resynchronise les drapeaux geres par la boite,
// puis les garde-fous : anti-wipe (aucune feuille de la boite ne disparait sans
// retrait explicite), un seul favori, au moins une feuille, UUID canoniques.
// Les erreurs portent le message destine a l'utilisateur.
func preparerArbre(reel []*playlist.Node, travailJSON []byte) (arbrePrepare, error) {
	var p arbrePrepare
	retirees := map[string]bool{}
	if len(travailJSON) > 0 {
		var travail []wNode
		if err := json.Unmarshal(travailJSON, &travail); err != nil {
			return p, errDetail("err_lecture_editions", err)
		}
		publie, images, ret, nouveaux, err := reconcilierLocal(travail)
		if err != nil {
			return p, errDetail("err_preparation_editions", err)
		}
		p.images, p.nouveaux, retirees = images, nouveaux, ret
		// Reinjecter les categories systeme (favoris, derniers ajouts) depuis la boite.
		for _, r := range reel {
			if playlist.EstCategorieSysteme(r.Title) {
				publie = append(publie, r)
			}
		}
		syncDrapeauxBoite(publie, reel)
		pFeuilles := playlist.Feuilles(publie)
		for u := range playlist.Feuilles(reel) {
			if !pFeuilles[u] && !retirees[u] {
				log.Printf("anti-wipe : piste %s presente sur la boite mais absente de l'arbre a publier", u)
				return p, errIHM("err_anti_wipe")
			}
		}
		p.publie = publie
	} else {
		p.publie = reel
	}

	// Derniers ajouts : l'etagere montre les histoires ajoutees par cette
	// synchro (cf. derniers.go).
	regenererDerniersAjouts(p.publie, p.nouveaux, retirees, nbDerniersAjouts)

	if nf := playlist.CompterFavoris(p.publie); nf != 1 {
		return p, errIHM("err_favoris", "n", nf)
	}
	if playlist.CompterFeuilles(p.publie) == 0 {
		return p, errIHM("err_arbre_degenere")
	}
	if err := playlist.VerifierUUIDs(p.publie); err != nil {
		return p, errDetail("err_uuid_invalide", err)
	}
	return p, nil
}
