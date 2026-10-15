package playlist

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// Enregistrements reels releves sur la boite le 2026-10-14 (constructeur/lire-listening).
const ecoutesReelles = "" +
	"9f23877240a43553de8e943eff479c73c375ce6a76ee8c43ed42ed4200f0ffffffffffffffffffffffffff" +
	"52f92a80d8293a5ad0b23e16e4bbf2d6538ace6a7f5c074bd841d84180ffffffffffffffffffffffffffff" +
	"6d2500490ef6175aa5a4ce07dd7864df3d8cce6a998136a02d402d40ffffffffffffffffffffffffffffff"

func TestDecoderEcoutesReel(t *testing.T) {
	b, _ := hex.DecodeString(ecoutesReelles)
	got := DecoderEcoutes(b)
	if len(got) != 3 {
		t.Fatalf("%d ecoutes, attendu 3", len(got))
	}
	want := []Ecoute{
		{UUID: "7287239f-a440-5335-8ede-739c47ff3e94", Debut: 1791915459, Minutes: 12},
		{UUID: "802af952-29d8-5a3a-b2d0-d6f2bbe4163e", Debut: 1791920723, Minutes: 7},
		{UUID: "4900256d-f60e-5a17-a4a5-df6478dd07ce", Debut: 1791921213, Minutes: 0},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("#%d = %+v, attendu %+v", i, got[i], w)
		}
	}
	// Heure murale locale encodee "comme UTC" : 18:17 le soir du 13/10.
	if h := time.Unix(int64(got[0].Debut), 0).UTC().Format("2006-01-02 15:04"); h != "2026-10-13 18:17" {
		t.Errorf("debut = %s", h)
	}
}

func TestDecoderEcoutesSlotsVidesEtReste(t *testing.T) {
	b, _ := hex.DecodeString(ecoutesReelles[:TailleEcoute*2])
	b = append(b, bytes.Repeat([]byte{0xFF}, TailleEcoute)...) // slot efface
	b = append(b, 0x01, 0x02)                                  // reste incomplet
	if got := DecoderEcoutes(b); len(got) != 1 {
		t.Fatalf("%d ecoutes, attendu 1", len(got))
	}
	if got := DecoderEcoutes(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil -> %v, attendu liste vide", got)
	}
}
