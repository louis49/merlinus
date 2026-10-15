package app

import (
	"os"
	"testing"

	"github.com/louis49/merlinus/internal/store"
)

// TestMain redirige le store vers un dossier temporaire : aucun test du paquet
// n'ecrit dans les vraies donnees de l'utilisateur.
func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "merlinus-test-*")
	if err != nil {
		panic(err)
	}
	store.DefinirRacinePourTests(d)
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}
