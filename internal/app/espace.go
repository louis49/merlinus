package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/louis49/merlinus/internal/store"
)

// Espace pendant une publication. Les medias retires de l'arbre ne sont
// supprimes que par clean_sd.cmd, APRES l'adoption du nouvel arbre, donc apres
// l'envoi des nouveaux fichiers. Pendant l'envoi, anciens et nouveaux
// fichiers coexistent : l'espace libre doit couvrir les octets a envoyer, meme
// si le bilan final (ajouts - retraits) tient dans la boite.

// margeEspace : reserve laissee libre (fichiers systeme qui grossissent :
// historique d'ecoute, journaux...).
const margeEspace = 5 << 20

type fichierEnvoi struct {
	Nom    string
	Taille int64
}

// octetsNets estime ce que l'envoi va reellement consommer sur la boite :
// un fichier deja present a la meme taille (inventaire connu, reprise apres
// coupure) ne coute rien ; un fichier remplace ne coute que la difference.
func octetsNets(envois []fichierEnvoi, connus map[string]uint32) int64 {
	var n int64
	for _, e := range envois {
		if t, ok := connus[e.Nom]; ok {
			if d := e.Taille - int64(t); d > 0 {
				n += d
			}
			continue
		}
		n += e.Taille
	}
	return n
}

// inventaireBoite charge l'inventaire persiste (nom de fichier -> taille).
func inventaireBoite(mac string) map[string]uint32 {
	inv := map[string]uint32{}
	if data, e := store.Load(ficTailles(mac)); e == nil {
		_ = json.Unmarshal(data, &inv)
	}
	if inv == nil { // fichier contenant "null" : Unmarshal remet la map a nil
		inv = map[string]uint32{}
	}
	return inv
}

func moLisible(o int64) string {
	if o < 0 {
		o = 0
	}
	return strings.Replace(fmt.Sprintf("%.1f", float64(o)/1048576), ".", ",", 1) + " Mo"
}
