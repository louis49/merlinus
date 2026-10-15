package library

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// trameMP3 : trame MPEG1 couche III 128 kb/s 44,1 kHz stereo (417 octets).
func trameMP3() []byte {
	t := make([]byte, 417)
	copy(t, []byte{0xFF, 0xFB, 0x90, 0x00})
	return t
}

func TestDureeMP3CBR(t *testing.T) {
	// 1000 trames x 1152 echantillons / 44100 Hz = 26,1 s ; precedees d'un ID3v2.
	id3 := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 20}
	id3 = append(id3, make([]byte, 20)...)
	data := append(id3, bytes.Repeat(trameMP3(), 1000)...)
	if d := dureeMP3(data, int64(len(data))); d != 26 {
		t.Fatalf("duree CBR = %d s, attendu 26", d)
	}
}

func TestDureeMP3Xing(t *testing.T) {
	// En-tete Xing (VBR) annoncant 2 297 trames = 60 s, fichier volontairement court.
	tr := trameMP3()
	x := 4 + 32
	copy(tr[x:], "Xing")
	binary.BigEndian.PutUint32(tr[x+4:], 1) // drapeau "nombre de trames"
	binary.BigEndian.PutUint32(tr[x+8:], 2297)
	if d := dureeMP3(tr, int64(len(tr))); d != 60 {
		t.Fatalf("duree Xing = %d s, attendu 60", d)
	}
}

func TestDureeMP3Illisible(t *testing.T) {
	if d := dureeMP3([]byte("pas un mp3"), 10); d != 0 {
		t.Fatalf("duree = %d, attendu 0", d)
	}
}
