package radiofrance

// Limiteur et disjoncteur des requetes vers Radio France (BUGS.md B3).
//
// Le site est derriere un CDN qui refuse (403) un client trop gourmand : une
// rafale de ~470 requetes en ~7 s au demarrage a suffi, pour un blocage de
// quelques secondes a ~20 min. Le declencheur exact n'est pas connu. Deux
// garde-fous, partages par toutes les requetes du Client :
//   - au plus maxSimultanees requetes en vol ;
//   - apres un 403 ou un 429, plus AUCUNE requete pendant la suspension
//     (Retry-After s'il est donne, sinon dureeSuspension) : insister prolonge le
//     blocage. Les appels echouent aussitot avec un *ErrRefus.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/httpx"
)

// maxSimultanees : requetes en vol vers Radio France, tous appels confondus.
const maxSimultanees = 6

// dureeSuspension : suspension apres un refus sans Retry-After exploitable
// (variable pour les tests).
var dureeSuspension = 5 * time.Minute

// Bornes appliquees a un Retry-After.
const (
	suspensionMin = 30 * time.Second
	suspensionMax = 30 * time.Minute
)

// ErrRefus : Radio France a refuse une requete (403 ou 429), ou la requete n'a
// pas ete envoyee parce qu'un refus recent a ouvert le disjoncteur.
type ErrRefus struct {
	Statut     int       // statut HTTP du refus
	Serveur    string    // en-tete Server du refus
	RetryAfter string    // en-tete Retry-After du refus (brut)
	Extrait    string    // debut du corps du refus, sur une ligne
	Jusqua     time.Time // fin de la suspension
	Suspendu   bool      // vrai : requete non envoyee (disjoncteur ouvert)
}

func (e *ErrRefus) Error() string {
	if e.Suspendu {
		return fmt.Sprintf("requetes Radio France suspendues jusqu'a %s (refus HTTP %d)",
			e.Jusqua.Format("15:04:05"), e.Statut)
	}
	return fmt.Sprintf("Radio France refuse la requete : HTTP %d (Server %q, Retry-After %q) %q",
		e.Statut, e.Serveur, e.RetryAfter, e.Extrait)
}

// EstRefus indique si err provient d'un refus de Radio France.
func EstRefus(err error) (*ErrRefus, bool) {
	var r *ErrRefus
	ok := errors.As(err, &r)
	return r, ok
}

// suspendu renvoie l'erreur a rendre sans envoyer de requete si le disjoncteur
// est ouvert, nil sinon.
func (c *Client) suspendu() error {
	c.disjMu.Lock()
	defer c.disjMu.Unlock()
	if c.refus == nil || !time.Now().Before(c.refus.Jusqua) {
		return nil
	}
	r := *c.refus
	r.Suspendu = true
	return &r
}

// disjoncter ouvre (ou prolonge) la suspension apres un refus et le journalise
// une seule fois par suspension.
func (c *Client) disjoncter(u string, rep httpx.Reponse) *ErrRefus {
	maintenant := time.Now()
	d := dureeSuspension
	ra := rep.Entetes.Get("Retry-After")
	if v, ok := delaiRetryAfter(ra, maintenant); ok {
		d = min(max(v, suspensionMin), suspensionMax)
	}
	e := &ErrRefus{
		Statut: rep.Statut, Serveur: rep.Entetes.Get("Server"), RetryAfter: ra,
		Extrait: uneLigne(string(rep.Corps), 200), Jusqua: maintenant.Add(d),
	}
	c.disjMu.Lock()
	deja := c.refus != nil && maintenant.Before(c.refus.Jusqua)
	if !deja || e.Jusqua.After(c.refus.Jusqua) {
		c.refus = e
	}
	c.disjMu.Unlock()
	if !deja {
		log.Printf("radiofrance : refus HTTP %d sur %s (Server %q, Retry-After %q, corps %q) : requetes suspendues jusqu'a %s",
			e.Statut, uneLigne(u, 200), e.Serveur, e.RetryAfter, e.Extrait, e.Jusqua.Format("15:04:05"))
	}
	return e
}

// delaiRetryAfter lit un Retry-After (secondes ou date HTTP).
func delaiRetryAfter(v string, maintenant time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.Sub(maintenant), true
	}
	return 0, false
}

// uneLigne met s sur une ligne et le borne a n runes.
func uneLigne(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return string(r)
}
