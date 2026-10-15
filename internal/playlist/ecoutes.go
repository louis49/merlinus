package playlist

import (
	"encoding/binary"
	"math/bits"
)

// TailleEcoute est la taille fixe d'un enregistrement de l'historique d'ecoute
// renvoye par GetFile("get_listening.cmd") (firmware 2.1.23 ; taille lue dans
// l'APK officielle, i74.java). Pas d'en-tete : les enregistrements s'enchainent,
// du plus ancien au plus recent.
const TailleEcoute = 43

// Ecoute est un enregistrement de l'historique d'ecoute de la boite.
//
// Format (decode en reel le 2026-10-14, cf. docs/PROTOCOLE-2.1.23.md) :
//
//	@0  16 o  UUID du contenu, segments octets-inverses (comme fav.bin)
//	@16 u32   debut d'ecoute, heure MURALE LOCALE de la boite encodee comme si
//	          c'etait de l'UTC (lire avec time.Unix(..).UTC(), jamais en local)
//	@20 u32, @24 u16 (repete en @26) : non identifies
//	@28 15 o  bitmap : un bit a 0 (depuis le MSB) par minute ecoutee ; 0xFF =
//	          flash effacee. 120 minutes au plus par enregistrement.
type Ecoute struct {
	UUID    string `json:"uuid"`
	Debut   uint32 `json:"debut"`   // heure murale locale "comme UTC" (voir ci-dessus)
	Minutes int    `json:"minutes"` // minutes ecoutees (bits a 0 du bitmap)
}

// DecoderEcoutes decode l'historique brut. Les enregistrements sans UUID valide
// (tout a 0x00 ou 0xFF) sont ignores ; un reste incomplet est ignore.
func DecoderEcoutes(b []byte) []Ecoute {
	out := []Ecoute{}
	for i := 0; i+TailleEcoute <= len(b); i += TailleEcoute {
		r := b[i : i+TailleEcoute]
		if uniforme(r[:16]) {
			continue
		}
		u := uuidDepuisBinaire(r[:16])
		if u == "" {
			continue
		}
		min := 0
		for _, o := range r[28:TailleEcoute] {
			min += 8 - bits.OnesCount8(o)
		}
		out = append(out, Ecoute{UUID: u, Debut: binary.LittleEndian.Uint32(r[16:20]), Minutes: min})
	}
	return out
}

// uniforme : vrai si tous les octets valent 0x00, ou tous 0xFF (slot vide).
func uniforme(b []byte) bool {
	z, f := true, true
	for _, o := range b {
		z = z && o == 0x00
		f = f && o == 0xFF
	}
	return z || f
}
