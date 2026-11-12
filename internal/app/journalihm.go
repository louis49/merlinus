package app

import (
	"log"
	"strings"

	"github.com/louis49/merlinus/internal/radiofrance"
)

// Erreurs du catalogue et de l'interface dans le journal de l'app
// (merlinus.log). L'IHM affichait un message generique ("Impossible de charger
// le catalogue") sans trace : on ne savait pas si la cause etait le reseau, le
// site source ou l'affichage.

// journaliserCatalogue note l'echec d'un appel catalogue (rien si err est nil).
func journaliserCatalogue(op, source, detail string, err error) {
	if err == nil {
		return
	}
	if detail != "" {
		detail = " " + borner(detail, 120)
	}
	log.Printf("catalogue %s %s%s : %v", source, op, detail, err)
}

// erreurCatalogue traduit pour l'IHM un refus de Radio France (BUGS.md B3) ;
// les autres erreurs passent telles quelles (message generique cote IHM).
func erreurCatalogue(err error) error {
	if r, ok := radiofrance.EstRefus(err); ok {
		return errIHM("err_rf_refus", "heure", r.Jusqua.Format("15:04"))
	}
	return err
}

// JournaliserIHM ecrit dans le journal une erreur survenue dans l'interface
// (exception JavaScript ou appel Go rejete), sur une ligne et bornee.
func (a *App) JournaliserIHM(contexte, message string) {
	log.Printf("ihm : %s : %s", borner(contexte, 120), borner(message, 600))
}

// borner met s sur une ligne et le tronque a n octets (sans couper un rune).
func borner(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
