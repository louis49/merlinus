package playlist

// Categories gerees par la boite elle-meme : favoris de l'enfant et etagere
// "derniers ajouts". Masquees de l'arbre editable de l'IHM, reinjectees telles
// quelles a la publication. SOURCE UNIQUE (Go et IHM, via le binding ArbreBoite)
// : les deux orthographes de "derniers ajouts" observees en reel sont couvertes.
var categoriesSysteme = map[string]bool{
	"Merlin_favorite": true,
	"Dernier ajouts":  true,
	"Derniers ajouts": true,
}

// EstCategorieSysteme : le titre est-il celui d'une categorie geree par la boite ?
func EstCategorieSysteme(titre string) bool { return categoriesSysteme[titre] }

// EstDerniersAjouts : le titre est-il celui de l'etagere "derniers ajouts" ?
func EstDerniersAjouts(titre string) bool {
	return titre == "Dernier ajouts" || titre == "Derniers ajouts"
}
