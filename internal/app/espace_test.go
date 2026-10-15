package app

import (
	"testing"

	"github.com/louis49/merlinus/internal/store"
)

func TestOctetsNets(t *testing.T) {
	connus := map[string]uint32{
		"deja.mp3":   1000, // reprise : deja envoye, meme taille
		"pl_enc.bin": 300,  // arbre remplace
	}
	envois := []fichierEnvoi{
		{Nom: "neuf.mp3", Taille: 5000},
		{Nom: "neuf.jpg", Taille: 200},
		{Nom: "deja.mp3", Taille: 1000},
		{Nom: "pl_enc.bin", Taille: 350},
	}
	if n := octetsNets(envois, connus); n != 5000+200+50 {
		t.Fatalf("octetsNets = %d, attendu %d", n, 5250)
	}
	// Remplacement par plus petit : ne coute rien (ne libere rien non plus).
	if n := octetsNets([]fichierEnvoi{{Nom: "pl_enc.bin", Taille: 100}}, connus); n != 0 {
		t.Fatalf("remplacement plus petit = %d, attendu 0", n)
	}
}

func TestMoLisible(t *testing.T) {
	if s := moLisible(1572864); s != "1,5 Mo" {
		t.Fatalf("moLisible = %q", s)
	}
}

// Un fichier de tailles contenant "null" ne doit pas donner une map nil (une
// ecriture dedans paniquerait pendant la publication).
func TestInventaireBoiteJamaisNil(t *testing.T) {
	mac := "zz-test-inventaire-nil"
	defer store.RemoveDir(dirCacheBoite(mac))
	defer store.RemoveDir(dirTravailBoite(mac))
	if err := store.Save(ficTailles(mac), []byte("null")); err != nil {
		t.Fatal(err)
	}
	inv := inventaireBoite(mac)
	inv["x.mp3"] = 1 // paniquerait sur une map nil
}
