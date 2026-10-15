package library

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ---- fabrique de tags ID3v2 (a la main) ----

// synchsafe encode n sur 4 octets de 7 bits utiles.
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

// cadreID3 construit un cadre v2.3 (taille entiere) ou v2.4 (taille synchsafe).
func cadreID3(majeur byte, id string, corps []byte) []byte {
	out := []byte(id)
	if majeur >= 4 {
		out = append(out, synchsafe(len(corps))...)
	} else {
		t := make([]byte, 4)
		binary.BigEndian.PutUint32(t, uint32(len(corps)))
		out = append(out, t...)
	}
	out = append(out, 0, 0) // flags du cadre
	return append(out, corps...)
}

// tagID3 assemble l'en-tete ID3v2 (taille synchsafe) + cadres + bourrage, suivi
// d'un faux debut de flux MP3.
func tagID3(majeur, flags byte, bourrage int, cadres ...[]byte) []byte {
	var corps []byte
	for _, c := range cadres {
		corps = append(corps, c...)
	}
	corps = append(corps, make([]byte, bourrage)...)
	out := []byte{'I', 'D', '3', majeur, 0, flags}
	out = append(out, synchsafe(len(corps))...)
	out = append(out, corps...)
	return append(out, 0xFF, 0xFB, 0x90, 0x00) // en-tete de trame MP3 factice
}

// apic construit le corps d'un cadre APIC.
func apic(enc byte, mime string, desc []byte, image []byte) []byte {
	out := []byte{enc}
	out = append(out, mime...)
	out = append(out, 0, 3) // fin du mime, type 3 = couverture
	out = append(out, desc...)
	return append(out, image...)
}

// fausseImage simule un JPEG de taille > 127 (pour distinguer une taille
// synchsafe d'une taille entiere en v2.4).
func fausseImage(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	copy(b, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	return b
}

func TestTagsID3v23Latin1(t *testing.T) {
	img := fausseImage(40)
	data := tagID3(3, 0, 16,
		cadreID3(3, "TALB", append([]byte{0}, "Album"...)),
		cadreID3(3, "TIT2", append([]byte{0}, 'C', 'a', 'f', 0xE9, 0)), // "Cafe" accentue en latin1, termine par \0
		cadreID3(3, "APIC", apic(0, "image/jpeg", []byte("desc\x00"), img)),
	)
	titre, cover := lireTags(".mp3", data)
	if titre != "Caf\u00e9" {
		t.Errorf("titre = %q, attendu %q", titre, "Caf\u00e9")
	}
	if !bytes.Equal(cover, img) {
		t.Errorf("couverture de %d octets, attendu l'image source (%d)", len(cover), len(img))
	}
}

func TestTagsID3v24SynchsafeEtUTF8(t *testing.T) {
	img := fausseImage(300) // 300 octets : taille synchsafe != taille entiere
	data := tagID3(4, 0, 0,
		cadreID3(4, "APIC", apic(3, "image/png", []byte("\x00"), img)),
		cadreID3(4, "TIT2", append([]byte{3}, "L\u00e9o et l'\u00e9t\u00e9"...)),
	)
	titre, cover := lireTags(".mp3", data)
	if titre != "L\u00e9o et l'\u00e9t\u00e9" {
		t.Errorf("titre = %q", titre)
	}
	if !bytes.Equal(cover, img) {
		t.Errorf("couverture de %d octets, attendu %d", len(cover), len(img))
	}
}

func TestTagsID3UTF16(t *testing.T) {
	// UTF-16 avec BOM little-endian (FF FE) : "Ete" accentue.
	le := []byte{1, 0xFF, 0xFE, 0xC9, 0x00, 't', 0, 0xE9, 0x00, 0, 0}
	// UTF-16 avec BOM big-endian (FE FF).
	be := []byte{1, 0xFE, 0xFF, 0x00, 0xC9, 0, 't', 0x00, 0xE9}
	// Encodage 2 : UTF-16BE sans BOM.
	sansBOM := []byte{2, 0x00, 0xC9, 0, 't', 0x00, 0xE9}
	for nom, corps := range map[string][]byte{"LE": le, "BE": be, "BE sans BOM": sansBOM} {
		titre, _ := lireTags(".mp3", tagID3(3, 0, 0, cadreID3(3, "TIT2", corps)))
		if titre != "\u00c9t\u00e9" {
			t.Errorf("%s : titre = %q", nom, titre)
		}
	}

	// APIC dont la description est en UTF-16 (terminaison \0\0).
	img := fausseImage(20)
	desc := []byte{0xFF, 0xFE, 'a', 0, 'b', 0, 0, 0}
	_, cover := lireTags(".mp3", tagID3(3, 0, 0, cadreID3(3, "APIC", apic(1, "image/jpeg", desc, img))))
	if !bytes.Equal(cover, img) {
		t.Errorf("couverture (desc UTF-16) : %d octets, attendu %d", len(cover), len(img))
	}
}

// Seuls le premier TIT2 et le premier APIC sont retenus.
func TestTagsID3PremierCadreRetenu(t *testing.T) {
	img1, img2 := fausseImage(10), bytes.Repeat([]byte{7}, 10)
	data := tagID3(3, 0, 0,
		cadreID3(3, "TIT2", append([]byte{3}, "Premier"...)),
		cadreID3(3, "APIC", apic(0, "image/jpeg", []byte{0}, img1)),
		cadreID3(3, "TIT2", append([]byte{3}, "Second"...)),
		cadreID3(3, "APIC", apic(0, "image/jpeg", []byte{0}, img2)),
	)
	titre, cover := lireTags(".mp3", data)
	if titre != "Premier" || !bytes.Equal(cover, img1) {
		t.Errorf("titre=%q, cover=% x", titre, cover)
	}
}

// Cas non geres ou corrompus : valeurs vides, jamais de titre garbage ni de panique.
func TestTagsID3NonGeres(t *testing.T) {
	tit2 := cadreID3(3, "TIT2", append([]byte{3}, "Titre"...))
	cas := map[string][]byte{
		"pas de tag":         {0xFF, 0xFB, 0x90, 0x00, 1, 2, 3, 4, 5, 6, 7},
		"trop court":         []byte("ID3"),
		"v2.2":               tagID3(2, 0, 0, tit2),
		"unsynchronisation":  tagID3(3, 0x80, 0, tit2),
		"en-tete etendu":     tagID3(3, 0x40, 0, tit2),
		"cadre taille nulle": tagID3(3, 0, 0, cadreID3(3, "TIT2", nil), tit2),
		"encodage inconnu":   tagID3(3, 0, 0, cadreID3(3, "TIT2", append([]byte{9}, "x"...))),
	}
	for nom, data := range cas {
		if titre, cover := lireTags(".mp3", data); titre != "" || cover != nil {
			t.Errorf("%s : titre=%q cover=%d octets, attendu vide", nom, titre, len(cover))
		}
	}

	// Cadre annoncant une taille qui depasse le tag : arret propre, les cadres
	// precedents restent lus.
	deborde := cadreID3(3, "APIC", apic(0, "image/jpeg", []byte{0}, fausseImage(50)))
	binary.BigEndian.PutUint32(deborde[4:8], 1<<20)
	titre, cover := lireTags(".mp3", tagID3(3, 0, 0, tit2, deborde))
	if titre != "Titre" || cover != nil {
		t.Errorf("cadre debordant : titre=%q cover=%d octets", titre, len(cover))
	}

	// Taille d'en-tete superieure au fichier : bornee au fichier.
	data := tagID3(3, 0, 0, tit2)
	copy(data[6:10], synchsafe(1<<20))
	if titre, _ := lireTags(".mp3", data); titre != "Titre" {
		t.Errorf("tag tronque : titre=%q, attendu Titre", titre)
	}

	// Toutes les troncatures d'un tag complet : jamais de panique.
	complet := tagID3(4, 0, 4, tit2, cadreID3(4, "APIC", apic(1, "image/jpeg", []byte{0xFF, 0xFE, 'a', 0, 0, 0}, fausseImage(30))))
	for n := 0; n <= len(complet); n++ {
		_, _ = lireTags(".mp3", complet[:n])
	}
}

func TestImageAPICCorrompue(t *testing.T) {
	for nom, b := range map[string][]byte{
		"trop court":           {0, 'a', 0},
		"mime sans fin":        {0, 'i', 'm', 'a', 'g', 'e'},
		"rien apres le mime":   {0, 'i', 'm', 'g', 0},
		"desc sans fin":        {0, 'i', 0, 3, 'd', 'e', 's', 'c'},
		"pas d'image apres":    {0, 'i', 0, 3, 'd', 0},
		"desc UTF-16 sans fin": {1, 'i', 0, 3, 0xFF, 0xFE, 'a', 0},
	} {
		if img := imageAPIC(b); img != nil {
			t.Errorf("%s : image % x, attendu nil", nom, img)
		}
	}
}

// ---- MP4 / m4a ----

// entreeIlst construit une entree ilst : [typ [data [type:4][locale:4][valeur]]].
func entreeIlst(typ string, typeDonnee uint32, valeur []byte) []byte {
	return boite(typ, boite("data", u32(typeDonnee), u32(0), valeur))
}

// fichierMP4Tags construit ftyp + moov(mvhd factice, udta(meta(hdlr, ilst))) + mdat.
func fichierMP4Tags(entrees ...[]byte) []byte {
	hdlr := boite("hdlr", versionFlags(), u32(0), []byte("mdir"), []byte("appl"), make([]byte, 9))
	ilst := boite("ilst", entrees...)
	meta := boite("meta", versionFlags(), hdlr, ilst)
	moov := boite("moov", boite("mvhd", versionFlags(), make([]byte, 96)), boite("udta", meta))
	out := boite("ftyp", []byte("M4A "), u32(0))
	out = append(out, moov...)
	return append(out, boite("mdat", []byte{1, 2, 3, 4})...)
}

func TestTagsMP4(t *testing.T) {
	img := fausseImage(64)
	data := fichierMP4Tags(
		entreeIlst("\xa9ART", 1, []byte("Artiste")),
		entreeIlst("\xa9nam", 1, []byte("La f\u00e9e\x00")),
		entreeIlst("covr", 13, img),
	)
	for _, ext := range []string{".m4a", ".mp4", ".m4b", ".m4v"} {
		titre, cover := lireTags(ext, data)
		if titre != "La f\u00e9e" {
			t.Errorf("%s : titre = %q", ext, titre)
		}
		if !bytes.Equal(cover, img) {
			t.Errorf("%s : couverture de %d octets, attendu %d", ext, len(cover), len(img))
		}
	}
	// Extension non geree ou mauvaise famille : rien.
	if titre, cover := lireTags(".ogg", data); titre != "" || cover != nil {
		t.Errorf(".ogg : titre=%q cover=%d", titre, len(cover))
	}
	if titre, _ := lireTags(".mp3", data); titre != "" {
		t.Errorf("m4a lu comme mp3 : titre=%q", titre)
	}
}

// Atome de taille 0 (= jusqu'a la fin du conteneur parent) accepte.
func TestTagsMP4AtomeTailleZero(t *testing.T) {
	data := fichierMP4Tags(entreeIlst("\xa9nam", 1, []byte("Titre")))
	// Le mdat est le dernier atome du fichier : taille 0 autorisee.
	i := bytes.LastIndex(data, []byte("mdat"))
	copy(data[i-4:i], []byte{0, 0, 0, 0})
	// On remplace aussi la taille du moov par 0 apres avoir retire le mdat : le
	// moov devient le dernier atome.
	j := bytes.Index(data, []byte("moov"))
	sansMdat := append([]byte{}, data[:i-4]...)
	copy(sansMdat[j-4:j], []byte{0, 0, 0, 0})
	for nom, d := range map[string][]byte{"mdat taille 0": data, "moov taille 0": sansMdat} {
		if titre, _ := lireTags(".m4a", d); titre != "Titre" {
			t.Errorf("%s : titre = %q", nom, titre)
		}
	}
}

func TestTagsMP4Incomplets(t *testing.T) {
	sansCovr := fichierMP4Tags(entreeIlst("\xa9nam", 1, []byte("Seul")))
	if titre, cover := lireTags(".m4a", sansCovr); titre != "Seul" || cover != nil {
		t.Errorf("sans covr : titre=%q cover=%d", titre, len(cover))
	}
	sansNom := fichierMP4Tags(entreeIlst("covr", 13, []byte{1, 2}))
	if titre, cover := lireTags(".m4a", sansNom); titre != "" || !bytes.Equal(cover, []byte{1, 2}) {
		t.Errorf("sans nom : titre=%q cover=% x", titre, cover)
	}

	vides := map[string][]byte{
		"sans moov":       boite("ftyp", []byte("M4A ")),
		"moov sans udta":  boite("moov", boite("mvhd", make([]byte, 100))),
		"udta sans meta":  boite("moov", boite("udta", boite("free"))),
		"meta trop court": boite("moov", boite("udta", boite("meta", []byte{0, 0}))),
		"meta sans ilst":  boite("moov", boite("udta", boite("meta", versionFlags()))),
		"data trop court": fichierMP4Tags(boite("\xa9nam", boite("data", []byte{0, 0, 0, 1}))),
		"nam sans data":   fichierMP4Tags(boite("\xa9nam", boite("free", []byte("Titre")))),
	}
	for nom, d := range vides {
		if titre, cover := lireTags(".m4a", d); titre != "" || cover != nil {
			t.Errorf("%s : titre=%q cover=%d", nom, titre, len(cover))
		}
	}

	// Taille d'atome incoherente (< 8 et != 0, ou depassant le parent) : arret.
	corrompu := fichierMP4Tags(entreeIlst("\xa9nam", 1, []byte("Titre")))
	k := bytes.Index(corrompu, []byte("moov"))
	binary.BigEndian.PutUint32(corrompu[k-4:k], 5)
	if titre, _ := lireTags(".m4a", corrompu); titre != "" {
		t.Errorf("taille 5 : titre=%q, attendu vide", titre)
	}
	binary.BigEndian.PutUint32(corrompu[k-4:k], 1<<30)
	if titre, _ := lireTags(".m4a", corrompu); titre != "" {
		t.Errorf("taille demesuree : titre=%q, attendu vide", titre)
	}

	// Troncatures : jamais de panique.
	complet := fichierMP4Tags(entreeIlst("\xa9nam", 1, []byte("Titre")), entreeIlst("covr", 13, fausseImage(20)))
	for n := 0; n <= len(complet); n++ {
		_, _ = lireTags(".m4a", complet[:n])
	}
}
