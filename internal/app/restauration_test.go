package app

import (
	"reflect"
	"testing"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/playlist"
)

const (
	uA = "11111111-1111-5111-8111-111111111111" // histoire de la sauvegarde
	uB = "22222222-2222-5222-8222-222222222222" // histoire de la sauvegarde, effacee depuis
	uM = "33333333-3333-5333-8333-333333333333" // ajout Merlinus
	uC = "44444444-4444-5444-8444-444444444444" // categorie de la sauvegarde
)

func TestPlanRestauration(t *testing.T) {
	sauv := map[string]int64{
		uA + ".mp3": 100, uA + ".jpg": 10,
		uB + ".mp3": 200, uB + ".jpg": 20,
		uC + ".jpg":  30,
		"pl_enc.bin": 500, "fav.bin": 32, "wifi.cfg": 26,
	}
	carte := []device.FichierInfo{
		{Nom: uA + ".mp3", Taille: 100}, {Nom: uA + ".jpg", Taille: 10},
		{Nom: uC + ".jpg", Taille: 31}, // taille differente : a renvoyer
		{Nom: uM + ".mp3", Taille: 900}, {Nom: uM + ".jpg", Taille: 9},
		{Nom: "pl_enc.bin", Taille: 700}, {Nom: "pl.bin", Taille: 400},
		{Nom: "fav.bin", Taille: 48}, {Nom: "listening.bin", Taille: 43}, {Nom: "wifi.cfg", Taille: 26},
	}
	sup, env := planRestauration(carte, sauv)
	if want := []string{uM + ".jpg", uM + ".mp3"}; !reflect.DeepEqual(sup, want) {
		t.Errorf("a supprimer = %v, attendu %v (jamais fav.bin, listening.bin, wifi.cfg ni playlist)", sup, want)
	}
	if want := []string{uB + ".jpg", uB + ".mp3", uC + ".jpg"}; !reflect.DeepEqual(env, want) {
		t.Errorf("a envoyer = %v, attendu %v", env, want)
	}
}

func TestAutrePlaylist(t *testing.T) {
	// Sauvegarde chiffree : un pl.bin perime est a supprimer ; sauvegarde en
	// clair : c'est le pl_enc.bin pose par Merlinus (lu en priorite) qui l'est.
	if a := autrePlaylist("pl_enc.bin"); a != "pl.bin" {
		t.Errorf("autre de pl_enc.bin = %q", a)
	}
	if a := autrePlaylist("pl.bin"); a != "pl_enc.bin" {
		t.Errorf("autre de pl.bin = %q", a)
	}
}

func TestManquantsSauvegarde(t *testing.T) {
	items := []playlist.Item{
		{FileName: playlist.UUIDRacine, KindRaw: 0x01},
		{FileName: uC, KindRaw: 0x02},
		{FileName: uA, KindRaw: 0x44},
		{FileName: uA, KindRaw: 0x44}, // reference en double (Derniers ajouts)
		{FileName: uB, KindRaw: 0x24},
		{FileName: uM, KindRaw: 0x44}, // audio en .aac (m4a remuxe)
	}
	presents := map[string]int64{uC + ".jpg": 1, uA + ".jpg": 1, uA + ".mp3": 1, uB + ".jpg": 1, uM + ".jpg": 1, uM + ".aac": 1}
	if m := manquantsSauvegarde(items, presents); !reflect.DeepEqual(m, []string{uB + ".mp3"}) {
		t.Fatalf("manquants = %v", m)
	}
}

func TestConserveAuRetrait(t *testing.T) {
	for nom, garde := range map[string]bool{
		"sauvegarde": true, "restauration-20261014-104933": true,
		"backups": false, "sauvegarde-x": false,
	} {
		if conserveAuRetrait(nom) != garde {
			t.Errorf("conserveAuRetrait(%q) = %v", nom, !garde)
		}
	}
}
