package app

import "testing"

// Controle du firmware a l'association : bloquant sous 2.1.23 ou si op5 est
// illisible, accepte a partir de 2.1.23 (y compris plus recent, non teste).
func TestControlerFirmware(t *testing.T) {
	cas := []struct {
		nom     string
		reponse []byte
		version string
		cle     string // "" = accepte
	}{
		{"valide", []byte{2, 1, 23, 0}, "2.1.23", ""},
		{"plus recent", []byte{2, 1, 24, 0}, "2.1.24", ""},
		{"majeure suivante", []byte{3, 0, 0, 0}, "3.0.0", ""},
		{"issue 2", []byte{2, 1, 16, 0}, "2.1.16", "err_firmware_ancien"},
		{"mineure anterieure", []byte{2, 0, 99, 0}, "2.0.99", "err_firmware_ancien"},
		{"reponse courte", []byte{2, 1}, "", "err_firmware_illisible"},
	}
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			b := nouvelleFausseBoite(t)
			b.firmware = tc.reponse
			v, err := controlerFirmware(b.connecter())
			if got := cleErreur(err); got != tc.cle {
				t.Fatalf("erreur = %v, cle attendue %q", err, tc.cle)
			}
			if tc.version != "" && v.String() != tc.version {
				t.Errorf("version = %s, attendu %s", v, tc.version)
			}
		})
	}
}
