package device

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/protocol"
)

func TestChiffrerHeure(t *testing.T) {
	mac := []byte{0x38, 0x18, 0x2b, 0x93, 0xef, 0x6d}
	const mural = 1793790000 // 2026-11-03 ~ 09:40 "murale"
	iv := bytes.Repeat([]byte{7}, 12)
	blob, err := chiffrerHeure(mac, mural, bytes.NewReader(iv))
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) != 32 {
		t.Fatalf("taille = %d, attendu 32 (la boite rejette toute autre longueur)", len(blob))
	}
	if !bytes.Equal(blob[:12], iv) {
		t.Errorf("IV non place en tete : % x", blob[:12])
	}
	clair, err := DechiffrerPlaylist(mac, blob)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(clair); len(clair) != 4 || got != mural {
		t.Errorf("clair = % x (%d), attendu u32 LE %d", clair, got, mural)
	}
}

func TestChiffrerHeureHorsPlage(t *testing.T) {
	mac := []byte{1, 2, 3, 4, 5, 6}
	for _, v := range []int64{-1, 1 << 32} {
		if _, err := chiffrerHeure(mac, v, nil); err == nil {
			t.Errorf("heure %d acceptee (hors u32)", v)
		}
	}
}

func TestLireHeure(t *testing.T) {
	addr := fauxServeur(t, func(conn net.Conn) {
		op, p, err := lireTrame(conn)
		if err != nil || op != protocol.OpGetTime || len(p) != 0 {
			t.Errorf("requete op27 inattendue : op=%d p=% x err=%v", op, p, err)
			return
		}
		frame, _ := protocol.BuildFrame(protocol.OpGetTime, binary.LittleEndian.AppendUint32(nil, 1793790000))
		conn.Write(frame)
	})
	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.LireHeure()
	if err != nil {
		t.Fatal(err)
	}
	if got != 1793790000 {
		t.Errorf("heure = %d, attendu 1793790000", got)
	}
}
