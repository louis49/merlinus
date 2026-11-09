package app

import (
	"errors"
	"strings"
	"testing"
)

func TestBorner(t *testing.T) {
	if got := borner("a\n  b\tc", 50); got != "a b c" {
		t.Errorf("borner multi-lignes = %q", got)
	}
	got := borner(strings.Repeat("é", 10), 5) // 2 octets par rune : coupe a 4
	if got != "éé..." {
		t.Errorf("borner UTF-8 = %q", got)
	}
}

func TestJournaliserCatalogueSansErreur(t *testing.T) {
	journaliserCatalogue("liste", "radiofrance", "", nil) // ne doit rien faire
	journaliserCatalogue("serie", "itunes", "123", errors.New("x"))
}
