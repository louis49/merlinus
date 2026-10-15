// Plages horaires d'utilisation de la boite (quand elle accepte de jouer) et
// drapeau "ne pas couper l'histoire". Voir docs/HORAIRES.md.
//
// Format (29 octets), ecrit via le fichier de commande "set_enabling_hours.cmd"
// et lu via "get_enabling_hours.cmd" :
//
//	pour chaque jour d (0=Dimanche .. 6=Samedi) :
//	  [d*4+0]=heure debut  [d*4+1]=minute debut  [d*4+2]=heure fin  [d*4+3]=minute fin
//	[28] = waitForPlaybackEnd (0/1)
//
// Jour desactive = 00:00-00:00. Aller-retour confirme en reel sur 2.1.23.
package device

import "fmt"

// Plage : minutes depuis minuit, dans [0..1439]. Debut==Fin==0 => jour inactif.
type Plage struct {
	DebutMin int `json:"debutMin"`
	FinMin   int `json:"finMin"`
}

// Horaires : une plage par jour (index 0=Dimanche .. 6=Samedi, ordre firmware)
// et le drapeau "ne pas couper l'histoire".
type Horaires struct {
	Jours       [7]Plage `json:"jours"`
	NePasCouper bool     `json:"nePasCouper"` // waitForPlaybackEnd
}

const octetsHoraires = 29

func borne(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// encoder serialise les 29 octets attendus par la boite.
func (h Horaires) encoder() []byte {
	b := make([]byte, octetsHoraires)
	for d := 0; d < 7; d++ {
		p := h.Jours[d]
		deb := borne(p.DebutMin, 23*60+59)
		fin := borne(p.FinMin, 23*60+59)
		// Jour desactive = plage de duree nulle ou inversee : on normalise en
		// 00:00-00:00 (convention), coherent avec l'affichage "desactive" de l'IHM.
		if deb >= fin {
			deb, fin = 0, 0
		}
		b[d*4+0] = byte(deb / 60)
		b[d*4+1] = byte(deb % 60)
		b[d*4+2] = byte(fin / 60)
		b[d*4+3] = byte(fin % 60)
	}
	if h.NePasCouper {
		b[28] = 1
	}
	return b
}

// decoderHoraires lit les 29 octets renvoyes par get_enabling_hours.cmd.
func decoderHoraires(b []byte) (Horaires, error) {
	if len(b) != octetsHoraires {
		return Horaires{}, fmt.Errorf("taille inattendue pour les horaires : %d octets (attendu %d)", len(b), octetsHoraires)
	}
	var h Horaires
	for d := 0; d < 7; d++ {
		h.Jours[d] = Plage{
			DebutMin: int(b[d*4+0])*60 + int(b[d*4+1]),
			FinMin:   int(b[d*4+2])*60 + int(b[d*4+3]),
		}
	}
	h.NePasCouper = b[28] != 0
	return h, nil
}

// LireHoraires lit les plages actuellement configurees sur la boite (op13).
func (c *Client) LireHoraires() (Horaires, error) {
	b, err := c.GetFileBytes("get_enabling_hours.cmd")
	if err != nil {
		return Horaires{}, err
	}
	return decoderHoraires(b)
}

// DefinirHoraires pose les plages sur la boite (commande set_enabling_hours.cmd,
// op1). La boite doit etre en mode point d'acces.
func (c *Client) DefinirHoraires(h Horaires) error {
	return c.EnvoyerCommandeRAM("set_enabling_hours.cmd", h.encoder())
}
