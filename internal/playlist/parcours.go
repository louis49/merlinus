package playlist

// Parcourir visite chaque noeud de l'arbre en profondeur, parent avant enfants
// (parent = nil pour un noeud de premier niveau). Les noeuds nil sont ignores.
func Parcourir(arbre []*Node, fn func(n, parent *Node)) {
	var walk func(ns []*Node, parent *Node)
	walk = func(ns []*Node, parent *Node) {
		for _, n := range ns {
			if n == nil {
				continue
			}
			fn(n, parent)
			walk(n.Children, n)
		}
	}
	walk(arbre, nil)
}

// IndexTitres : uuid -> titre, pour les noeuds ayant un uuid et un titre.
func IndexTitres(arbre []*Node) map[string]string {
	idx := map[string]string{}
	Parcourir(arbre, func(n, _ *Node) {
		if n.UUID != "" && n.Title != "" {
			idx[n.UUID] = n.Title
		}
	})
	return idx
}

// UUIDs : ensemble des uuid (non vides) de l'arbre.
func UUIDs(arbre []*Node) map[string]bool {
	set := map[string]bool{}
	Parcourir(arbre, func(n, _ *Node) {
		if n.UUID != "" {
			set[n.UUID] = true
		}
	})
	return set
}

// Histoires : ensemble des uuid des HISTOIRES, feuilles qui portent un audio.
// Les feuilles sous un conteneur webradio sont des stations (logo seul), comme
// dans kindBrutV2.
func Histoires(arbre []*Node) map[string]bool {
	set := map[string]bool{}
	var walk func(ns []*Node, sousWebradio bool)
	walk = func(ns []*Node, sousWebradio bool) {
		for _, n := range ns {
			if n == nil {
				continue
			}
			if n.Children == nil {
				if !sousWebradio {
					set[n.UUID] = true
				}
				continue
			}
			walk(n.Children, sousWebradio || n.Webradio != 0)
		}
	}
	walk(arbre, false)
	return set
}

// Feuilles : ensemble des uuid des feuilles (noeuds sans child).
func Feuilles(arbre []*Node) map[string]bool {
	set := map[string]bool{}
	Parcourir(arbre, func(n, _ *Node) {
		if n.Children == nil {
			set[n.UUID] = true
		}
	})
	return set
}
