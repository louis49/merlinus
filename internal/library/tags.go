package library

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// lireTags extrait au mieux le titre et l'image de couverture embarques dans un
// fichier audio. mp3 : ID3v2 (cadres TIT2, APIC). m4a/mp4 : atomes (moov/udta/
// meta/ilst : (c)nam, covr). Best-effort et sans dependance : renvoie des
// valeurs vides si le tag est absent, tronque ou d'un format non gere.
func lireTags(ext string, data []byte) (titre string, cover []byte) {
	switch ext {
	case ".mp3":
		return tagsID3(data)
	case ".m4a", ".mp4", ".m4b", ".m4v":
		return tagsMP4(data)
	}
	return "", nil
}

// ---- ID3v2 (mp3) ----

// tailleSynchsafe decode un entier 28 bits reparti sur 4 octets (7 bits utiles
// chacun), format des tailles d'en-tete ID3v2 et des cadres v2.4.
func tailleSynchsafe(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

func tagsID3(data []byte) (string, []byte) {
	if len(data) < 10 || !bytes.Equal(data[0:3], []byte("ID3")) {
		return "", nil
	}
	majeur := data[3] // 2.majeur.x (3 ou 4 en pratique)
	// v2.2 a un format de cadre different (en-tete 6 octets, ID 3 octets) : on
	// ne le lit pas en v2.3/2.4 sous peine de titre garbage. De meme, en-tete
	// etendu (0x40) et unsynchronisation (0x80, decalage de toutes les tailles)
	// ne sont pas geres. Best-effort honnete : mieux vaut rien qu'un tag faux.
	if majeur < 3 || data[5]&0xC0 != 0 {
		return "", nil
	}
	tailleTag := tailleSynchsafe(data[6:10])
	fin := 10 + tailleTag
	if fin > len(data) {
		fin = len(data)
	}
	var titre string
	var cover []byte
	p := 10
	for p+10 <= fin {
		id := string(data[p : p+4])
		if id == "\x00\x00\x00\x00" { // zone de bourrage : fin des cadres
			break
		}
		var taille int
		if majeur >= 4 {
			taille = tailleSynchsafe(data[p+4 : p+8]) // v2.4 : synchsafe
		} else {
			taille = int(binary.BigEndian.Uint32(data[p+4 : p+8])) // v2.3 : entier plein
		}
		corpsDeb := p + 10
		corpsFin := corpsDeb + taille
		if taille <= 0 || corpsFin > fin {
			break
		}
		corps := data[corpsDeb:corpsFin]
		switch id {
		case "TIT2":
			if titre == "" {
				titre = decoderTexteID3(corps)
			}
		case "APIC":
			if cover == nil {
				cover = imageAPIC(corps)
			}
		}
		p = corpsFin
	}
	return titre, cover
}

// decoderTexteID3 decode un cadre texte : 1er octet = encodage, puis la chaine.
func decoderTexteID3(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	enc := b[0]
	payload := b[1:]
	switch enc {
	case 0: // ISO-8859-1
		return string(bytes.TrimRight(latin1(payload), "\x00"))
	case 3: // UTF-8
		return string(bytes.TrimRight(payload, "\x00"))
	case 1, 2: // UTF-16 (avec BOM) / UTF-16BE
		return decoderUTF16(payload, enc == 1)
	}
	return ""
}

func latin1(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		out = append(out, []byte(string(rune(c)))...)
	}
	return out
}

func decoderUTF16(b []byte, avecBOM bool) string {
	if len(b) < 2 {
		return ""
	}
	be := false
	if avecBOM {
		if b[0] == 0xFE && b[1] == 0xFF {
			be = true
		}
		b = b[2:]
	} else {
		be = true // UTF-16BE explicite
	}
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := 0; i < len(u); i++ {
		if be {
			u[i] = binary.BigEndian.Uint16(b[2*i:])
		} else {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
	}
	s := string(utf16.Decode(u))
	return string(bytes.TrimRight([]byte(s), "\x00"))
}

// imageAPIC extrait les octets image d'un cadre APIC :
// [encodage:1][mime\0][type:1][description(term. selon encodage)][image...].
func imageAPIC(b []byte) []byte {
	if len(b) < 4 {
		return nil
	}
	enc := b[0]
	i := 1
	// mime : chaine ISO-8859-1 terminee par \0
	j := bytes.IndexByte(b[i:], 0)
	if j < 0 {
		return nil
	}
	i += j + 1
	if i >= len(b) {
		return nil
	}
	i++ // type d'image (1 octet)
	// description : terminaison \0 (1 octet) ou \0\0 (UTF-16)
	if enc == 1 || enc == 2 {
		for i+1 < len(b) {
			if b[i] == 0 && b[i+1] == 0 {
				i += 2
				break
			}
			i += 2
		}
	} else {
		k := bytes.IndexByte(b[i:], 0)
		if k < 0 {
			return nil
		}
		i += k + 1
	}
	if i >= len(b) {
		return nil
	}
	return b[i:]
}

// ---- MP4 / m4a (atomes) ----

// tagsMP4 descend moov/udta/meta/ilst et lit (c)nam (titre) et covr (image).
func tagsMP4(data []byte) (string, []byte) {
	moov := sousAtome(data, "moov")
	if moov == nil {
		return "", nil
	}
	udta := sousAtome(moov, "udta")
	if udta == nil {
		return "", nil
	}
	meta := sousAtome(udta, "meta")
	if meta == nil {
		return "", nil
	}
	// meta porte 4 octets version/flags avant ses enfants.
	if len(meta) < 4 {
		return "", nil
	}
	ilst := sousAtome(meta[4:], "ilst")
	if ilst == nil {
		return "", nil
	}
	titre := ""
	if nam := sousAtome(ilst, "\xa9nam"); nam != nil {
		if d := donneesAtome(nam); d != nil {
			titre = string(bytes.TrimRight(d, "\x00"))
		}
	}
	var cover []byte
	if covr := sousAtome(ilst, "covr"); covr != nil {
		cover = donneesAtome(covr)
	}
	return titre, cover
}

// sousAtome renvoie le CONTENU (hors en-tete 8 octets) du premier atome de type
// donne trouve directement dans buf. Ne descend pas recursivement.
func sousAtome(buf []byte, typ string) []byte {
	p := 0
	for p+8 <= len(buf) {
		taille := int(binary.BigEndian.Uint32(buf[p : p+4]))
		nom := string(buf[p+4 : p+8])
		if taille < 8 || p+taille > len(buf) {
			// taille 0 = jusqu'a la fin ; sinon en-tete corrompu -> stop
			if taille == 0 {
				taille = len(buf) - p
			} else {
				return nil
			}
		}
		if nom == typ {
			return buf[p+8 : p+taille]
		}
		p += taille
	}
	return nil
}

// donneesAtome lit le sous-atome "data" d'une entree ilst et renvoie sa charge
// utile (apres 8 octets d'en-tete data + 4 version/flags + 4 reserve).
func donneesAtome(entry []byte) []byte {
	d := sousAtome(entry, "data")
	if d == nil || len(d) < 8 {
		return nil
	}
	return d[8:]
}
