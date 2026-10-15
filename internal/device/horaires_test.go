package device

import (
	"bytes"
	"testing"
)

func TestHorairesAllerRetour(t *testing.T) {
	var h Horaires
	// Valeurs distinctes par jour (cf. test reel).
	for d := 0; d < 7; d++ {
		h.Jours[d] = Plage{DebutMin: d*60 + 10, FinMin: d*60 + 200}
	}
	h.NePasCouper = true

	b := h.encoder()
	if len(b) != 29 {
		t.Fatalf("taille = %d, attendu 29", len(b))
	}
	if b[28] != 1 {
		t.Errorf("octet 28 (waitForPlaybackEnd) = %d, attendu 1", b[28])
	}
	got, err := decoderHoraires(b)
	if err != nil {
		t.Fatalf("decoder : %v", err)
	}
	if got != h {
		t.Errorf("aller-retour : %+v != %+v", got, h)
	}
}

func TestHorairesVecteurReel(t *testing.T) {
	// Payload distinctif effectivement ecrit puis relu sur la boite (hex).
	attendu := []byte{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 6, 30, 8, 40, 1,
	}
	var h Horaires
	h.Jours[0] = Plage{1*60 + 2, 3*60 + 4}
	h.Jours[1] = Plage{5*60 + 6, 7*60 + 8}
	h.Jours[2] = Plage{9*60 + 10, 11*60 + 12}
	h.Jours[3] = Plage{13*60 + 14, 15*60 + 16}
	h.Jours[4] = Plage{17*60 + 18, 19*60 + 20}
	h.Jours[5] = Plage{21*60 + 22, 23*60 + 24}
	h.Jours[6] = Plage{6*60 + 30, 8*60 + 40}
	h.NePasCouper = true
	if b := h.encoder(); !bytes.Equal(b, attendu) {
		t.Errorf("encoder = % d\nattendu  = % d", b, attendu)
	}
}

func TestHorairesBorne(t *testing.T) {
	h := Horaires{}
	h.Jours[0] = Plage{DebutMin: -5, FinMin: 9999} // hors bornes
	b := h.encoder()
	if b[0] != 0 || b[1] != 0 {
		t.Errorf("debut negatif non borne a 00:00 : %d:%d", b[0], b[1])
	}
	if b[2] != 23 || b[3] != 59 {
		t.Errorf("fin hors borne non plafonnee a 23:59 : %d:%d", b[2], b[3])
	}
}

func TestHorairesJourDesactiveNormalise(t *testing.T) {
	var h Horaires
	h.Jours[1] = Plage{DebutMin: 10 * 60, FinMin: 10 * 60} // 10:00-10:00 (duree nulle)
	h.Jours[2] = Plage{DebutMin: 15 * 60, FinMin: 9 * 60}  // inversee
	b := h.encoder()
	// Lundi (index 1) -> octets 4..7 tous a 0
	for i := 4; i <= 7; i++ {
		if b[i] != 0 {
			t.Errorf("jour duree nulle non normalise : octet %d = %d", i, b[i])
		}
	}
	// Mardi (index 2) -> octets 8..11 tous a 0
	for i := 8; i <= 11; i++ {
		if b[i] != 0 {
			t.Errorf("jour inverse non normalise : octet %d = %d", i, b[i])
		}
	}
}
