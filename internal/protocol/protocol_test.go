package protocol

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// Vecteur de controle standard du catalogue CRC (CRC-32/MPEG-2, "check").
func TestCRC32MPEG2VecteurReference(t *testing.T) {
	if got := CRC32MPEG2([]byte("123456789")); got != 0x0376E6E7 {
		t.Fatalf("CRC32MPEG2(\"123456789\") = %08x, attendu 0376e6e7", got)
	}
}

// Sans XorOut, le CRC d'une entree vide est la valeur initiale.
func TestCRC32MPEG2Vide(t *testing.T) {
	if got := CRC32MPEG2(nil); got != 0xFFFFFFFF {
		t.Fatalf("CRC32MPEG2(vide) = %08x, attendu ffffffff", got)
	}
}

// Propriete du CRC sans XorOut : ajouter le CRC en big-endian annule le reste.
func TestCRC32MPEG2ResteNul(t *testing.T) {
	data := []byte{0x02, 0x10, 0x20, 0x30}
	c := CRC32MPEG2(data)
	avec := binary.BigEndian.AppendUint32(append([]byte{}, data...), c)
	if got := CRC32MPEG2(avec); got != 0 {
		t.Fatalf("CRC(data||crc BE) = %08x, attendu 0", got)
	}
}

func TestBuildFrameStructure(t *testing.T) {
	payload := []byte{0xAA, 0xBB, 0xCC}
	f, err := BuildFrame(OpGetFile, payload)
	if err != nil {
		t.Fatalf("BuildFrame : %v", err)
	}
	if len(f) != 1+1+len(payload)+4 {
		t.Fatalf("taille de trame %d, attendu %d", len(f), 1+1+len(payload)+4)
	}
	if int(f[0]) != len(payload)+5 {
		t.Fatalf("octet de longueur %d, attendu %d", f[0], len(payload)+5)
	}
	if f[1] != OpGetFile {
		t.Fatalf("opcode %d, attendu %d", f[1], OpGetFile)
	}
	if !bytes.Equal(f[2:2+len(payload)], payload) {
		t.Fatalf("payload %x, attendu %x", f[2:2+len(payload)], payload)
	}
	crc := CRC32MPEG2(append([]byte{OpGetFile}, payload...))
	if got := binary.LittleEndian.Uint32(f[len(f)-4:]); got != crc {
		t.Fatalf("CRC LE %08x, attendu %08x", got, crc)
	}
}

// Trame ping attendue, octet par octet (opcode seul, payload vide).
func TestBuildFramePingOctets(t *testing.T) {
	f, err := BuildFrame(OpPing, nil)
	if err != nil {
		t.Fatalf("BuildFrame : %v", err)
	}
	crc := CRC32MPEG2([]byte{OpPing})
	attendu := []byte{5, OpPing, byte(crc), byte(crc >> 8), byte(crc >> 16), byte(crc >> 24)}
	if !bytes.Equal(f, attendu) {
		t.Fatalf("trame ping %x, attendu %x", f, attendu)
	}
}

func TestBuildFrameBornePayload(t *testing.T) {
	f, err := BuildFrame(OpUploadFile, make([]byte, MaxPayload))
	if err != nil {
		t.Fatalf("payload de %d octets refuse : %v", MaxPayload, err)
	}
	if f[0] != 255 || len(f) != 256 {
		t.Fatalf("trame maximale : len=%d taille=%d, attendu 255 et 256", f[0], len(f))
	}
	if _, err := BuildFrame(OpUploadFile, make([]byte, MaxPayload+1)); err == nil {
		t.Fatalf("payload de %d octets accepte", MaxPayload+1)
	}
}

func TestBuildFrameOpcodeHorsPlage(t *testing.T) {
	for _, op := range []byte{0, 34, OpErreur} {
		if _, err := BuildFrame(op, nil); err == nil {
			t.Errorf("opcode %d accepte", op)
		}
	}
	for _, op := range []byte{1, 33} {
		if _, err := BuildFrame(op, nil); err != nil {
			t.Errorf("opcode %d refuse : %v", op, err)
		}
	}
}

func TestParseFrameAllerRetour(t *testing.T) {
	cas := [][]byte{nil, {0x00}, []byte("playlist.bin"), bytes.Repeat([]byte{0x5A}, MaxPayload)}
	for _, p := range cas {
		f, err := BuildFrame(OpUpdatePlaylist, p)
		if err != nil {
			t.Fatalf("BuildFrame : %v", err)
		}
		op, payload, err := ParseFrame(f)
		if err != nil {
			t.Fatalf("ParseFrame (payload %d octets) : %v", len(p), err)
		}
		if op != OpUpdatePlaylist || !bytes.Equal(payload, p) {
			t.Fatalf("aller-retour : op=%d payload=%x, attendu op=%d payload=%x", op, payload, OpUpdatePlaylist, p)
		}
	}
}

// ParseFrame n'impose pas la plage d'opcodes : la boite repond 0xFF en erreur.
func TestParseFrameOpErreur(t *testing.T) {
	body := []byte{OpErreur}
	f := append([]byte{5}, body...)
	f = binary.LittleEndian.AppendUint32(f, CRC32MPEG2(body))
	op, payload, err := ParseFrame(f)
	if err != nil || op != OpErreur || len(payload) != 0 {
		t.Fatalf("trame d'erreur : op=%d payload=%x err=%v", op, payload, err)
	}
}

func TestParseFrameCRCFaux(t *testing.T) {
	f, _ := BuildFrame(OpPing, []byte{1, 2, 3})
	for _, i := range []int{1, 2, len(f) - 1} { // opcode, payload, CRC
		g := append([]byte{}, f...)
		g[i] ^= 0x01
		if _, _, err := ParseFrame(g); err == nil || !strings.Contains(err.Error(), "CRC") {
			t.Errorf("octet %d altere : erreur CRC attendue, obtenu %v", i, err)
		}
	}
}

func TestParseFrameTropCourte(t *testing.T) {
	for _, f := range [][]byte{nil, {}, {5}, {5, 2, 0, 0, 0}} {
		if _, _, err := ParseFrame(f); err == nil {
			t.Errorf("trame de %d octets acceptee", len(f))
		}
	}
}

func TestParseFrameLongueurIncoherente(t *testing.T) {
	f, _ := BuildFrame(OpPing, []byte{1, 2, 3})
	// Octet de longueur trop grand puis trop petit.
	for _, l := range []byte{f[0] + 1, f[0] - 1} {
		g := append([]byte{}, f...)
		g[0] = l
		if _, _, err := ParseFrame(g); err == nil {
			t.Errorf("longueur annoncee %d (reelle %d) acceptee", l, len(f)-1)
		}
	}
	// Octet de trop en fin de trame.
	if _, _, err := ParseFrame(append(append([]byte{}, f...), 0)); err == nil {
		t.Error("trame avec octet surnumeraire acceptee")
	}
	// Longueur aberrante (< 5) sur une trame de taille suffisante.
	g := append([]byte{}, f...)
	g[0] = 4
	if _, _, err := ParseFrame(g); err == nil {
		t.Error("octet de longueur 4 accepte")
	}
}
