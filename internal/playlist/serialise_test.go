package playlist

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTronquerTitreCourtInchange(t *testing.T) {
	for _, s := range []string{"", "Le loup", strings.Repeat("a", MaxTitreOctets)} {
		if got := tronquerTitre(s); got != s {
			t.Errorf("tronquerTitre(%q) = %q, attendu inchange", s, got)
		}
	}
}

func TestTronquerTitreASCII(t *testing.T) {
	got := tronquerTitre(strings.Repeat("a", MaxTitreOctets+1))
	attendu := strings.Repeat("a", MaxTitreOctets-len("…")) + "…"
	if got != attendu {
		t.Fatalf("tronquerTitre(64 x a) = %q (%d o), attendu %q", got, len(got), attendu)
	}
}

// Aucune rune multi-octets n'est coupee, et le resultat tient en 63 octets.
func TestTronquerTitreMultiOctets(t *testing.T) {
	cas := []string{
		strings.Repeat("é", 40),        // 2 octets
		strings.Repeat("€", 30),        // 3 octets
		strings.Repeat("𝄞", 20),        // 4 octets
		"a" + strings.Repeat("𝄞", 20),  // 4 octets decales : frontiere a 57
		"ab" + strings.Repeat("€", 30), // 3 octets decales
		strings.Repeat("Noël à l'école ", 6),
	}
	for _, s := range cas {
		got := tronquerTitre(s)
		if len(got) > MaxTitreOctets {
			t.Errorf("tronquerTitre(%q) : %d octets > %d", s, len(got), MaxTitreOctets)
		}
		if !utf8.ValidString(got) {
			t.Errorf("tronquerTitre(%q) = %q : UTF-8 invalide", s, got)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("tronquerTitre(%q) = %q : ellipse absente", s, got)
		}
		if !strings.HasPrefix(s, strings.TrimSuffix(got, "…")) {
			t.Errorf("tronquerTitre(%q) = %q : pas un prefixe de l'entree", s, got)
		}
	}
	// Cas precis : "a" + runes de 4 octets -> derniere frontiere <= 60 a l'octet 57.
	got := tronquerTitre("a" + strings.Repeat("𝄞", 20))
	if attendu := "a" + strings.Repeat("𝄞", 14) + "…"; got != attendu {
		t.Fatalf("frontiere de rune : %q, attendu %q", got, attendu)
	}
}

// Les espaces finaux avant l'ellipse sont retires.
func TestTronquerTitreEspaceFinal(t *testing.T) {
	s := strings.Repeat("a", 59) + "  " + strings.Repeat("b", 10)
	if got, attendu := tronquerTitre(s), strings.Repeat("a", 59)+"…"; got != attendu {
		t.Fatalf("tronquerTitre = %q, attendu %q", got, attendu)
	}
}

func TestNettoyerTitreBoite(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"Le loup 🐺 et l'agneau", "Le loup et l'agneau"},
		{"  ⭐️ Etoile  ", "Etoile"},
		{"Famille 👨‍👩‍👧 ok", "Famille ok"},
		{"1️⃣ Un", "1 Un"},
		{"Réveil ⏰ 🇫🇷 drapeau", "Réveil drapeau"},
		{"Été à Noël © ½", "Été à Noël © ½"},
		{"a\t\tb\n c", "a b c"},
		{"🎵🎶", TitreParDefaut}, // B18 : jamais de titre vide vers la boite
		{"   ", TitreParDefaut},
		{"", TitreParDefaut},
	}
	for _, c := range cas {
		if got := nettoyerTitreBoite(c.entree); got != c.attendu {
			t.Errorf("nettoyerTitreBoite(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
}

func TestCompterFavoris(t *testing.T) {
	arbre := []*Node{
		{UUID: "a", Title: "Merlin_favorite", Favorite: 1, Children: []*Node{}},
		{UUID: "b", Title: "Cat", Children: []*Node{
			{UUID: "c", Title: "Serie", Favorite: 2, Children: []*Node{{UUID: "d", Title: "H"}}},
			{UUID: "e", Title: "H2"},
		}},
	}
	if n := CompterFavoris(arbre); n != 2 {
		t.Fatalf("CompterFavoris = %d, attendu 2", n)
	}
	arbre[1].Children[0].Favorite = 0
	if n := CompterFavoris(arbre); n != 1 {
		t.Fatalf("CompterFavoris = %d, attendu 1", n)
	}
	if n := CompterFavoris(nil); n != 0 {
		t.Fatalf("CompterFavoris(nil) = %d, attendu 0", n)
	}
}

// Une categorie vide (child:[]) n'est pas une feuille.
func TestCompterFeuilles(t *testing.T) {
	arbre := []*Node{
		{UUID: "a", Title: "Vide", Children: []*Node{}},
		{UUID: "b", Title: "Cat", Children: []*Node{
			{UUID: "c", Title: "Serie", Children: []*Node{{UUID: "d"}, {UUID: "e"}}},
			{UUID: "f"},
			{UUID: "g", Children: []*Node{}},
		}},
	}
	if n := CompterFeuilles(arbre); n != 3 {
		t.Fatalf("CompterFeuilles = %d, attendu 3", n)
	}
	degenere := []*Node{{UUID: "a", Children: []*Node{{UUID: "b", Children: []*Node{}}}}}
	if n := CompterFeuilles(degenere); n != 0 {
		t.Fatalf("arbre degenere : CompterFeuilles = %d, attendu 0", n)
	}
}

// cles decode un JSON objet en carte cle -> valeur brute.
func cles(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("JSON invalide %s : %v", b, err)
	}
	return m
}

func TestMarshalJSONConteneurVide(t *testing.T) {
	b, err := json.Marshal(&Node{UUID: "u", Title: "Merlin_favorite", Children: []*Node{}, Favorite: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := cles(t, b)
	if string(m["child"]) != "[]" {
		t.Fatalf("conteneur vide : child = %s, attendu [] (json %s)", m["child"], b)
	}
	if string(m["favorite"]) != "1" {
		t.Fatalf("favorite = %s, attendu 1", m["favorite"])
	}
}

func TestMarshalJSONFeuille(t *testing.T) {
	b, err := json.Marshal(&Node{UUID: "u", Title: "Histoire", AddTimeUnix: 1700000000})
	if err != nil {
		t.Fatal(err)
	}
	m := cles(t, b)
	if _, ok := m["child"]; ok {
		t.Fatalf("feuille : cle child emise (json %s)", b)
	}
	if string(m["add_time"]) != "1700000000" {
		t.Fatalf("add_time = %s", m["add_time"])
	}
	// Drapeaux a zero et limit_time nil : jamais emis.
	for _, k := range []string{"favorite", "webradio", "discover", "limit_time"} {
		if _, ok := m[k]; ok {
			t.Errorf("cle %q emise alors que nulle (json %s)", k, b)
		}
	}
}

func TestMarshalJSONLimitEtDrapeaux(t *testing.T) {
	zero := uint32(0)
	b, err := json.Marshal(&Node{UUID: "u", Title: "R", LimitTime: &zero, Webradio: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := cles(t, b)
	// Pointeur non nil vers 0 : la cle est emise (seuls les drapeaux suivent la regle de presence).
	if string(m["limit_time"]) != "0" {
		t.Errorf("limit_time = %s, attendu 0", m["limit_time"])
	}
	if string(m["webradio"]) != "1" {
		t.Errorf("drapeau webradio=%s", m["webradio"])
	}
}

// Via SerialiserBoite (copieTronquee) : nil reste nil, [] reste [], titres
// nettoyes et bornes, entree non mutee.
func TestSerialiserBoiteNilVsVide(t *testing.T) {
	long := strings.Repeat("x", 80) + " 🐺"
	arbre := []*Node{
		{UUID: "c", Title: "Cat", Children: []*Node{{UUID: "f", Title: long}}},
		{UUID: "v", Title: "Vide", Children: []*Node{}},
	}
	b, err := SerialiserBoite(arbre)
	if err != nil {
		t.Fatal(err)
	}
	var brut []map[string]json.RawMessage
	if err := json.Unmarshal(b, &brut); err != nil {
		t.Fatal(err)
	}
	if string(brut[1]["child"]) != "[]" {
		t.Fatalf("categorie vide : child = %s, attendu []", brut[1]["child"])
	}
	var enfants []map[string]json.RawMessage
	if err := json.Unmarshal(brut[0]["child"], &enfants); err != nil {
		t.Fatal(err)
	}
	if _, ok := enfants[0]["child"]; ok {
		t.Fatalf("feuille devenue categorie : %s", b)
	}
	var titre string
	_ = json.Unmarshal(enfants[0]["title"], &titre)
	if len(titre) > MaxTitreOctets || !strings.HasSuffix(titre, "…") {
		t.Fatalf("titre non borne : %q", titre)
	}
	if arbre[0].Children[0].Title != long {
		t.Fatal("SerialiserBoite a mute l'arbre d'entree")
	}
}

// Avec une carte connu : kind brut et limite viennent de la boite, pas de
// l'arbre ; pour un uuid inconnu, defauts (0x44, LimitTime) ; la date d'ajout
// n'est jamais ecrite.
func TestSerialiserArbreV2AvecConnu(t *testing.T) {
	const (
		uCat = "11111111-1111-1111-1111-111111111111"
		uMus = "22222222-2222-2222-2222-222222222222"
		uNeu = "33333333-3333-3333-3333-333333333333"
	)
	lt5, lt7 := uint32(5), uint32(7)
	top := []*Node{
		{UUID: uCat, Title: "Musiques", Children: []*Node{
			{UUID: uMus, Title: "Chanson", LimitTime: &lt5, AddTimeUnix: 111},
			{UUID: uNeu, Title: "Nouvelle", LimitTime: &lt7, AddTimeUnix: 222},
		}},
	}
	connu := map[string]Connu{
		UUIDRacine: {Kind: KindRoot},
		uCat:       {Kind: 0x02},
		uMus:       {Kind: 0x24, Limite: 999},
	}
	items := SerialiserArbreV2(top, connu)
	if len(items) != 4 {
		t.Fatalf("attendu 4 records, obtenu %d", len(items))
	}
	type att struct {
		id, parent, order, enfants, kind uint16
		ts                               uint32
		uuid                             string
	}
	attendus := []att{
		{0, 0, 0, 1, KindRoot, 0, UUIDRacine},
		{1, 0, 0, 2, 0x02, 0, uCat},
		{2, 1, 0, 0, 0x24, 999, uMus}, // repris de connu (pas 0x04, pas 5)
		{3, 1, 1, 0, 0x04, 7, uNeu},   // inconnu : defauts (0x04 comme la boite), limite = LimitTime (pas 222)
	}
	for i, a := range attendus {
		it := items[i]
		if it.ID != a.id || it.ParentID != a.parent || it.Order != a.order || it.ChildrenCount != a.enfants ||
			it.KindRaw != a.kind || it.LimitTimeUnix != a.ts || it.FileName != a.uuid {
			t.Errorf("record %d = %+v, attendu %+v", i, it, a)
		}
	}
}

// connu nil et uuid inconnu sans LimitTime : horodatage a 0.
func TestSerialiserArbreV2SansLimite(t *testing.T) {
	items := SerialiserArbreV2([]*Node{{UUID: "11111111-1111-1111-1111-111111111111", Title: "H", AddTimeUnix: 1700000000}}, nil)
	if items[1].LimitTimeUnix != 0 {
		t.Fatalf("horodatage = %d, attendu 0 (la date d'ajout ne doit pas y aller)", items[1].LimitTimeUnix)
	}
}

func TestSerialiserArbreV2Titres(t *testing.T) {
	long := strings.Repeat("é", 50) + " 🐺"
	items := SerialiserArbreV2([]*Node{{UUID: "11111111-1111-1111-1111-111111111111", Title: long}}, nil)
	got := items[1].Title
	if len(got) > MaxTitreOctets || !utf8.ValidString(got) || strings.ContainsRune(got, '🐺') {
		t.Fatalf("titre pousse %q (%d o) : non nettoye ou non borne", got, len(got))
	}
}

// arbreRadio : favori vide, categorie d'histoires, conteneur webradio + 2 stations.
func arbreRadio() []*Node {
	return []*Node{
		{UUID: "aaaaaaaa-0000-0000-0000-000000000001", Title: "Merlin_favorite", Favorite: 1, Children: []*Node{}},
		{UUID: "aaaaaaaa-0000-0000-0000-000000000002", Title: "Histoires", Children: []*Node{
			{UUID: "aaaaaaaa-0000-0000-0000-000000000003", Title: "Le loup"},
		}},
		{UUID: "aaaaaaaa-0000-0000-0000-000000000004", Title: "Radio", Webradio: 1, Children: []*Node{
			{UUID: "aaaaaaaa-0000-0000-0000-000000000005", Title: "Station A"},
			{UUID: "aaaaaaaa-0000-0000-0000-000000000006", Title: "Station B"},
		}},
	}
}

func TestSerialiserArbreV2Webradio(t *testing.T) {
	items := SerialiserArbreV2(arbreRadio(), nil)
	if len(items) != 7 {
		t.Fatalf("attendu 7 records, obtenu %d", len(items))
	}
	attendus := map[string]uint16{
		"Root":            KindRoot,
		"Merlin_favorite": KindFolderFavorite, // 0x0a
		"Histoires":       KindFolder,         // 0x02
		"Le loup":         KindAudio,          // histoire nouvelle : 0x04
		"Radio":           0x12,               // conteneur webradio
		"Station A":       KindFolder,         // station : dossier vide
		"Station B":       KindFolder,
	}
	for _, it := range items {
		if k, ok := attendus[it.Title]; !ok || it.KindRaw != k {
			t.Errorf("%s : KindRaw 0x%02x, attendu 0x%02x", it.Title, it.KindRaw, k)
		}
	}
	if items[5].ParentID != items[4].ID || items[6].ParentID != items[4].ID || items[6].Order != 1 {
		t.Errorf("stations mal rattachees : %+v %+v", items[5], items[6])
	}
	if items[4].ChildrenCount != 2 || items[1].ChildrenCount != 0 {
		t.Errorf("childrenCount : radio=%d favori=%d", items[4].ChildrenCount, items[1].ChildrenCount)
	}
}

// Arbre -> Items -> pl.bin -> Items : champs encodes identiques, octets stables,
// et l'arbre relu garde le favori unique et le conteneur radio.
func TestSerialiserArbreV2AllerRetourWebradio(t *testing.T) {
	items := SerialiserArbreV2(arbreRadio(), nil)
	bin := SerialiserV2(items)
	dec := DecodeV2Must(t, bin)
	if len(dec) != len(items) {
		t.Fatalf("relu %d records, attendu %d", len(dec), len(items))
	}
	for i := range items {
		a, b := items[i], dec[i]
		if a.ID != b.ID || a.ParentID != b.ParentID || a.Order != b.Order || a.ChildrenCount != b.ChildrenCount ||
			a.KindRaw != b.KindRaw || a.LimitTimeUnix != b.LimitTimeUnix || a.SautJingle != b.SautJingle || a.FileName != b.FileName || a.Title != b.Title {
			t.Errorf("record %d : ecrit %+v, relu %+v", i, a, b)
		}
	}
	if !bytes.Equal(SerialiserV2(dec), bin) {
		t.Fatal("SerialiserV2(DecodeV2(bin)) != bin")
	}
	arbre, err := BuildTree(dec)
	if err != nil {
		t.Fatalf("BuildTree : %v", err)
	}
	if n := CompterFavoris(arbre); n != 1 {
		t.Fatalf("favoris apres aller-retour : %d, attendu 1", n)
	}
	var radio *Node
	for _, n := range arbre {
		if n.Title == "Radio" {
			radio = n
		}
	}
	if radio == nil || radio.Webradio != 1 || len(radio.Children) != 2 {
		t.Fatalf("conteneur radio mal relu : %+v", radio)
	}
	if h := arbre[1].Children[0]; h.Title != "Le loup" || h.Children != nil || h.Webradio != 0 {
		t.Fatalf("histoire mal relue : %+v", h)
	}
	// Re-serialiser l'arbre relu en reprenant les kinds de la boite donne les memes octets.
	if again := SerialiserV2(SerialiserArbreV2(arbre, ConnusDepuis(dec))); !bytes.Equal(again, bin) {
		t.Fatal("re-serialisation de l'arbre relu (avec connu) differente du pl.bin d'origine")
	}
}

// B19 : un Node serialise PAR VALEUR garde `child: []` (categorie vide).
func TestMarshalJSONParValeur(t *testing.T) {
	vide := Node{UUID: "u", Title: "Vide", Children: []*Node{}}
	for nom, v := range map[string]any{
		"tranche de valeurs": []Node{vide},
		"map de valeurs":     map[string]Node{"k": vide},
		"pointeur":           &vide,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s : %v", nom, err)
		}
		if !strings.Contains(string(b), `"child":[]`) {
			t.Errorf("%s : child vide perdu : %s", nom, b)
		}
	}
	var nul *Node
	if b, _ := json.Marshal(nul); string(b) != "null" {
		t.Errorf("pointeur nul : %s", b)
	}
}
