package library

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// ---- fabrique de fixtures MP4 (boites ecrites a la main, en memoire) ----

// boite construit une boite ISO-BMFF : [taille:4 BE][type:4][charge...].
func boite(typ string, parties ...[]byte) []byte {
	var charge []byte
	for _, p := range parties {
		charge = append(charge, p...)
	}
	out := make([]byte, 8, 8+len(charge))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(charge)))
	copy(out[4:8], typ)
	return append(out, charge...)
}

func u16(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }
func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

// versionFlags : en-tete des FullBox (version 0, flags 0).
func versionFlags() []byte { return []byte{0, 0, 0, 0} }

// asc encode une AudioSpecificConfig minimale (2 octets).
func asc(aot, freqIdx, chan_ byte) []byte {
	return []byte{aot<<3 | freqIdx>>1, (freqIdx&1)<<7 | chan_<<3}
}

// esdsCharge construit le contenu d'une esds (hors en-tete de boite) :
// ES_Descriptor(0x03) > DecoderConfigDescriptor(0x04) > DecSpecificInfo(0x05),
// puis SLConfig(0x06). Longueurs sur un octet (forme courte).
func esdsCharge(config []byte) []byte {
	dsi := append([]byte{0x05, byte(len(config))}, config...)
	dcd := []byte{0x04, byte(13 + len(dsi)), 0x40, 0x15, 0, 0, 0}
	dcd = append(dcd, u32(128000)...)
	dcd = append(dcd, u32(128000)...)
	dcd = append(dcd, dsi...)
	sl := []byte{0x06, 0x01, 0x02}
	es := []byte{0x03, byte(3 + len(dcd) + len(sl)), 0x00, 0x01, 0x00}
	es = append(es, dcd...)
	es = append(es, sl...)
	return append(versionFlags(), es...)
}

// mp4aEntree : AudioSampleEntry 'mp4a' contenant l'esds.
func mp4aEntree(config []byte) []byte {
	var se []byte
	se = append(se, 0, 0, 0, 0, 0, 0) // reserve
	se = append(se, u16(1)...)        // data_reference_index
	se = append(se, make([]byte, 8)...)
	se = append(se, u16(2)...)  // channelcount
	se = append(se, u16(16)...) // samplesize
	se = append(se, u16(0)...)  // pre_defined
	se = append(se, u16(0)...)  // reserve
	se = append(se, u32(44100<<16)...)
	return boite("mp4a", se, boite("esds", esdsCharge(config)))
}

// stszTailleConstante : si vrai, m4aMinimal ecrit un stsz a taille constante
// (sans table). Pose et remis a faux par le test qui l'utilise.
var stszTailleConstante bool

// m4aMinimal assemble ftyp + moov + mdat. echantillons est decoupe en chunks
// selon parChunk (ex. [2,1] : 2 echantillons dans le 1er chunk, 1 dans le 2e).
// decalage permet de fausser les offsets stco (test hors limites).
func m4aMinimal(config []byte, echantillons [][]byte, parChunk []int, decalage uint32) []byte {
	ftyp := boite("ftyp", []byte("M4A "), u32(0), []byte("M4A isom"))

	construireMoov := func(offsets []uint32) []byte {
		tkhd := append(versionFlags(), make([]byte, 80)...)
		binary.BigEndian.PutUint32(tkhd[12:16], 1) // track_ID
		mdhd := append(versionFlags(), u32(0)...)
		mdhd = append(mdhd, u32(0)...)
		mdhd = append(mdhd, u32(44100)...)
		mdhd = append(mdhd, u32(uint32(1024*len(echantillons)))...)
		mdhd = append(mdhd, u16(0x55c4)...)
		mdhd = append(mdhd, u16(0)...)
		hdlr := append(versionFlags(), u32(0)...)
		hdlr = append(hdlr, []byte("soun")...)
		hdlr = append(hdlr, make([]byte, 12)...)
		hdlr = append(hdlr, 0)

		stsd := boite("stsd", versionFlags(), u32(1), mp4aEntree(config))
		stts := boite("stts", versionFlags(), u32(1), u32(uint32(len(echantillons))), u32(1024))
		stscC := append(versionFlags(), u32(uint32(len(parChunk)))...)
		for i, n := range parChunk {
			stscC = append(stscC, u32(uint32(i+1))...)
			stscC = append(stscC, u32(uint32(n))...)
			stscC = append(stscC, u32(1)...)
		}
		var stszC []byte
		if stszTailleConstante && len(echantillons) > 0 {
			// stsz a taille constante : sample_size != 0, sans table.
			stszC = append(versionFlags(), u32(uint32(len(echantillons[0])))...)
			stszC = append(stszC, u32(uint32(len(echantillons)))...)
		} else {
			stszC = append(versionFlags(), u32(0)...)
			stszC = append(stszC, u32(uint32(len(echantillons)))...)
			for _, e := range echantillons {
				stszC = append(stszC, u32(uint32(len(e)))...)
			}
		}
		stcoC := append(versionFlags(), u32(uint32(len(offsets)))...)
		for _, o := range offsets {
			stcoC = append(stcoC, u32(o)...)
		}
		stbl := boite("stbl", stsd, stts, boite("stsc", stscC), boite("stsz", stszC), boite("stco", stcoC))
		minf := boite("minf", stbl)
		mdia := boite("mdia", boite("mdhd", mdhd), boite("hdlr", hdlr), minf)
		trak := boite("trak", boite("tkhd", tkhd), mdia)
		return boite("moov", trak)
	}

	// La taille du moov ne depend pas des valeurs d'offset : 1re passe a blanc.
	tailleMoov := len(construireMoov(make([]uint32, len(parChunk))))
	base := uint32(len(ftyp)+tailleMoov+8) + decalage
	offsets := make([]uint32, len(parChunk))
	si, cur := 0, base
	for c, n := range parChunk {
		offsets[c] = cur
		for k := 0; k < n; k++ {
			cur += uint32(len(echantillons[si]))
			si++
		}
	}
	var mdat []byte
	for _, e := range echantillons {
		mdat = append(mdat, e...)
	}
	out := append([]byte{}, ftyp...)
	out = append(out, construireMoov(offsets)...)
	return append(out, boite("mdat", mdat)...)
}

// trameADTS est une trame decodee independamment du code teste.
type trameADTS struct {
	profil, freqIdx, chanCfg byte
	charge                   []byte
}

// decouperADTS relit un flux ADTS (sans CRC) en verifiant chaque en-tete.
func decouperADTS(t *testing.T, flux []byte) []trameADTS {
	t.Helper()
	var out []trameADTS
	for p := 0; p < len(flux); {
		if p+7 > len(flux) {
			t.Fatalf("en-tete ADTS tronque a l'offset %d", p)
		}
		h := flux[p : p+7]
		if h[0] != 0xFF || h[1] != 0xF1 {
			t.Fatalf("syncword/protection inattendus a %d : % x", p, h[:2])
		}
		lg := int(h[3]&0x03)<<11 | int(h[4])<<3 | int(h[5]>>5)
		if lg < 7 || p+lg > len(flux) {
			t.Fatalf("frame_length %d invalide a %d", lg, p)
		}
		if h[5]&0x1F != 0x1F || h[6] != 0xFC {
			t.Fatalf("buffer fullness / nb de blocs inattendus : % x", h[5:7])
		}
		out = append(out, trameADTS{
			profil:  h[2] >> 6,
			freqIdx: (h[2] >> 2) & 0x0F,
			chanCfg: (h[2]&0x01)<<2 | h[3]>>6,
			charge:  flux[p+7 : p+lg],
		})
		p += lg
	}
	return out
}

func echantillonsTest() [][]byte {
	return [][]byte{
		bytes.Repeat([]byte{0x11}, 10),
		bytes.Repeat([]byte{0x22}, 300), // > 255 : exerce les bits hauts de frame_length
		bytes.Repeat([]byte{0x33}, 5),
	}
}

// Cas nominal : AAC-LC 44,1 kHz stereo, 3 echantillons repartis sur 2 chunks.
// Chaque echantillon doit devenir exactement une trame ADTS, dans l'ordre.
func TestRemuxerAACenADTSNominal(t *testing.T) {
	ech := echantillonsTest()
	m4a := m4aMinimal(asc(2, 4, 2), ech, []int{2, 1}, 0)
	out, err := RemuxerAACenADTS(m4a)
	if err != nil {
		t.Fatalf("remux : %v", err)
	}
	trames := decouperADTS(t, out)
	if len(trames) != len(ech) {
		t.Fatalf("%d trames, attendu %d", len(trames), len(ech))
	}
	for i, tr := range trames {
		if tr.profil != 1 { // profil ADTS = AOT - 1 = 1 (LC)
			t.Errorf("trame %d : profil %d, attendu 1", i, tr.profil)
		}
		if tr.freqIdx != 4 || tr.chanCfg != 2 {
			t.Errorf("trame %d : freqIdx=%d chan=%d, attendu 4/2", i, tr.freqIdx, tr.chanCfg)
		}
		if !bytes.Equal(tr.charge, ech[i]) {
			t.Errorf("trame %d : charge differente de l'echantillon source", i)
		}
	}
	if len(out) != 3*7+10+300+5 {
		t.Errorf("taille du flux %d inattendue", len(out))
	}
}

// Configuration a 7 canaux (bit haut de channel_configuration porte par l'octet 2)
// et frequence 48 kHz (indice 3).
func TestRemuxerAACenADTSCanauxEtFrequence(t *testing.T) {
	ech := [][]byte{{1, 2, 3}}
	out, err := RemuxerAACenADTS(m4aMinimal(asc(2, 3, 7), ech, []int{1}, 0))
	if err != nil {
		t.Fatalf("remux : %v", err)
	}
	tr := decouperADTS(t, out)
	if len(tr) != 1 || tr[0].freqIdx != 3 || tr[0].chanCfg != 7 {
		t.Fatalf("trames = %+v, attendu freqIdx=3 chan=7", tr)
	}
}

func TestRemuxerAACenADTSRefus(t *testing.T) {
	ech := [][]byte{{1, 2, 3}}
	cas := []struct {
		nom     string
		m4a     []byte
		extrait string
	}{
		{"HE-AAC (AOT 5)", m4aMinimal(asc(5, 4, 2), ech, []int{1}, 0), "non-LC"},
		{"canal 0", m4aMinimal(asc(2, 4, 0), ech, []int{1}, 0), "hors normes"},
		{"frequence reservee (13)", m4aMinimal(asc(2, 13, 2), ech, []int{1}, 0), "hors normes"},
		{"offset hors fichier", m4aMinimal(asc(2, 4, 2), ech, []int{1}, 1000), "hors limites"},
		{"echantillon trop grand", m4aMinimal(asc(2, 4, 2), [][]byte{make([]byte, 0x1FFF-7+1)}, []int{1}, 0), "trop grand"},
		{"pas un mp4", []byte("ceci n'est pas un conteneur"), ""},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			out, err := RemuxerAACenADTS(c.m4a)
			if err == nil {
				t.Fatalf("erreur attendue, obtenu %d octets", len(out))
			}
			if c.extrait != "" && !strings.Contains(err.Error(), c.extrait) {
				t.Errorf("erreur %q, attendu un message contenant %q", err, c.extrait)
			}
		})
	}
}

// Taille maximale admise par ADTS : 0x1FFF - 7 octets de charge.
func TestRemuxerAACenADTSTailleLimite(t *testing.T) {
	ech := [][]byte{bytes.Repeat([]byte{0xAB}, 0x1FFF-7)}
	out, err := RemuxerAACenADTS(m4aMinimal(asc(2, 4, 2), ech, []int{1}, 0))
	if err != nil {
		t.Fatalf("remux : %v", err)
	}
	if tr := decouperADTS(t, out); len(tr) != 1 || len(tr[0].charge) != 0x1FFF-7 {
		t.Fatalf("trame limite mal ecrite")
	}
}

// Un conteneur sans piste audio (stsd sans mp4a) est refuse.
func TestRemuxerAACenADTSSansPisteAudio(t *testing.T) {
	m4a := m4aMinimal(asc(2, 4, 2), [][]byte{{1}}, []int{1}, 0)
	m4a = bytes.Replace(m4a, []byte("mp4a"), []byte("xxxx"), 1)
	if _, err := RemuxerAACenADTS(m4a); err == nil || !strings.Contains(err.Error(), "aucune piste AAC") {
		t.Fatalf("erreur attendue 'aucune piste AAC', obtenu %v", err)
	}
}

func TestLireASC(t *testing.T) {
	// Nominal : l'ASC est extraite telle quelle.
	config := []byte{0x12, 0x10}
	b := append([]byte("prefixe"), boite("esds", esdsCharge(config))...)
	got, err := lireASC(b)
	if err != nil || !bytes.Equal(got, config) {
		t.Fatalf("lireASC = % x, %v ; attendu % x", got, err, config)
	}

	// Longueurs de descripteurs en forme longue (0x80 0x80 0x80 n), telle
	// qu'ecrite par de nombreux encodeurs.
	long := func(n int) []byte { return []byte{0x80, 0x80, 0x80, byte(n)} }
	dsi := append(append([]byte{0x05}, long(2)...), config...)
	dcd := append(append([]byte{0x04}, long(13+len(dsi))...), 0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	dcd = append(dcd, dsi...)
	es := append(append([]byte{0x03}, long(3+len(dcd))...), 0, 1, 0)
	es = append(es, dcd...)
	got, err = lireASC(boite("esds", versionFlags(), es))
	if err != nil || !bytes.Equal(got, config) {
		t.Fatalf("forme longue : % x, %v", got, err)
	}

	// Erreurs : jamais de panique sur une entree tronquee ou incoherente.
	complet := boite("esds", esdsCharge(config))
	for _, c := range []struct {
		nom     string
		b       []byte
		extrait string
	}{
		{"sans esds", []byte("rien ici"), "esds absente"},
		{"esds vide", []byte("esds"), "ES_Descriptor absent"},
		{"mauvais tag ES", append([]byte("esds\x00\x00\x00\x00"), 0x07), "ES_Descriptor absent"},
		{"sans DecoderConfig", append([]byte("esds\x00\x00\x00\x00"), 0x03, 0x05, 0, 1, 0, 0x09), "DecoderConfigDescriptor absent"},
		{"ASC tronquee", complet[:len(complet)-4], "ASC tronquee"},
	} {
		if _, err := lireASC(c.b); err == nil || !strings.Contains(err.Error(), c.extrait) {
			t.Errorf("%s : erreur %v, attendu %q", c.nom, err, c.extrait)
		}
	}
	// Toutes les troncatures possibles : erreur ou resultat, jamais de panique.
	for n := 0; n <= len(complet); n++ {
		_, _ = lireASC(complet[:n])
	}
}

// B21 : stsz a taille constante -> erreur explicite, jamais un audio muet.
func TestRemuxerAACenADTSStszTailleConstante(t *testing.T) {
	stszTailleConstante = true
	defer func() { stszTailleConstante = false }()
	ech := [][]byte{bytes.Repeat([]byte{0x21}, 50), bytes.Repeat([]byte{0x22}, 50)}
	out, err := RemuxerAACenADTS(m4aMinimal(asc(2, 4, 2), ech, []int{2}, 0))
	if err == nil {
		t.Fatalf("attendu une erreur, obtenu %d octets (audio muet ?)", len(out))
	}
}

// B21 : champs optionnels de l'ES_Descriptor (dependsOn, URL, OCR) sautes.
func TestLireASCFlagsESDescriptor(t *testing.T) {
	config := []byte{0x12, 0x10}
	dsi := append([]byte{0x05, byte(len(config))}, config...)
	dcd := append([]byte{0x04, byte(13 + len(dsi)), 0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, dsi...)
	for _, c := range []struct {
		nom       string
		flags     byte
		optionnel []byte
	}{
		{"aucun", 0x00, nil},
		{"streamDependence", 0x80, []byte{0x00, 0x02}},
		{"URL", 0x40, append([]byte{3}, "a/b"...)},
		{"OCR", 0x20, []byte{0x00, 0x03}},
		{"les trois", 0xE0, append(append([]byte{0x00, 0x02, 2}, "ab"...), 0x00, 0x03)},
	} {
		corps := append([]byte{0x00, 0x01, c.flags}, c.optionnel...)
		corps = append(corps, dcd...)
		es := append([]byte{0x03, byte(len(corps))}, corps...)
		got, err := lireASC(boite("esds", versionFlags(), es))
		if err != nil || !bytes.Equal(got, config) {
			t.Errorf("%s : % x, %v", c.nom, got, err)
		}
	}
}

// DT9 : l'esds est lu dans la piste audio, pas par recherche brute ; un "esds"
// rencontre plus tot dans le fichier (autre boite, autre piste) est ignore.
func TestEsdsPisteAudioIgnoreLeurre(t *testing.T) {
	config := asc(2, 4, 2)
	leurre := boite("free", []byte("esds"), versionFlags(), []byte{0x07, 0x00})
	m := append(leurre, m4aMinimal(config, echantillonsTest(), []int{2, 1}, 0)...)
	if _, err := lireASC(m); err == nil {
		t.Fatal("le leurre devrait tromper une recherche brute (cas de test invalide)")
	}
	esds, err := esdsPisteAudio(m)
	if err != nil {
		t.Fatalf("esdsPisteAudio : %v", err)
	}
	got, err := lireASC(esds)
	if err != nil || !bytes.Equal(got, config) {
		t.Fatalf("ASC = % x, %v ; attendu % x", got, err, config)
	}
}
