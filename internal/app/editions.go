package app

import (
	"bytes"
	"encoding/json"

	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

// EditionsEnAttente indique si une publication changerait quelque chose sur la
// boite mac : l'arbre de travail est prepare exactement comme a la publication
// (preparerArbre, sur l'arbre de la boite en cache, sans reseau), puis compare a
// cet arbre ; des medias ou illustrations a envoyer comptent aussi. Un simple
// etat d'affichage du travail (noeud ouvert, _open) ne compte donc pas. Dans le
// doute (arbre illisible, preparation refusee), renvoie vrai : la publication
// affichera elle-meme la raison du refus.
func (a *App) EditionsEnAttente(mac string) bool {
	travail, err := store.Load(ficTravail(mac))
	if err != nil || len(travail) == 0 {
		return false // aucune edition
	}
	brut, err := store.Load(ficArbre(mac))
	if err != nil {
		return true
	}
	// Deux decodages : la preparation peut modifier l'arbre qu'on lui donne.
	var reel, reference []*playlist.Node
	if json.Unmarshal(brut, &reel) != nil || json.Unmarshal(brut, &reference) != nil {
		return true
	}
	prep, err := preparerArbre(reel, travail)
	if err != nil {
		return true
	}
	if len(prep.images) > 0 || len(prep.nouveaux) > 0 {
		return true
	}
	return !bytes.Equal(signatureArbre(prep.publie), signatureArbre(reference))
}

// signatureArbre : ce que la boite recevrait (titres nettoyes et tronques,
// structure, drapeaux), sans la date d'ajout locale, absente du binaire.
func signatureArbre(arbre []*playlist.Node) []byte {
	var sansDate func(ns []*playlist.Node) []*playlist.Node
	sansDate = func(ns []*playlist.Node) []*playlist.Node {
		if ns == nil {
			return nil
		}
		out := make([]*playlist.Node, len(ns))
		for i, n := range ns {
			c := *n
			c.AddTimeUnix = 0
			c.Children = sansDate(n.Children)
			out[i] = &c
		}
		return out
	}
	s, _ := playlist.SerialiserBoite(sansDate(arbre))
	return s
}
