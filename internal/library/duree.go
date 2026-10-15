package library

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"

	"github.com/louis49/merlinus/internal/store"
)

// DureeAudio renvoie la duree (secondes) du MP3 local d'un contenu, 0 si
// inconnue (absent, autre format, en-tete illisible). Lit seulement le debut du
// fichier : en-tete Xing/Info ou VBRI (nombre de trames), sinon estimation CBR
// a partir du debit de la 1re trame et de la taille.
func DureeAudio(uuid string) int {
	if ExtensionAudio(uuid) != "mp3" {
		return 0
	}
	return DureeMP3(CheminAudio(uuid, "mp3"))
}

// DureeMP3 : duree (secondes) d'un fichier MP3 du dossier de donnees (chemin
// relatif au store), 0 si inconnue. Sert aussi aux copies de la sauvegarde.
func DureeMP3(rel string) int {
	dir, err := store.Dir()
	if err != nil {
		return 0
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	// Etiquette ID3v2 sautee par positionnement : elle peut embarquer une
	// pochette de plusieurs centaines de Ko, bien au-dela du tampon de lecture.
	taille := st.Size()
	tete := make([]byte, 10)
	if n, _ := io.ReadFull(f, tete); n == 10 && string(tete[:3]) == "ID3" {
		fin := int64(10 + tailleSynchsafe(tete[6:10]))
		if tete[5]&0x10 != 0 {
			fin += 10 // pied de page ID3v2.4
		}
		if _, err := f.Seek(fin, io.SeekStart); err != nil {
			return 0
		}
		taille -= fin
	} else if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	debut := make([]byte, 64*1024)
	n, _ := io.ReadFull(f, debut)
	return dureeMP3(debut[:n], taille)
}

// Tables MPEG audio (couche III).
var (
	debitsV1L3 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	debitsV2L3 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	frequences = [3][3]int{{44100, 48000, 32000}, {22050, 24000, 16000}, {11025, 12000, 8000}} // V1, V2, V2.5
)

// dureeMP3 : debut = premiers octets du fichier, taille = taille totale.
func dureeMP3(b []byte, taille int64) int {
	off := 0
	// Etiquette ID3v2 en tete : sautee (taille synchsafe).
	if len(b) >= 10 && string(b[:3]) == "ID3" {
		off = 10 + tailleSynchsafe(b[6:10])
		if b[5]&0x10 != 0 {
			off += 10 // pied de page ID3v2.4
		}
	}
	// 1re trame valide (synchro 11 bits, couche III).
	for ; off+4 <= len(b); off++ {
		if b[off] != 0xFF || b[off+1]&0xE0 != 0xE0 {
			continue
		}
		version := (b[off+1] >> 3) & 3 // 3 = V1, 2 = V2, 0 = V2.5
		couche := (b[off+1] >> 1) & 3  // 1 = couche III
		iDebit := b[off+2] >> 4
		iFreq := (b[off+2] >> 2) & 3
		if version == 1 || couche != 1 || iDebit == 0 || iDebit == 15 || iFreq == 3 {
			continue
		}
		v := map[byte]int{3: 0, 2: 1, 0: 2}[version]
		freq := frequences[v][iFreq]
		debit := debitsV1L3[iDebit]
		echParTrame := 1152
		if v > 0 {
			debit = debitsV2L3[iDebit]
			echParTrame = 576
		}
		mono := b[off+3]>>6 == 3
		// Position de l'en-tete Xing/Info apres les "side info".
		side := 32
		switch {
		case v == 0 && mono:
			side = 17
		case v > 0 && !mono:
			side = 17
		case v > 0 && mono:
			side = 9
		}
		if x := off + 4 + side; x+16 <= len(b) {
			tag := string(b[x : x+4])
			if (tag == "Xing" || tag == "Info") && b[x+7]&1 != 0 {
				if trames := binary.BigEndian.Uint32(b[x+8 : x+12]); trames > 0 {
					return int(int64(trames) * int64(echParTrame) / int64(freq))
				}
			}
		}
		if x := off + 4 + 32; x+18 <= len(b) && string(b[x:x+4]) == "VBRI" {
			if trames := binary.BigEndian.Uint32(b[x+14 : x+18]); trames > 0 {
				return int(int64(trames) * int64(echParTrame) / int64(freq))
			}
		}
		// CBR : (taille audio en bits) / debit.
		return int((taille - int64(off)) * 8 / int64(debit*1000))
	}
	return 0
}
