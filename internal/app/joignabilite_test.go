package app

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

// Journal aux seules transitions : une ligne par nature d'echec, une ligne au
// retour, rien quand la boite reste joignable.
func TestJoignabiliteTransitions(t *testing.T) {
	var buf bytes.Buffer
	defer log.SetOutput(log.Writer())
	log.SetOutput(&buf)

	j := joignabilite{origine: "sonde"}
	j.succes() // joignable d'emblee : rien
	delai := errors.New("i/o timeout")
	j.echec(delai)
	j.echec(delai)
	j.echec(errors.New("connection refused"))
	j.succes()
	j.succes()

	lignes := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lignes) != 3 {
		t.Fatalf("%d lignes, attendu 3 :\n%s", len(lignes), buf.String())
	}
	for i, attendu := range []string{"injoignable (i/o timeout)", "injoignable (connection refused)", "joignable apres"} {
		if !strings.Contains(lignes[i], "sonde : boite "+attendu) {
			t.Errorf("ligne %d = %q, attendu %q", i, lignes[i], attendu)
		}
	}
	if !strings.Contains(lignes[2], "(3 tentatives)") {
		t.Errorf("compte de tentatives : %q", lignes[2])
	}
}
