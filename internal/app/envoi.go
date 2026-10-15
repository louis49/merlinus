package app

import (
	"log"
	"time"

	"github.com/louis49/merlinus/internal/device"
)

// Briques communes a la publication et a la restauration.

// televerserBride envoie un fichier (op1) en limitant les evenements de
// progression vers l'IHM a un toutes les 300 ms (plus le dernier).
func televerserBride(c *device.Client, nom string, data []byte, progres func(envoye int64)) error {
	var dernier time.Time
	return c.UploadFile(nom, data, func(sent, tot int) {
		if sent < tot && time.Since(dernier) < 300*time.Millisecond {
			return
		}
		dernier = time.Now()
		if progres != nil {
			progres(int64(sent))
		}
	})
}

// controlerEspace refuse (message utilisateur) si besoin + marge depasse
// l'espace libre annonce par la boite. Espace illisible : journalise, envoi tente.
func (a *App) controlerEspace(c *device.Client, besoin int64) error {
	libre, err := c.SizeAvailable()
	if err != nil {
		log.Printf("controle d'espace impossible (%v) : envoi tente", err)
		return nil
	}
	if besoin+margeEspace > int64(libre) {
		return errIHM("err_espace", "besoinOctets", besoin+margeEspace, "libreOctets", int64(libre))
	}
	return nil
}
