package playlist

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// record fabrique un enregistrement DecodeV2 (96 octets) pour les tests.
//
//	id@0 parent@2 order@4 childrenCount@6 kind@8 timestamp(u32)@10
//	favoriteOrder(u16)@14 UUID(16 octets)@16 titre(64 octets)@32
func record(id, parent, kind uint16, addTime uint32, uuid16 []byte, title string) []byte {
	b := make([]byte, RecordSizeV2)
	binary.LittleEndian.PutUint16(b[0:2], id)
	binary.LittleEndian.PutUint16(b[2:4], parent)
	binary.LittleEndian.PutUint16(b[8:10], kind)
	binary.LittleEndian.PutUint32(b[10:14], addTime)
	copy(b[16:32], uuid16)
	copy(b[32:RecordSizeV2], title)
	return b
}

// uuidAudio : 16 octets quelconques pour l'histoire de test.
var uuidAudio = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func fixture() []byte {
	var buf []byte
	buf = append(buf, record(1, 0, KindRoot, 0, nil, "Root")...)
	buf = append(buf, record(2, 1, KindFolderFavorite, 0, nil, "Favoris")...)
	buf = append(buf, record(3, 2, KindAudio, 12345, uuidAudio, "Histoire 1")...)
	buf = append(buf, record(4, 1, KindFolder, 0, nil, "Documentaires")...)
	return buf
}

func TestDecodeV2(t *testing.T) {
	items, err := DecodeV2(fixture())
	if err != nil {
		t.Fatalf("DecodeV2 : %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("attendu 4 enregistrements, obtenu %d", len(items))
	}
	if items[2].Title != "Histoire 1" {
		t.Errorf("titre audio mal decode : %+v", items[2])
	}
	if items[2].FileName != uuidDepuisBinaire(uuidAudio) {
		t.Errorf("uuid audio attendu %q, obtenu %q", uuidDepuisBinaire(uuidAudio), items[2].FileName)
	}
	if items[2].LimitTimeUnix != 12345 {
		t.Errorf("limit_time attendu 12345, obtenu %d", items[2].LimitTimeUnix)
	}
}

func TestDecodeV2TailleInvalide(t *testing.T) {
	if _, err := DecodeV2([]byte{1, 2, 3}); err == nil {
		t.Fatal("attendu une erreur pour une taille non multiple de 96")
	}
}

func TestSerialiserV2RoundTrip(t *testing.T) {
	// SerialiserV2 doit etre l'inverse exact de DecodeV2 (octet pour octet) sur des
	// enregistrements a titres null-paddes.
	buf := fixture()
	items, err := DecodeV2(buf)
	if err != nil {
		t.Fatalf("DecodeV2 : %v", err)
	}
	reenc := SerialiserV2(items)
	if !bytes.Equal(reenc, buf) {
		t.Fatalf("SerialiserV2(DecodeV2(buf)) != buf : %x vs %x", reenc, buf)
	}
	// uuidVersBinaire inverse uuidDepuisBinaire.
	if got := uuidVersBinaire(uuidDepuisBinaire(uuidAudio)); !bytes.Equal(got, uuidAudio) {
		t.Fatalf("uuidVersBinaire o uuidDepuisBinaire != id : %x", got)
	}
}

func TestSerialiserArbreV2RoundTrip(t *testing.T) {
	// Arbre -> Items -> octets -> re-decode -> arbre : la structure doit survivre.
	top := []*Node{
		{UUID: "11111111-1111-1111-1111-111111111111", Title: "Histoires", Children: []*Node{
			{UUID: "22222222-2222-2222-2222-222222222222", Title: "Serie", Children: []*Node{
				{UUID: "33333333-3333-3333-3333-333333333333", Title: "Une histoire"}, // feuille audio
			}},
		}},
		{UUID: "44444444-4444-4444-4444-444444444444", Title: "Merlin_favorite", Favorite: 1, Children: []*Node{}},
	}
	items := SerialiserArbreV2(top, nil)
	// Racine + 2 categories + 1 serie + 1 histoire = 5 records.
	if len(items) != 5 {
		t.Fatalf("attendu 5 records, obtenu %d", len(items))
	}
	if items[0].KindRaw != KindRoot || items[0].ID != 0 || items[0].ChildrenCount != 2 {
		t.Fatalf("racine mal formee : %+v", items[0])
	}
	arbre, err := BuildTree(DecodeV2Must(t, SerialiserV2(items)))
	if err != nil {
		t.Fatalf("BuildTree : %v", err)
	}
	if len(arbre) != 2 {
		t.Fatalf("attendu 2 categories, obtenu %d", len(arbre))
	}
	var fav *Node
	for _, n := range arbre {
		if n.Title == "Merlin_favorite" {
			fav = n
		}
	}
	if fav == nil || fav.Favorite != 1 {
		t.Fatalf("favori non reconstruit : %+v", fav)
	}
	// La feuille audio doit redescendre a 2 niveaux et garder son titre.
	h := arbre[0].Children[0].Children[0]
	if h.Title != "Une histoire" || h.Children != nil {
		t.Fatalf("histoire mal reconstruite : %+v", h)
	}
}

func DecodeV2Must(t *testing.T, buf []byte) []Item {
	t.Helper()
	items, err := DecodeV2(buf)
	if err != nil {
		t.Fatalf("DecodeV2 : %v", err)
	}
	return items
}

func TestFavorisDepuisBin(t *testing.T) {
	a := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	b := []byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	buf := append(append([]byte{}, a...), b...)
	fav := FavorisDepuisBin(buf)
	if len(fav) != 2 || fav[0] != uuidDepuisBinaire(a) || fav[1] != uuidDepuisBinaire(b) {
		t.Fatalf("favoris mal decodes : %v", fav)
	}
	// Reliquat de moins de 16 octets : ignore.
	if n := len(FavorisDepuisBin(append(append([]byte{}, a...), 9, 9, 9))); n != 1 {
		t.Fatalf("reliquat : attendu 1 favori, obtenu %d", n)
	}
	// fav.bin vide (0 octet) -> aucun favori, pas nil.
	if fav := FavorisDepuisBin(nil); fav == nil || len(fav) != 0 {
		t.Fatalf("vide : attendu liste vide non nil, obtenu %v", fav)
	}
}

func TestBuildTree(t *testing.T) {
	items, err := DecodeV2(fixture())
	if err != nil {
		t.Fatalf("DecodeV2 : %v", err)
	}
	racine, err := BuildTree(items)
	if err != nil {
		t.Fatalf("BuildTree : %v", err)
	}
	if len(racine) != 2 {
		t.Fatalf("attendu 2 noeuds de premier niveau, obtenu %d", len(racine))
	}
	// Le dossier favori doit porter favorite=1 et contenir l'histoire.
	var fav *Node
	for _, n := range racine {
		if n.Title == "Favoris" {
			fav = n
		}
	}
	if fav == nil {
		t.Fatal("dossier favori absent de l'arbre")
	}
	if fav.Favorite != 1 {
		t.Errorf("favorite attendu 1, obtenu %d", fav.Favorite)
	}
	if len(fav.Children) != 1 || fav.Children[0].Title != "Histoire 1" {
		t.Errorf("enfant du favori inattendu : %+v", fav.Children)
	}
}

func TestSummarize(t *testing.T) {
	items, _ := DecodeV2(fixture())
	r := Summarize(items, len(fixture()))
	if r.Total != 4 || r.Histoires != 1 || r.Dossiers != 2 || r.Categories != 2 {
		t.Errorf("resume inattendu : %+v", r)
	}
	if r.FavoriTitre != "Favoris" {
		t.Errorf("titre du favori attendu 'Favoris', obtenu %q", r.FavoriTitre)
	}
}

// Regression 2026-10-14 : en 2.1.23, une histoire (0x44) ou une musique (0x24)
// n'est PAS une radio ; seul le conteneur 0x12 (bit 0x10) l'est.
func TestDecodeV2HistoireNEstPasRadio(t *testing.T) {
	items := []Item{
		{ID: 0, Kind: KindRoot, KindRaw: 0x01, FileName: UUIDRacine, Title: "Root"},
		{ID: 1, ParentID: 0, KindRaw: 0x02, FileName: "11111111-1111-5111-8111-111111111111", Title: "Histoires", ChildrenCount: 2},
		{ID: 2, ParentID: 1, KindRaw: 0x44, FileName: "22222222-2222-5222-8222-222222222222", Title: "Berger"},
		{ID: 3, ParentID: 1, Order: 1, KindRaw: 0x24, FileName: "33333333-3333-5333-8333-333333333333", Title: "Chanson"},
		{ID: 4, ParentID: 0, Order: 1, KindRaw: 0x12, FileName: "44444444-4444-5444-8444-444444444444", Title: "Radio"},
	}
	dec, err := DecodeV2(SerialiserV2(items))
	if err != nil {
		t.Fatal(err)
	}
	arbre, err := BuildTree(dec)
	if err != nil {
		t.Fatal(err)
	}
	var radios []string
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.Webradio != 0 {
				radios = append(radios, n.Title)
			}
			walk(n.Children)
		}
	}
	walk(arbre)
	if len(radios) != 1 || radios[0] != "Radio" {
		t.Fatalf("noeuds webradio = %v, attendu [Radio]", radios)
	}
	// Re-encodage fidele : les kinds bruts sont conserves.
	for i, it := range dec {
		if it.KindRaw != items[i].KindRaw {
			t.Errorf("%s : KindRaw 0x%02x, attendu 0x%02x", it.Title, it.KindRaw, items[i].KindRaw)
		}
	}
}

func TestVerifierUUIDs(t *testing.T) {
	ok := "11111111-1111-5111-8111-111111111111"
	feuille := func(u string) []*Node {
		return []*Node{{UUID: ok, Title: "Cat", Children: []*Node{{UUID: u, Title: "Histoire"}}}}
	}
	if err := VerifierUUIDs(feuille("22222222-2222-4222-8222-222222222222")); err != nil {
		t.Errorf("arbre conforme refuse : %v", err)
	}
	for _, u := range []string{"new-1728900000000-42", "", UUIDRacine, "11111111-1111-5111-8111-11111111111"} {
		if err := VerifierUUIDs(feuille(u)); err == nil {
			t.Errorf("uuid %q accepte", u)
		}
	}
}

// B17 : les Items de SerialiserArbreV2 se relisent comme ceux de DecodeV2 (une
// histoire 0x44 n'est pas une radio ; le conteneur radio 0x12 en est une).
func TestSerialiserArbreV2KindCommeDecodeV2(t *testing.T) {
	u := func(c byte) string {
		return strings.Repeat(string(c), 8) + "-" + strings.Repeat(string(c), 4) + "-4" + strings.Repeat(string(c), 3) + "-8" + strings.Repeat(string(c), 3) + "-" + strings.Repeat(string(c), 12)
	}
	arbre := []*Node{
		{UUID: u('a'), Title: "Cat", Favorite: 1, Children: []*Node{{UUID: u('b'), Title: "Histoire"}}},
		{UUID: u('c'), Title: "Radio", Webradio: 1, Children: []*Node{{UUID: u('d'), Title: "Station", Children: []*Node{}}}},
	}
	items := SerialiserArbreV2(arbre, nil)
	relus, err := DecodeV2(SerialiserV2(items))
	if err != nil {
		t.Fatal(err)
	}
	for i := range items {
		if items[i].Kind != relus[i].Kind {
			t.Errorf("item %d (%s) : Kind %#x, DecodeV2 donne %#x", i, items[i].Title, items[i].Kind, relus[i].Kind)
		}
	}
	tree, err := BuildTree(items)
	if err != nil {
		t.Fatal(err)
	}
	if h := tree[0].Children[0]; h.Webradio != 0 {
		t.Error("histoire marquee radio")
	}
	if tree[1].Webradio == 0 {
		t.Error("conteneur radio non marque")
	}
}

// B9 : @10 = limit_time (pose sur la feuille, jamais en add_time) ; @14 (saut de
// jingle) conserve a la republication via Connu.
func TestLimiteEtJingleConserves(t *testing.T) {
	h := "a0000000-0000-4000-8000-000000000002"
	items := []Item{
		{ID: 0, KindRaw: 0x01, Kind: 0x01, ChildrenCount: 1, FileName: UUIDRacine, Title: "Root"},
		{ID: 1, KindRaw: 0x0a, Kind: 0x0a, ChildrenCount: 1, FileName: "a0000000-0000-4000-8000-000000000001", Title: "Fav"},
		{ID: 2, ParentID: 1, KindRaw: 0x04, Kind: 0x04, FileName: h, Title: "H", LimitTimeUnix: 1814392800, SautJingle: 7},
	}
	relus, err := DecodeV2(SerialiserV2(items))
	if err != nil || relus[2].LimitTimeUnix != 1814392800 || relus[2].SautJingle != 7 {
		t.Fatalf("aller-retour binaire : %+v %v", relus[2], err)
	}
	arbre, err := BuildTree(relus)
	if err != nil {
		t.Fatal(err)
	}
	f := arbre[0].Children[0]
	if f.LimitTime == nil || *f.LimitTime != 1814392800 || f.AddTimeUnix != 0 {
		t.Errorf("feuille : limit_time=%v add_time=%d", f.LimitTime, f.AddTimeUnix)
	}
	re := SerialiserArbreV2(arbre, ConnusDepuis(relus))
	if re[2].SautJingle != 7 || re[2].LimitTimeUnix != 1814392800 {
		t.Errorf("republication : jingle=%d limite=%d", re[2].SautJingle, re[2].LimitTimeUnix)
	}
}
