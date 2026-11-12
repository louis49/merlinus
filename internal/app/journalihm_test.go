package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/radiofrance"
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

// Un refus Radio France (meme enveloppe) devient une ErreurIHM traduisible ;
// toute autre erreur passe telle quelle.
func TestErreurCatalogue(t *testing.T) {
	fin := time.Date(2026, 11, 12, 21, 5, 0, 0, time.Local)
	err := erreurCatalogue(fmt.Errorf("serie : %w", &radiofrance.ErrRefus{Statut: 403, Jusqua: fin}))
	var e *ErreurIHM
	if !errors.As(err, &e) || e.Cle != "err_rf_refus" || e.Params["heure"] != "21:05" {
		t.Errorf("refus : %v", err)
	}
	autre := errors.New("HTTP 500")
	if erreurCatalogue(autre) != autre || erreurCatalogue(nil) != nil {
		t.Error("erreur ordinaire modifiee")
	}
}
