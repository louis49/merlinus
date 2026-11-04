package app

import (
	"reflect"
	"testing"
	"time"
)

func TestReglerHeure(t *testing.T) {
	b := nouvelleFausseBoite(t)
	b.heure = 1000 // horloge de la boite tres en retard
	c := b.connecter()

	// 2026-11-03 09:40:00 a Paris (heure d'hiver, UTC+1).
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("base des fuseaux indisponible :", err)
	}
	maintenant := time.Date(2026, 11, 3, 9, 40, 0, 0, paris)
	if err := reglerHeure(c, macTestTexte, maintenant); err != nil {
		t.Fatal(err)
	}
	// Heure murale = 09:40 "comme UTC".
	if want := time.Date(2026, 11, 3, 9, 40, 0, 0, time.UTC).Unix(); b.heure != want {
		t.Errorf("heure posee = %s, attendu %s", heureMurale(b.heure), heureMurale(want))
	}
	if got, want := b.operations(""), []string{"heure?", "upload set_time.cmd", "heure?"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sequence = %q, attendu %q", got, want)
	}
}

func TestReglerHeureSansEffet(t *testing.T) {
	b := nouvelleFausseBoite(t)
	b.heure = 1000
	b.heureFigee = true // acquitte, mais l'horloge ne bouge pas
	c := b.connecter()
	if err := reglerHeure(c, macTestTexte, time.Now()); err == nil {
		t.Error("heure non prise en compte non detectee")
	}
}
