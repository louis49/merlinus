package device

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestChiffrerDechiffrerWifiAllerRetour(t *testing.T) {
	mac := []byte{0x38, 0x18, 0x2b, 0x93, 0xef, 0x6d}
	clair := "motdepasse-test-123"

	hexStr, err := chiffrerMotDePasseWifi(mac, clair, nil)
	if err != nil {
		t.Fatalf("chiffrement : %v", err)
	}
	// Hexa majuscule, longueur = 2 * (12 + len + 16).
	if hexStr != strings.ToUpper(hexStr) {
		t.Errorf("hexa non majuscule : %q", hexStr)
	}
	blob, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("hexa invalide : %v", err)
	}
	if want := 12 + len(clair) + 16; len(blob) != want {
		t.Errorf("taille blob = %d, attendu %d (IV12 + clair + tag16)", len(blob), want)
	}

	retour, err := dechiffrerMotDePasseWifi(mac, hexStr)
	if err != nil {
		t.Fatalf("dechiffrement : %v", err)
	}
	if retour != clair {
		t.Errorf("aller-retour : %q != %q", retour, clair)
	}

	// Un MAC different ne doit pas dechiffrer (tag GCM invalide).
	autreMac := []byte{0x38, 0x18, 0x2b, 0x93, 0xef, 0x6c}
	if _, err := dechiffrerMotDePasseWifi(autreMac, hexStr); err == nil {
		t.Errorf("dechiffrement reussi avec un mauvais MAC (devrait echouer)")
	}
}

func TestDechiffrerPlaylistAllerRetour(t *testing.T) {
	// Le pl_enc.bin reutilise le MEME schema que le mot de passe Wi-Fi (AES-256-GCM,
	// cle = SHA-256(MAC), IV(12) || chiffre || tag(16)). On fabrique donc un blob
	// avec chiffrerMotDePasseWifi (qui rend l'hexa de ce blob) et on verifie que
	// DechiffrerPlaylist le rend a l'identique. Cle = MAC SoftAP (op30 tel quel).
	mac := []byte{0x38, 0x18, 0x2b, 0x93, 0xef, 0x6d}
	clair := []byte("contenu-playlist-binaire-quelconque")
	hexStr, err := chiffrerMotDePasseWifi(mac, string(clair), nil)
	if err != nil {
		t.Fatalf("fabrication du blob : %v", err)
	}
	blob, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	got, err := DechiffrerPlaylist(mac, blob)
	if err != nil {
		t.Fatalf("DechiffrerPlaylist : %v", err)
	}
	if string(got) != string(clair) {
		t.Fatalf("clair different : got %q, attendu %q", got, clair)
	}
	// Mauvais MAC (dernier octet -1 = MAC base) : le tag GCM doit echouer.
	if _, err := DechiffrerPlaylist([]byte{0x38, 0x18, 0x2b, 0x93, 0xef, 0x6c}, blob); err == nil {
		t.Fatal("un mauvais MAC aurait du invalider le tag GCM")
	}
}

func TestContenuStaWifiEstUnTableau(t *testing.T) {
	contenu, err := contenuStaWifi("MonReseau", "DEADBEEF")
	if err != nil {
		t.Fatalf("serialisation : %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(contenu, &arr); err != nil {
		t.Fatalf("le contenu doit etre un tableau JSON : %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("attendu 1 entree, recu %d", len(arr))
	}
	e := arr[0]
	if e["ssid"] != "MonReseau" || e["pwd"] != "DEADBEEF" || e["encode"] != true {
		t.Errorf("entree inattendue : %v", e)
	}
}

func TestChiffrerRejetteMacInvalide(t *testing.T) {
	if _, err := chiffrerMotDePasseWifi([]byte{1, 2, 3}, "xxxxxxxx", nil); err == nil {
		t.Errorf("un MAC de 3 octets devrait etre rejete")
	}
}

func TestSSIDWifiMaison(t *testing.T) {
	c, _ := contenuStaWifi("Maison", "ABCD")
	if s := SSIDWifiMaison(c); s != "Maison" {
		t.Fatalf("SSID = %q", s)
	}
	for _, vide := range [][]byte{nil, []byte("[]"), []byte("pas du json")} {
		if s := SSIDWifiMaison(vide); s != "" {
			t.Fatalf("%q -> %q, attendu vide", vide, s)
		}
	}
}
