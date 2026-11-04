package app

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/louis49/merlinus/internal/device"
)

// toleranceHeure : ecart admis entre l'heure posee et celle relue juste apres
// (duree de l'echange, arrondi a la seconde).
const toleranceHeure = 5

// RegleHeure remet la boite mac a l'heure de l'ordinateur (heure murale
// locale, voir docs/HEURE.md). L'IHM l'appelle une fois par connexion, au
// passage deconnecte -> connecte. Renvoie false si la connexion est occupee ou
// la boite injoignable (l'IHM reessaie), true sinon : un echec est journalise,
// jamais affiche ni bloquant.
func (a *App) RegleHeure(mac string) (bool, error) {
	err := a.sonderBoite(mac, func(c *device.Client) error { return reglerHeure(c, mac, time.Now()) })
	if errors.Is(err, errOccupee) || errors.Is(err, errInjoignable) {
		return false, nil
	}
	if err != nil {
		log.Printf("heure %s : remise a l'heure impossible (%v)", mac, err)
	}
	return true, nil
}

// reglerHeure pose l'heure murale de `maintenant` sur la boite connectee et
// controle par relecture (op27) qu'elle a ete prise en compte.
func reglerHeure(c *device.Client, mac string, maintenant time.Time) error {
	avant, errAvant := c.LireHeure()
	mural := maintenantMural(maintenant)
	if err := c.DefinirHeure(mural); err != nil {
		return err
	}
	apres, err := c.LireHeure()
	if err != nil {
		return fmt.Errorf("relecture : %w", err)
	}
	if d := apres - mural; d < -toleranceHeure || d > toleranceHeure {
		return fmt.Errorf("heure non prise en compte : posee %s, relue %s", heureMurale(mural), heureMurale(apres))
	}
	ecart := "inconnu"
	if errAvant == nil {
		ecart = (time.Duration(avant-mural) * time.Second).String()
	}
	log.Printf("heure %s : boite %s -> %s (ecart avant reglage %s)", mac, heureMurale(avant), heureMurale(apres), ecart)
	return nil
}

// heureMurale formate une heure de la boite (murale encodee comme UTC).
func heureMurale(v int64) string {
	return time.Unix(v, 0).UTC().Format("2006-01-02 15:04:05")
}
