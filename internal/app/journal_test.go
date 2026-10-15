package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOuvrirJournalDansRotation(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "merlinus.log")
	if err := os.WriteFile(chemin, []byte(strings.Repeat("x", tailleMaxJournal+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := ouvrirJournalDans(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("nouveau\n")
	f.Close()
	if b, _ := os.ReadFile(chemin); string(b) != "nouveau\n" {
		t.Errorf("journal courant = %d octets, attendu le seul nouveau contenu", len(b))
	}
	if st, err := os.Stat(chemin + ".1"); err != nil || st.Size() != tailleMaxJournal+1 {
		t.Errorf("archive .1 absente ou fausse : %v", err)
	}
	// Sous le seuil : ajout, pas d'archivage.
	f, _ = ouvrirJournalDans(dir)
	f.WriteString("suite\n")
	f.Close()
	if b, _ := os.ReadFile(chemin); string(b) != "nouveau\nsuite\n" {
		t.Errorf("ajout : %q", b)
	}
}
