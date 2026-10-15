package app

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/playlist"
)

// carteFactice : fichiers presents sur la carte ; illisibles = presents mais
// dont la lecture echoue (coupure reseau).
type carteFactice struct {
	fichiers   map[string][]byte
	illisibles map[string]bool
}

func (cf carteFactice) lire(nom string) ([]byte, error) {
	if b, ok := cf.fichiers[nom]; ok && !cf.illisibles[nom] {
		return b, nil
	}
	return nil, errors.New("lecture impossible")
}

func (cf carteFactice) existe(nom string) (bool, error) {
	_, ok := cf.fichiers[nom]
	return ok, nil
}

func playlistDe(titre string) []byte {
	return playlist.SerialiserV2([]playlist.Item{{ID: 1, KindRaw: 0x01, FileName: playlist.UUIDRacine, Title: titre}})
}

func TestChoisirPlaylist(t *testing.T) {
	mac := []byte{0x24, 0x0a, 0xc4, 0x01, 0x02, 0x03}
	enc, err := device.ChiffrerPlaylist(mac, playlistDe("publie"), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dechiffrer := func(b []byte) ([]byte, error) { return device.DechiffrerPlaylist(mac, b) }
	perime := playlistDe("perime")

	cas := []struct {
		nom   string
		carte carteFactice
		titre string // titre attendu de la racine ; "" = erreur attendue
	}{
		{"pl_enc.bin seul", carteFactice{fichiers: map[string][]byte{"pl_enc.bin": enc}}, "publie"},
		{"pl.bin seul", carteFactice{fichiers: map[string][]byte{"pl.bin": perime}}, "perime"},
		{"les deux : pl_enc.bin gagne", carteFactice{fichiers: map[string][]byte{"pl_enc.bin": enc, "pl.bin": perime}}, "publie"},
		{"pl_enc.bin present mais illisible : pas de repli", carteFactice{
			fichiers:   map[string][]byte{"pl_enc.bin": enc, "pl.bin": perime},
			illisibles: map[string]bool{"pl_enc.bin": true},
		}, ""},
		{"aucun", carteFactice{fichiers: map[string][]byte{}}, ""},
	}
	for _, c := range cas {
		items, _, err := choisirPlaylist(c.carte.lire, c.carte.existe, dechiffrer)
		if c.titre == "" {
			if err == nil {
				t.Errorf("%s : erreur attendue, lu %q", c.nom, items[0].Title)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s : %v", c.nom, err)
			continue
		}
		if items[0].Title != c.titre {
			t.Errorf("%s : lu %q, attendu %q", c.nom, items[0].Title, c.titre)
		}
	}
}
