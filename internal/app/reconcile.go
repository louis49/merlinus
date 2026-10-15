package app

// Projection de l'arbre de travail (IHM) vers l'arbre boite a publier.
//
// Modele LOCAL-FIRST : l'arbre de travail est la source de verite (structure,
// ordre, titres, add_time...). On le NETTOIE (applique _rem/_titre/_new, retire
// les drapeaux d'edition) sans jamais superposer un autre arbre. La relecture de
// la boite ne sert, cote PublierEditions, qu'a trois choses ponctuelles :
//   - reinjecter les categories systeme filtrees de l'IHM (Merlin_favorite porte
//     l'unique favorite ; Dernier ajouts) : l'EXCEPTION favoris ;
//   - resynchroniser les drapeaux geres par la boite (favorite/webradio,
//     encodes dans la playlist) ;
//   - verifier l'anti-wipe.

import (
	"fmt"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
)

// wNode : noeud de l'arbre de travail serialise par l'IHM. Child est un pointeur
// pour distinguer l'ABSENCE de cle (feuille) d'un tableau vide (categorie vide) :
// la boite fait cette distinction a la presence de `child`.
type wNode struct {
	UUID      string   `json:"uuid"`
	Title     string   `json:"title"`
	Child     *[]wNode `json:"child"`
	AddTime   uint32   `json:"add_time"`
	LimitTime *uint32  `json:"limit_time"`
	Favorite  int      `json:"favorite"`
	Webradio  int      `json:"webradio"`
	Titre     *string  `json:"_titre"` // renommage
	Rem       bool     `json:"_rem"`   // marque a retirer (cascade)
	New       bool     `json:"_new"`   // cree localement
	Img       string   `json:"_img"`   // illustration (data URL) a pousser
}

// imgUpload : une illustration a televerser sous {uuid}.jpg.
type imgUpload struct {
	uuid string
	data []byte
}

func titreTravail(w wNode) string {
	if w.Titre != nil && *w.Titre != "" {
		return *w.Titre
	}
	return w.Title
}

// feuillesSous collecte les uuids des feuilles sous un noeud de travail.
func feuillesSous(w wNode, out map[string]bool) {
	if w.Child == nil {
		out[w.UUID] = true
		return
	}
	for _, c := range *w.Child {
		feuillesSous(c, out)
	}
}

// reconcilierLocal nettoie l'arbre de travail en arbre boite. Renvoie l'arbre
// (contenu utilisateur, hors categories systeme), les illustrations a televerser
// et l'ensemble des feuilles RETIREES intentionnellement (pour l'anti-wipe).
func reconcilierLocal(travail []wNode) ([]*playlist.Node, []imgUpload, map[string]bool, []string, error) {
	var images []imgUpload
	var nouveaux []string
	retirees := map[string]bool{}

	var conv func(w wNode) (*playlist.Node, error)
	conv = func(w wNode) (*playlist.Node, error) {
		if w.Rem {
			feuillesSous(w, retirees) // retrait intentionnel (cascade)
			return nil, nil
		}
		if w.New {
			nouveaux = append(nouveaux, w.UUID) // media/illustration a televerser (verifie a la publication)
		}
		n := &playlist.Node{
			UUID: w.UUID, Title: titreTravail(w),
			AddTimeUnix: w.AddTime, LimitTime: w.LimitTime,
			Favorite: w.Favorite, Webradio: w.Webradio,
		}
		if w.Child != nil { // conteneur (meme vide) -> child present
			kids := []*playlist.Node{}
			for _, c := range *w.Child {
				cn, err := conv(c)
				if err != nil {
					return nil, err
				}
				if cn != nil {
					kids = append(kids, cn)
				}
			}
			n.Children = kids
		}
		if w.Img != "" {
			data, err := library.JPEG128DepuisDataURL(w.Img)
			if err != nil {
				return nil, fmt.Errorf("illustration de %q : %w", n.Title, err)
			}
			images = append(images, imgUpload{uuid: w.UUID, data: data})
		}
		return n, nil
	}

	var out []*playlist.Node
	for _, w := range travail {
		if playlist.EstCategorieSysteme(titreTravail(w)) {
			continue // les categories systeme sont reinjectees depuis la boite
		}
		n, err := conv(w)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if n != nil {
			out = append(out, n)
		}
	}
	return out, images, retirees, nouveaux, nil
}

// syncDrapeauxBoite recopie sur l'arbre a publier les drapeaux geres par la
// boite (favorite/webradio) depuis l'arbre reel, par UUID. Ces drapeaux
// sont encodes dans la playlist : les resynchroniser evite qu'un arbre local un
// peu ancien (ex. avant decodage du bit webradio) ne les perde.
func syncDrapeauxBoite(publie []*playlist.Node, reel []*playlist.Node) {
	idx := map[string]*playlist.Node{}
	playlist.Parcourir(reel, func(n, _ *playlist.Node) { idx[n.UUID] = n })
	playlist.Parcourir(publie, func(n, _ *playlist.Node) {
		if r := idx[n.UUID]; r != nil {
			n.Favorite = r.Favorite
			n.Webradio = r.Webradio
		}
	})
}
