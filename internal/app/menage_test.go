package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/store"
)

func TestMediasOrphelins(t *testing.T) {
	ref := "11111111-1111-5111-8111-111111111111"
	orph := "22222222-2222-5222-8222-222222222222"
	refs := map[string]bool{ref: true}
	noms := []string{
		ref + ".mp3", ref + ".jpg",
		"11111111-1111-5111-8111-111111111111",     // sans extension : garde
		"AAAAAAAA-1111-5111-8111-111111111111.aac", // casse differente, non reference
		orph + ".mp3", orph + ".jpg",
		"notes.txt", ".tmp-xyz", "pas-un-uuid.mp3",
	}
	want := []string{"AAAAAAAA-1111-5111-8111-111111111111.aac", orph + ".mp3", orph + ".jpg"}
	if got := mediasOrphelins(noms, refs); !reflect.DeepEqual(got, want) {
		t.Errorf("orphelins = %v, attendu %v", got, want)
	}
	// Reference en majuscules dans le fichier source : comparaison sans casse.
	if got := mediasOrphelins([]string{ref + ".mp3"}, map[string]bool{ref: true}); got != nil {
		t.Errorf("reference ignoree : %v", got)
	}
}

func TestAEvincer(t *testing.T) {
	t0 := time.Unix(1000, 0)
	f := []fichierCache{
		{"recent", 40, t0.Add(3 * time.Hour)},
		{"ancien", 40, t0},
		{"moyen", 40, t0.Add(time.Hour)},
	}
	if got := aEvincer(f, 120); got != nil {
		t.Errorf("sous le plafond : %v", got)
	}
	if got := aEvincer(f, 100); !reflect.DeepEqual(got, []string{"ancien"}) {
		t.Errorf("plafond 100 : %v", got)
	}
	if got := aEvincer(f, 40); !reflect.DeepEqual(got, []string{"ancien", "moyen"}) {
		t.Errorf("plafond 40 : %v", got)
	}
}

// Seuls les fichiers vivants protegent un media : arbre de la boite, travail,
// perso, sauvegarde et archives de restauration ; pas les exports backups/,
// suivis.json ni ecoutes.json.
func TestReferencesBibliotheque(t *testing.T) {
	const k = "travail/boites/0200000077aa"
	for _, d := range []string{k, "cache/boites/0200000077aa"} {
		if err := store.RemoveDir(d); err != nil {
			t.Fatal(err)
		}
	}
	u := func(c byte) string {
		return string([]byte{c, c, c, c, c, c, c, c}) + "-0000-4000-8000-000000000000"
	}
	fichiers := map[string]string{
		k + "/travail.json":                            u('a'),
		"cache/boites/0200000077aa/arbre.json":         u('b'),
		k + "/sauvegarde/arbre.json":                   u('c'),
		k + "/restauration-20261015-090000/arbre.json": u('d'),
		k + "/backups/20261015-090000.json":            u('e'),
		k + "/suivis.json":                             u('f'),
		k + "/ecoutes.json":                            u('9'),
	}
	for f, c := range fichiers {
		if err := store.Save(f, []byte(`["`+c+`"]`)); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := referencesBibliotheque()
	if err != nil {
		t.Fatal(err)
	}
	for c, garde := range map[byte]bool{'a': true, 'b': true, 'c': true, 'd': true, 'e': false, 'f': false, '9': false} {
		if refs[u(c)] != garde {
			t.Errorf("%s : reference = %v, attendu %v", u(c), refs[u(c)], garde)
		}
	}
}
