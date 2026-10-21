package device

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/protocol"
)

// fauxServeur simule l'enceinte pour un seul echange, sur une adresse locale.
// handler recoit la connexion apres l'accept ; il lit la requete et repond.
func fauxServeur(t *testing.T, handler func(conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen : %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		defer ln.Close()
		handler(conn)
	}()
	return ln.Addr().String()
}

// lireTrame lit une trame complete cote serveur et renvoie op + payload.
func lireTrame(conn net.Conn) (byte, []byte, error) {
	head := make([]byte, 1)
	if _, err := io.ReadFull(conn, head); err != nil {
		return 0, nil, err
	}
	rest := make([]byte, int(head[0]))
	if _, err := io.ReadFull(conn, rest); err != nil {
		return 0, nil, err
	}
	return protocol.ParseFrame(append(head, rest...))
}

func TestPing(t *testing.T) {
	addr := fauxServeur(t, func(conn net.Conn) {
		op, _, err := lireTrame(conn)
		if err != nil || op != protocol.OpPing {
			t.Errorf("requete ping inattendue : op=%d err=%v", op, err)
			return
		}
		frame, _ := protocol.BuildFrame(protocol.OpPing, nil)
		conn.Write(frame)
	})

	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial : %v", err)
	}
	defer c.Close()
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping : %v", err)
	}
}

func TestGetFile(t *testing.T) {
	contenu := []byte("contenu binaire de playlist.bin \x00\x01\x02\xff")
	addr := fauxServeur(t, func(conn net.Conn) {
		op, payload, err := lireTrame(conn)
		if err != nil || op != protocol.OpGetFile {
			t.Errorf("requete GetFile inattendue : op=%d err=%v", op, err)
			return
		}
		nom := string(payload)
		// En-tete : [status:1][lenChemin:1][chemin][taille:4 LE]
		hdr := []byte{0x00, byte(len(nom))}
		hdr = append(hdr, nom...)
		hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(contenu)))
		frame, _ := protocol.BuildFrame(protocol.OpGetFile, hdr)
		conn.Write(frame)
		// Puis les octets bruts, hors trame.
		conn.Write(contenu)
	})

	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial : %v", err)
	}
	defer c.Close()

	got, err := c.GetFileBytes("playlist.bin")
	if err != nil {
		t.Fatalf("GetFileBytes : %v", err)
	}
	if string(got) != string(contenu) {
		t.Fatalf("contenu different :\n  attendu %q\n  obtenu  %q", contenu, got)
	}
}

func TestGetFileStatutErreur(t *testing.T) {
	addr := fauxServeur(t, func(conn net.Conn) {
		_, payload, _ := lireTrame(conn)
		nom := string(payload)
		hdr := []byte{0x02, byte(len(nom))} // statut != 0 : refus
		hdr = append(hdr, nom...)
		hdr = binary.LittleEndian.AppendUint32(hdr, 0)
		frame, _ := protocol.BuildFrame(protocol.OpGetFile, hdr)
		conn.Write(frame)
	})

	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial : %v", err)
	}
	defer c.Close()
	if _, err := c.GetFileBytes("absent.bin"); err == nil {
		t.Fatal("attendu une erreur pour un statut de refus")
	}
}

func TestDecoderWifiConfig(t *testing.T) {
	w, err := decoderWifiConfig([]byte{1, 3, 2, 'a', 'b', 'c', 'x', 'y'})
	if err != nil || w.SSID != "abc" || w.Password != "xy" || w.Flag != 1 {
		t.Errorf("reponse complete : %+v %v", w, err)
	}
	for _, p := range [][]byte{nil, {1, 3}, {1, 3, 2, 'a', 'b', 'c', 'x'}} {
		if _, err := decoderWifiConfig(p); err == nil {
			t.Errorf("reponse tronquee %v acceptee", p)
		}
	}
}

// Refus reduit au seul octet de statut (get_listening.cmd sans historique,
// constate en reel) : ErrFichierRefuse, et non "en-tete trop courte".
func TestGetFileRefusStatutSeul(t *testing.T) {
	addr := fauxServeur(t, func(conn net.Conn) {
		lireTrame(conn)
		frame, _ := protocol.BuildFrame(protocol.OpGetFile, []byte{0x02})
		conn.Write(frame)
	})
	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial : %v", err)
	}
	defer c.Close()
	_, err = c.GetFileBytes("get_listening.cmd")
	var refus *ErrFichierRefuse
	if !errors.As(err, &refus) || refus.Statut != 2 {
		t.Fatalf("ErrFichierRefuse statut 2 attendu, obtenu %v", err)
	}
}

func TestFirmwareComparer(t *testing.T) {
	ref := Firmware{Major: 2, Minor: 1, Patch: 23}
	cas := []struct {
		f    Firmware
		want int
	}{
		{Firmware{2, 1, 23}, 0},
		{Firmware{2, 1, 16}, -1},
		{Firmware{2, 1, 24}, 1},
		{Firmware{2, 0, 300}, -1}, // la mineure prime sur le correctif
		{Firmware{3, 0, 0}, 1},
		{Firmware{1, 9, 99}, -1},
	}
	for _, c := range cas {
		if got := c.f.Comparer(ref); got != c.want {
			t.Errorf("%s.Comparer(%s) = %d, attendu %d", c.f, ref, got, c.want)
		}
	}
}
