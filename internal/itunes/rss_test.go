package itunes

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestCharsetReaderRSS(t *testing.T) {
	// "café" encode en ISO-8859-1 (é = 0xE9).
	r, err := charsetReaderRSS("iso-8859-1", bytes.NewReader([]byte{0x63, 0x61, 0x66, 0xE9}))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "café" {
		t.Errorf("decodage latin1 : %q (attendu café)", b)
	}
	// windows-1252 : guillemet « (0x93) -> doit devenir du vrai UTF-8.
	r2, _ := charsetReaderRSS("windows-1252", bytes.NewReader([]byte{0x93}))
	if b, _ := io.ReadAll(r2); b[0] < 0x80 || len(b) < 2 {
		t.Errorf("windows-1252 non decode : % x", b)
	}
	// UTF-8 : passe-plat (ne doit rien alterer).
	r3, _ := charsetReaderRSS("utf-8", bytes.NewReader([]byte("déjà")))
	if b, _ := io.ReadAll(r3); string(b) != "déjà" {
		t.Errorf("passe-plat utf-8 : %q", b)
	}
	// Inconnu : passe-plat.
	r4, _ := charsetReaderRSS("charset-bidon", bytes.NewReader([]byte("abc")))
	if b, _ := io.ReadAll(r4); string(b) != "abc" {
		t.Errorf("inconnu : %q", b)
	}
}

// Un feedUrl mal forme (donnee externe d'iTunes) donne une erreur, pas une
// panique (requete nil dereferencee).
func TestURLInvalideSansPanique(t *testing.T) {
	if _, err := ParseFeedComplet(http.DefaultClient, "https://exa mple.com/feed"); err == nil {
		t.Error("ParseFeedComplet : erreur attendue")
	}
	if _, _, err := New().requete("https://exa mple.com/x"); err == nil {
		t.Error("requete : erreur attendue")
	}
}
