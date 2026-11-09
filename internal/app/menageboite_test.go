package app

import (
	"reflect"
	"testing"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

func TestFichiersARetirer(t *testing.T) {
	const (
		garde   = "a0000000-0000-4000-8000-00000000000a"
		retire  = "a0000000-0000-4000-8000-00000000000b"
		deplace = "a0000000-0000-4000-8000-00000000000c"
	)
	avant := map[string]bool{garde: true, retire: true, deplace: true, uuidNul: true, "Cat": true, "A0000000-0000-4000-8000-00000000000D": true}
	apres := map[string]bool{garde: true, "A0000000-0000-4000-8000-00000000000C": true, "a0000000-0000-4000-8000-00000000000e": true} // deplace, en majuscules
	got := fichiersARetirer(avant, apres)
	want := []string{retire + ".aac", retire + ".jpg", retire + ".mp3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fichiersARetirer = %v, attendu %v", got, want)
	}
	if got := fichiersARetirer(map[string]bool{garde: true}, map[string]bool{garde: true}); len(got) != 0 {
		t.Errorf("rien de retire, obtenu %v", got)
	}
}

func TestAACAVerifier(t *testing.T) {
	boiteVierge(t, "02:00:00:00:5c:05") // store isole
	const (
		hAAC   = "a0000000-0000-4000-8000-0000000000a1"
		hMP3   = "a0000000-0000-4000-8000-0000000000a2"
		hNeuf  = "a0000000-0000-4000-8000-0000000000a3"
		radio  = "a0000000-0000-4000-8000-0000000000a4"
		statio = "a0000000-0000-4000-8000-0000000000a5"
	)
	for rel, d := range map[string][]byte{
		library.CheminAudio(hAAC, "aac"):   {1},
		library.CheminAudio(hMP3, "mp3"):   {1},
		library.CheminAudio(hNeuf, "aac"):  {1},
		library.CheminAudio(statio, "aac"): {1}, // station webradio : pas une histoire
	} {
		if err := store.Save(rel, d); err != nil {
			t.Fatal(err)
		}
	}
	arbre := []*playlist.Node{
		{UUID: pCat, Title: "Cat", Children: []*playlist.Node{{UUID: hAAC, Title: "A"}, {UUID: hMP3, Title: "M"}, {UUID: hNeuf, Title: "N"}}},
		{UUID: radio, Title: "Radio", Webradio: 1, Children: []*playlist.Node{{UUID: statio, Title: "S"}}},
	}
	if got := aacAVerifier(arbre, []string{hNeuf}); !reflect.DeepEqual(got, []string{hAAC}) {
		t.Errorf("aacAVerifier = %v, attendu [%s]", got, hAAC)
	}
}
