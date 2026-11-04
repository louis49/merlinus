// Horloge de la boite. Voir docs/HEURE.md.
//
// La boite n'a pas de fuseau : elle tient l'heure locale murale exprimee en
// secondes depuis 1970 "comme si c'etait de l'UTC" (u32). Lecture par op27,
// ecriture par la commande chiffree "set_time.cmd" (firmware >= 2.1.17) :
// IV(12) || AES-256-GCM(u32 LE)(4) || tag(16), cle = SHA-256(MAC op30), soit
// 32 octets exactement (la boite rejette toute autre longueur). L'ancien op8 en
// clair n'est pas employe (sans effet en 2.1.23).
package device

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/louis49/merlinus/internal/protocol"
)

const octetsHeureChiffree = 32

// LireHeure (op 27) renvoie l'heure murale de la boite.
func (c *Client) LireHeure() (int64, error) {
	p, err := c.command(protocol.OpGetTime, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 4 {
		return 0, fmt.Errorf("reponse trop courte pour op 27")
	}
	return int64(binary.LittleEndian.Uint32(p[:4])), nil
}

// chiffrerHeure construit le contenu de set_time.cmd. `alea` : source de l'IV
// (nil -> crypto/rand).
func chiffrerHeure(macBytes []byte, mural int64, alea io.Reader) ([]byte, error) {
	if mural < 0 || mural > math.MaxUint32 {
		return nil, fmt.Errorf("heure hors plage u32 : %d", mural)
	}
	blob, err := ChiffrerPlaylist(macBytes, binary.LittleEndian.AppendUint32(nil, uint32(mural)), alea)
	if err != nil {
		return nil, err
	}
	if len(blob) != octetsHeureChiffree {
		return nil, fmt.Errorf("set_time.cmd de %d octets (attendu %d)", len(blob), octetsHeureChiffree)
	}
	return blob, nil
}

// DefinirHeure regle l'horloge de la boite sur l'heure murale `mural` : lit le
// MAC (cle), chiffre, televerse set_time.cmd. La boite doit etre en mode point
// d'acces.
func (c *Client) DefinirHeure(mural int64) error {
	mac, err := c.MACBytes()
	if err != nil {
		return err
	}
	blob, err := chiffrerHeure(mac, mural, nil)
	if err != nil {
		return err
	}
	return c.EnvoyerCommandeRAM("set_time.cmd", blob)
}
