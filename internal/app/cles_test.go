package app

import (
	"strings"
	"testing"
)

func TestMacKey(t *testing.T) {
	for mac, cle := range map[string]string{
		"38:18:2B:93:EF:6D": "38182b93ef6d",
		"38-18-2b-93-ef-6d": "38182b93ef6d",
		"..":                "_", // jamais vide : "" designerait le dossier de toutes les boites
		"../../x":           "x",
		"a/b\\c":            "abc",
	} {
		if k := macKey(mac); k != cle {
			t.Errorf("macKey(%q) = %q, attendu %q", mac, k, cle)
		}
	}
}

func TestUUIDMediaValide(t *testing.T) {
	for uuid, ok := range map[string]bool{
		"11111111-1111-5111-8111-111111111111": true,
		"":                                     false,
		"../../Users/x/Desktop/a":              false,
		"a/b":                                  false,
		"a.mp3":                                false,
	} {
		if err := uuidMediaValide(uuid); (err == nil) != ok {
			t.Errorf("uuidMediaValide(%q) : err=%v", uuid, err)
		}
	}
}

func TestNomFichierBoiteSain(t *testing.T) {
	for nom, ok := range map[string]bool{
		"11111111-1111-5111-8111-111111111111.mp3": true,
		"pl_enc.bin":          true,
		"sta_wifi.json":       true,
		"":                    false,
		".":                   false,
		"..":                  false,
		"../../boites.json":   false,
		"a/b.jpg":             false,
		"a\\b.jpg":            false,
		".cache":              false,
		"nom avec espace.mp3": false,
		"caf\xc3\xa9.mp3":     false,
		"wifi.cfg":            true,
		"listening.bin":       true,
		"a:b.mp3":             false, // ":" interdit sous Windows
		"x?.jpg":              false,
		"NUL":                 false, // noms reserves Windows
		"nul.txt":             false,
		"COM1.bin":            false,
		"console.bin":         true,
	} {
		if nomFichierBoiteSain(nom) != ok {
			t.Errorf("nomFichierBoiteSain(%q) != %v", nom, ok)
		}
	}
	if plafondSauvegarde(0) != 1<<32 || plafondSauvegarde(3908501504) != 3908501504 {
		t.Error("plafondSauvegarde")
	}
}

func TestMemeMAC(t *testing.T) {
	if !memeMAC("38:18:2B:93:EF:6D", "38182b93ef6d") || memeMAC("38:18:2b:93:ef:6d", "38:18:2b:93:ef:6e") {
		t.Error("memeMAC")
	}
}

// R3 : un registre indecodable n'est jamais reecrit ; absent = vide.
func TestModifierListe(t *testing.T) {
	ajout := func(l []BoiteMeta) []BoiteMeta { return append(l, BoiteMeta{MAC: "aa:bb"}) }
	if _, err := modifierListe([]byte(`[{"mac":"11:22"`), ajout); err == nil {
		t.Error("registre tronque reecrit")
	}
	out, err := modifierListe(nil, ajout)
	if err != nil || !strings.Contains(string(out), `"mac":"aa:bb"`) {
		t.Errorf("registre absent : %s, %v", out, err)
	}
	out, err = modifierListe([]byte(`[{"mac":"11:22","nom":"Lou"}]`), ajout)
	if err != nil || !strings.Contains(string(out), `"11:22"`) || !strings.Contains(string(out), `"aa:bb"`) {
		t.Errorf("ajout : %s, %v", out, err)
	}
}
