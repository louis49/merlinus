// Package httpx : GET HTTP commun aux sources de catalogue (iTunes, Radio
// France) : User-Agent, plafond de taille, reessais sur echec transitoire.
package httpx

import (
	"io"
	"net/http"
	"time"
)

// Options d'une requete.
type Options struct {
	UA      string          // User-Agent envoye
	Plafond int64           // taille maximale lue du corps (octets)
	Delais  []time.Duration // attentes entre reessais (len = nombre de reessais)
}

// Get effectue un GET avec reessais sur echec transitoire (erreur reseau ou
// statut 5xx/429). Un 4xx est deterministe : rendu immediatement, l'appelant
// decide. Le statut vaut 0 en cas d'erreur reseau ; une URL invalide est une
// erreur immediate (sans reessai).
func Get(c *http.Client, u string, o Options) (body []byte, status int, err error) {
	for essai := 0; ; essai++ {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, 0, err
		}
		if o.UA != "" {
			req.Header.Set("User-Agent", o.UA)
		}
		resp, e := c.Do(req)
		if e != nil {
			body, status, err = nil, 0, e
		} else {
			b, re := io.ReadAll(io.LimitReader(resp.Body, o.Plafond))
			resp.Body.Close()
			body, status, err = b, resp.StatusCode, re
		}
		transitoire := err != nil || status >= 500 || status == 429
		if !transitoire || essai >= len(o.Delais) {
			return body, status, err
		}
		time.Sleep(o.Delais[essai])
	}
}
