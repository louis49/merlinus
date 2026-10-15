package app

// "Derniers ajouts". L'etagere systeme "Dernier ajouts" de la boite n'est
// affichee que si elle a des enfants, et c'est a l'emetteur de la playlist de la
// peupler : des FEUILLES qui referencent des histoires deja presentes ailleurs
// dans l'arbre (meme uuid, meme titre, meme kind ; cout de stockage nul).
//
// Regle : l'etagere montre les histoires AJOUTEES PAR LA DERNIERE SYNCHRO.
//   - publication avec ajouts : l'etagere est remplacee par ces ajouts ;
//   - publication sans ajout : l'etagere est conservee (elle decrit toujours la
//     derniere synchro qui a ajoute quelque chose), moins les histoires retirees.
// Exception de surete : une reference ORPHELINE (uuid absent du reste de
// l'arbre) qui n'a pas ete retiree volontairement est conservee, sinon le
// menage effacerait son media sans que l'utilisateur l'ait demande.

import "github.com/louis49/merlinus/internal/playlist"

// nbDerniersAjouts : nombre maximal d'histoires sur l'etagere.
const nbDerniersAjouts = 20

// feuillesHistoires indexe les histoires (feuilles hors conteneur webradio) de
// l'arbre, en ignorant le noeud `exclu` (l'etagere elle-meme). Premiere
// occurrence retenue.
func feuillesHistoires(arbre []*playlist.Node, exclu *playlist.Node) map[string]*playlist.Node {
	out := map[string]*playlist.Node{}
	var walk func(ns []*playlist.Node, sousRadio bool)
	walk = func(ns []*playlist.Node, sousRadio bool) {
		for _, n := range ns {
			if n == exclu {
				continue
			}
			if n.Children == nil {
				if !sousRadio && out[n.UUID] == nil {
					out[n.UUID] = n
				}
				continue
			}
			walk(n.Children, sousRadio || n.Webradio != 0)
		}
	}
	walk(arbre, false)
	return out
}

// regenererDerniersAjouts applique la regle ci-dessus a l'etagere (si elle
// existe). `nouveaux` : uuids ajoutes par cette publication (ordre de l'arbre) ;
// `retirees` : histoires retirees volontairement.
func regenererDerniersAjouts(publie []*playlist.Node, nouveaux []string, retirees map[string]bool, max int) {
	var etagere *playlist.Node
	for _, n := range publie {
		if playlist.EstDerniersAjouts(n.Title) && n.Children != nil {
			etagere = n
			break
		}
	}
	if etagere == nil {
		return
	}
	histoires := feuillesHistoires(publie, etagere)

	refs := []*playlist.Node{}
	pris := map[string]bool{}
	for _, u := range nouveaux {
		if len(refs) >= max {
			break
		}
		if h := histoires[u]; h != nil && !pris[u] {
			c := *h
			c.Children = nil // feuille
			refs = append(refs, &c)
			pris[u] = true
		}
	}
	avecAjouts := len(refs) > 0

	for _, r := range etagere.Children {
		if r.Children != nil || pris[r.UUID] || retirees[r.UUID] {
			continue
		}
		// Avec ajouts : les anciennes references presentes ailleurs sortent de
		// l'etagere ; seules les orphelines restent (surete du menage).
		if avecAjouts && histoires[r.UUID] != nil {
			continue
		}
		c := *r
		refs = append(refs, &c)
		pris[r.UUID] = true
	}
	etagere.Children = refs // jamais nil : l'etagere reste un conteneur
}
