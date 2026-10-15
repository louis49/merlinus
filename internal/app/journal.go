package app

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/louis49/merlinus/internal/store"
)

// tailleMaxJournal : au-dela, le journal est archive en merlinus.log.1 au
// demarrage (un seul fichier precedent garde).
const tailleMaxJournal = 1 << 20

// ouvrirJournal duplique le journal (log.Printf) dans <dossier de donnees>/
// merlinus.log : une app lancee depuis le Finder, le menu Demarrer (pas de
// console sous Windows) ou un lanceur Linux n'a pas de sortie d'erreur visible.
// Contenu : MAC de la boite, uuid, noms de fichiers ; jamais le mot de passe
// Wi-Fi. Echec silencieux : le journal reste alors sur stderr seulement.
func ouvrirJournal() io.Closer {
	dir, err := store.Dir()
	if err != nil {
		return nil
	}
	f, err := ouvrirJournalDans(dir)
	if err != nil {
		return nil
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return f
}

// ouvrirJournalDans ouvre dir/merlinus.log en ajout, apres archivage de
// l'ancien s'il depasse tailleMaxJournal.
func ouvrirJournalDans(dir string) (*os.File, error) {
	chemin := filepath.Join(dir, "merlinus.log")
	if st, err := os.Stat(chemin); err == nil && st.Size() > tailleMaxJournal {
		ancien := chemin + ".1"
		_ = os.Remove(ancien) // Windows : Rename n'ecrase pas une cible existante
		_ = os.Rename(chemin, ancien)
	}
	return os.OpenFile(chemin, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
