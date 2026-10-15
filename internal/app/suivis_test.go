package app

import "testing"

// Une serie de l'annuaire n'est suivie que si elle est dans l'arbre : sur la
// boite (meme a retirer) ou en attente d'ajout. Ajoutee puis retiree avant la
// synchro, elle ne l'est plus.
func TestSeriesSuivies(t *testing.T) {
	annuaire := map[string]SuiviSerie{
		"boite": {Title: "Sur la boite"}, "ajout": {Title: "En attente"},
		"retiree": {Title: "Ajoutee puis retiree"}, "a-retirer": {Title: "A retirer"},
	}
	e := EtatsUUID{SurBoite: []string{"boite", "a-retirer", "ep1"}, AttenteAjout: []string{"ajout"}, AttenteSuppr: []string{"a-retirer"}}
	got := seriesSuivies(annuaire, e)
	if len(got) != 3 || got["retiree"].Title != "" || got["boite"].Title == "" || got["ajout"].Title == "" || got["a-retirer"].Title == "" {
		t.Errorf("series suivies = %v", got)
	}
}
