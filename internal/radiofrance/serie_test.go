package radiofrance

import (
	"sort"
	"testing"
)

func TestCompareNaturel(t *testing.T) {
	in := []string{"Episode 10/12", "Episode 2/12", "Episode 1/12", "Episode 12/12", "Episode 9/12"}
	want := []string{"Episode 1/12", "Episode 2/12", "Episode 9/12", "Episode 10/12", "Episode 12/12"}
	sort.SliceStable(in, func(i, j int) bool { return compareNaturel(in[i], in[j]) < 0 })
	for k := range want {
		if in[k] != want[k] {
			t.Fatalf("ordre naturel incorrect : %v (attendu %v)", in, want)
		}
	}
	if compareNaturel("abc", "abc") != 0 {
		t.Error("egalite attendue")
	}
	if compareNaturel("A", "b") >= 0 {
		t.Error("insensible a la casse : A < b")
	}
	if compareNaturel("x2", "x2a") != -1 {
		t.Error("prefixe plus court d'abord")
	}
	if compareNaturel("00007", "7") != 0 {
		t.Error("zeros de tete ignores")
	}
}

// URL mal formee : erreur, pas de panique (requete nil dereferencee).
func TestRequeteURLInvalide(t *testing.T) {
	if _, _, err := New().requete("https://exa mple.com/x"); err == nil {
		t.Error("erreur attendue")
	}
}
