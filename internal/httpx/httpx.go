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
	// Sans429 : un 429 est rendu sans reessai (source qui limite le debit :
	// reessayer aussitot prolonge la limitation).
	Sans429 bool
}

// Reponse : corps (borne au plafond), statut (0 sur erreur reseau) et en-tetes
// de la derniere tentative.
type Reponse struct {
	Corps   []byte
	Statut  int
	Entetes http.Header
}

// Get effectue un GET avec reessais sur echec transitoire (erreur reseau ou
// statut 5xx/429). Un 4xx est deterministe : rendu immediatement, l'appelant
// decide. Le statut vaut 0 en cas d'erreur reseau ; une URL invalide est une
// erreur immediate (sans reessai).
func Get(c *http.Client, u string, o Options) (body []byte, status int, err error) {
	r, err := GetComplet(c, u, o)
	return r.Corps, r.Statut, err
}

// GetComplet : comme Get, en rendant aussi les en-tetes de la reponse.
func GetComplet(c *http.Client, u string, o Options) (r Reponse, err error) {
	for essai := 0; ; essai++ {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return Reponse{}, err
		}
		if o.UA != "" {
			req.Header.Set("User-Agent", o.UA)
		}
		resp, e := c.Do(req)
		if e != nil {
			r, err = Reponse{}, e
		} else {
			b, re := io.ReadAll(io.LimitReader(resp.Body, o.Plafond))
			resp.Body.Close()
			r, err = Reponse{Corps: b, Statut: resp.StatusCode, Entetes: resp.Header}, re
		}
		transitoire := err != nil || r.Statut >= 500 || (r.Statut == 429 && !o.Sans429)
		if !transitoire || essai >= len(o.Delais) {
			return r, err
		}
		time.Sleep(o.Delais[essai])
	}
}
