package app

import (
	"errors"
	"log"
	"time"

	"github.com/louis49/merlinus/internal/device"
)

// Connexion a la boite : une seule socket a la fois vers :50000 (connMu). Deux
// usages :
//   - sonde (sonderBoite) : lecture opportuniste au passage connecte, silencieuse
//     si la boite est occupee, injoignable ou si c'est une autre boite ;
//   - ecriture (connecterPourEcrire) : action demandee par l'utilisateur, avec
//     progression et messages, et controle d'identite avant toute ecriture.

var (
	errOccupee     = errors.New("occupee")
	errInjoignable = errors.New("injoignable")
	errAutreBoite  = errors.New("autre boite, ou identite illisible")
)

// sonderBoite ouvre une connexion courte vers la boite mac et y execute fn.
// Renvoie errOccupee, errInjoignable ou errAutreBoite sans rien executer dans
// ces cas (identite illisible = autre boite : on n'attribue rien a mac), sinon
// l'erreur de fn.
func (a *App) sonderBoite(mac string, fn func(c *device.Client) error) error {
	if !a.connMu.TryLock() {
		return errOccupee
	}
	defer a.connMu.Unlock()
	c, err := device.Dial(device.DefaultAddr(), 1500*time.Millisecond)
	if err != nil {
		return errInjoignable
	}
	defer c.Close()
	c.SetTimeout(3 * time.Second)
	if c.Ping() != nil {
		return errInjoignable
	}
	if m, err := c.MAC(); err != nil || !memeMAC(m, mac) {
		return errAutreBoite
	}
	return fn(c)
}

// connecterPourEcrire ouvre la connexion d'une operation d'ecriture sur la boite
// mac (publication, Wi-Fi, horaires, restauration) et y execute fn : progression
// "prog_connexion", jusqu'a 8 pings (la boite peut tarder a repondre juste
// apres le passage en AP), puis verifierIdentite (ne jamais ecrire la mauvaise
// boite). Les messages d'erreur sont destines a l'utilisateur.
func (a *App) connecterPourEcrire(mac string, fn func(c *device.Client) error) error {
	if !a.connMu.TryLock() {
		return errIHM("err_occupee")
	}
	defer a.connMu.Unlock()

	a.emitProgres("prog_connexion", 0)
	c, err := device.Dial(device.DefaultAddr(), 10*time.Second)
	if err != nil {
		return errIHM("err_injoignable")
	}
	defer c.Close()
	c.SetTimeout(3 * time.Second)
	pret := false
	for i := 0; i < 8 && !pret; i++ {
		pret = c.Ping() == nil
	}
	if !pret {
		return errIHM("err_pas_prete")
	}
	c.SetTimeout(15 * time.Second)
	if err := a.verifierIdentite(c, mac); err != nil {
		return err
	}
	return fn(c)
}

// joignabilite mesure le delai de detection de la boite (veille d'association,
// sonde EtatBoite) : journal aux seules transitions, pas a chaque tentative.
// La nature de l'echec distingue l'attente du Wi-Fi (delai depasse : machine
// pas encore sur l'AP) de celle de la boite (refus : AP joint, serveur pas pret).
type joignabilite struct {
	origine     string
	echecDepuis time.Time
	tentatives  int
	derniereErr string
}

func (j *joignabilite) echec(err error) {
	if j.echecDepuis.IsZero() {
		j.echecDepuis = time.Now()
	}
	j.tentatives++
	if e := err.Error(); e != j.derniereErr {
		log.Printf("%s : boite injoignable (%s)", j.origine, e)
		j.derniereErr = e
	}
}

func (j *joignabilite) succes() {
	if j.echecDepuis.IsZero() {
		return
	}
	log.Printf("%s : boite joignable apres %s (%d tentatives)",
		j.origine, time.Since(j.echecDepuis).Round(100*time.Millisecond), j.tentatives)
	*j = joignabilite{origine: j.origine}
}
