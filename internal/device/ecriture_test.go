package device

// Tests des commandes d'ecriture (op1, op15, op31, clean_sd.cmd) contre un faux
// serveur TCP local. Le faux serveur verifie aussi ce que le client envoie
// (opcode, charge utile, flux brut).

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/protocol"
)

// serveurEcriture enveloppe fauxServeur : renvoie l'adresse et une fonction qui
// attend la fin du handler (evite un t.Errorf apres la fin du test).
func serveurEcriture(t *testing.T, handler func(conn net.Conn)) (string, func()) {
	t.Helper()
	fini := make(chan struct{})
	addr := fauxServeur(t, func(conn net.Conn) {
		defer close(fini)
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		handler(conn)
	})
	attendre := func() {
		t.Helper()
		select {
		case <-fini:
		case <-time.After(5 * time.Second):
			t.Fatal("le faux serveur ne termine pas")
		}
	}
	return addr, attendre
}

// connecter ouvre un client vers le faux serveur avec un delai court.
func connecter(t *testing.T, addr string) *Client {
	t.Helper()
	c, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial : %v", err)
	}
	return c
}

// repondre envoie une trame de reponse cote serveur.
func repondre(t *testing.T, conn net.Conn, op byte, payload []byte) {
	t.Helper()
	if op == protocol.OpErreur {
		// Trame d'erreur 0xFF construite a la main (BuildFrame borne l'opcode).
		body := append([]byte{op}, payload...)
		frame := []byte{byte(len(body) + 4)}
		frame = append(frame, body...)
		frame = binary.LittleEndian.AppendUint32(frame, protocol.CRC32MPEG2(body))
		if _, err := conn.Write(frame); err != nil {
			t.Errorf("ecriture reponse : %v", err)
		}
		return
	}
	frame, err := protocol.BuildFrame(op, payload)
	if err != nil {
		t.Errorf("BuildFrame : %v", err)
		return
	}
	if _, err := conn.Write(frame); err != nil {
		t.Errorf("ecriture reponse : %v", err)
	}
}

// attendreFermeture verifie que le client n'envoie plus rien avant de fermer.
func attendreFermeture(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if n > 0 {
		t.Errorf("octets inattendus recus du client : %x", buf[:n])
		return
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("attendu la fermeture du client, obtenu : %v", err)
	}
}

// enTeteUpload construit l'en-tete op1 attendu : [nameLen][name][size LE][sha256].
func enTeteUpload(nom string, data []byte) []byte {
	sum := sha256.Sum256(data)
	h := []byte{byte(len(nom))}
	h = append(h, nom...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, sum[:]...)
}

// serveurUpload simule un televersement op1 complet : verifie l'en-tete, repond
// statutEnTete, puis (si 0) lit le flux brut, verifie son sha et repond
// opAck/statutAck.
func serveurUpload(t *testing.T, nom string, data []byte, statutEnTete byte, opAck byte, ack []byte) (string, func()) {
	t.Helper()
	return serveurEcriture(t, func(conn net.Conn) {
		op, payload, err := lireTrame(conn)
		if err != nil {
			t.Errorf("lecture en-tete : %v", err)
			return
		}
		if op != protocol.OpUploadFile {
			t.Errorf("opcode en-tete = %d, attendu %d", op, protocol.OpUploadFile)
		}
		if want := enTeteUpload(nom, data); !bytes.Equal(payload, want) {
			t.Errorf("en-tete upload :\n  attendu %x\n  obtenu  %x", want, payload)
		}
		repondre(t, conn, protocol.OpUploadFile, []byte{statutEnTete})
		if statutEnTete != 0 {
			attendreFermeture(t, conn)
			return
		}
		recu := make([]byte, len(data))
		if _, err := io.ReadFull(conn, recu); err != nil {
			t.Errorf("lecture flux brut : %v", err)
			return
		}
		if !bytes.Equal(recu, data) {
			t.Errorf("flux brut different des donnees envoyees")
		}
		repondre(t, conn, opAck, ack)
		attendreFermeture(t, conn)
	})
}

func TestUploadFileDecoupageEtProgression(t *testing.T) {
	// 250 000 octets : 3 blocs (102400, 102400, 45200).
	data := make([]byte, 250000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	nom := "0123abcd-0000-1111-2222-333344445555.mp3"
	addr, attendre := serveurUpload(t, nom, data, 0, protocol.OpUploadFile, []byte{1})

	c := connecter(t, addr)
	var appels [][2]int
	err := c.UploadFile(nom, data, func(sent, total int) {
		appels = append(appels, [2]int{sent, total})
	})
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("UploadFile : %v", err)
	}
	want := [][2]int{{102400, 250000}, {204800, 250000}, {250000, 250000}}
	if len(appels) != len(want) {
		t.Fatalf("progression : %v, attendu %v", appels, want)
	}
	for i := range want {
		if appels[i] != want[i] {
			t.Errorf("progression[%d] = %v, attendu %v", i, appels[i], want[i])
		}
	}
}

func TestUploadFilePetitFichierSansProgression(t *testing.T) {
	data := []byte("petit contenu")
	addr, attendre := serveurUpload(t, "a.jpg", data, 0, protocol.OpUploadFile, []byte{1})
	c := connecter(t, addr)
	err := c.UploadFile("a.jpg", data, nil)
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("UploadFile : %v", err)
	}
}

func TestUploadFileNomMaximal(t *testing.T) {
	nom := strings.Repeat("n", MaxNomFichier)
	data := []byte{1, 2, 3}
	addr, attendre := serveurUpload(t, nom, data, 0, protocol.OpUploadFile, []byte{1})
	c := connecter(t, addr)
	err := c.UploadFile(nom, data, nil)
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("UploadFile nom de %d octets : %v", MaxNomFichier, err)
	}
}

func TestUploadFileEnTeteRefuse(t *testing.T) {
	data := []byte("donnees")
	addr, attendre := serveurUpload(t, "x.mp3", data, 2, 0, nil)
	c := connecter(t, addr)
	progression := false
	err := c.UploadFile("x.mp3", data, func(int, int) { progression = true })
	c.Close()
	attendre()
	if err == nil || !strings.Contains(err.Error(), "NOT_ENOUGH_SPACE") {
		t.Fatalf("attendu un refus NOT_ENOUGH_SPACE, obtenu : %v", err)
	}
	if progression {
		t.Error("progression appelee alors que l'en-tete est refuse")
	}
}

func TestUploadFileAcquittementRefuse(t *testing.T) {
	cas := []struct {
		nom   string
		opAck byte
		ack   []byte
		motif string
	}{
		{"sha invalide", protocol.OpUploadFile, []byte{4}, "SHA256_INVALID"},
		{"echec ecriture", protocol.OpUploadFile, []byte{8}, "FAIL_WRITE"},
		{"statut inconnu", protocol.OpUploadFile, []byte{42}, "42"},
		// 0 (SUCCESS) n'est pas l'acquittement attendu apres le flux (1 = SHA valide).
		{"succes au lieu de sha valide", protocol.OpUploadFile, []byte{0}, "SUCCESS"},
		{"opcode inattendu", protocol.OpPing, []byte{1}, "inattendu"},
		{"acquittement vide", protocol.OpUploadFile, nil, "inattendu"},
		// B16 : rejet CRC nomme explicitement.
		{"rejet CRC", protocol.OpErreur, nil, "CRC"},
	}
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			data := []byte("contenu a televerser")
			addr, attendre := serveurUpload(t, "f.mp3", data, 0, tc.opAck, tc.ack)
			c := connecter(t, addr)
			err := c.UploadFile("f.mp3", data, nil)
			c.Close()
			attendre()
			if err == nil || !strings.Contains(err.Error(), tc.motif) {
				t.Fatalf("attendu une erreur contenant %q, obtenu : %v", tc.motif, err)
			}
		})
	}
}

func TestUploadFileNomsRefusesSansTrafic(t *testing.T) {
	cas := []string{"", strings.Repeat("n", MaxNomFichier+1), "commande.cmd"}
	for _, nom := range cas {
		addr, attendre := serveurEcriture(t, func(conn net.Conn) {
			attendreFermeture(t, conn)
		})
		c := connecter(t, addr)
		err := c.UploadFile(nom, []byte{1}, nil)
		c.Close()
		attendre()
		if err == nil {
			t.Errorf("nom %q accepte", nom)
		}
	}
}

func TestEnvoyerCommandeRAM(t *testing.T) {
	payload := make([]byte, 29)
	for i := range payload {
		payload[i] = byte(i)
	}
	nom := "set_enabling_hours.cmd"
	addr, attendre := serveurUpload(t, nom, payload, 0, protocol.OpUploadFile, []byte{1})
	c := connecter(t, addr)
	err := c.EnvoyerCommandeRAM(nom, payload)
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("EnvoyerCommandeRAM : %v", err)
	}
}

func TestEnvoyerCommandeRAMChargeVide(t *testing.T) {
	nom := "clean_listening.cmd"
	addr, attendre := serveurUpload(t, nom, nil, 0, protocol.OpUploadFile, []byte{1})
	c := connecter(t, addr)
	err := c.EnvoyerCommandeRAM(nom, nil)
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("EnvoyerCommandeRAM charge vide : %v", err)
	}
}

func TestEnvoyerCommandeRAMNomSansCmd(t *testing.T) {
	addr, attendre := serveurEcriture(t, func(conn net.Conn) {
		attendreFermeture(t, conn)
	})
	c := connecter(t, addr)
	err := c.EnvoyerCommandeRAM("pas_une_commande.bin", []byte{1})
	c.Close()
	attendre()
	if err == nil {
		t.Fatal("nom sans .cmd accepte")
	}
}

// enregistrementOp31 : [status=0][nameLen][name][size LE][sha256].
func enregistrementOp31(nom string, taille uint32, sha []byte) []byte {
	r := []byte{0, byte(len(nom))}
	r = append(r, nom...)
	r = binary.LittleEndian.AppendUint32(r, taille)
	return append(r, sha...)
}

func TestIsFileExist(t *testing.T) {
	nom := "abcd.mp3"
	shaFichier := sha256.Sum256([]byte("fichier sur la boite"))
	autreSha := sha256.Sum256([]byte("autre contenu"))

	cas := []struct {
		nom          string
		reponse      []byte
		sha          []byte
		present      bool
		identique    bool
		erreurAttend bool
	}{
		{"absent", []byte{1}, shaFichier[:], false, false, false},
		{"present sha identique", enregistrementOp31(nom, 1234, shaFichier[:]), shaFichier[:], true, true, false},
		{"present sha different", enregistrementOp31(nom, 1234, shaFichier[:]), autreSha[:], true, false, false},
		{"present sans sha demande", enregistrementOp31(nom, 1234, shaFichier[:]), nil, true, false, false},
		{"reponse vide", nil, shaFichier[:], false, false, true},
		{"statut inconnu", []byte{5}, shaFichier[:], false, false, true},
		// B15 : "present" avec enregistrement absent ou tronque = erreur.
		{"present sans enregistrement", []byte{0}, shaFichier[:], false, false, true},
		{"present tronque avant le sha", enregistrementOp31(nom, 1234, shaFichier[:])[:1+1+len(nom)+4+10], shaFichier[:], false, false, true},
		// Nom renvoye different : seulement journalise, resultat inchange.
		{"present nom different", enregistrementOp31("autre.mp3", 1234, shaFichier[:]), shaFichier[:], true, true, false},
	}
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			addr, attendre := serveurEcriture(t, func(conn net.Conn) {
				op, payload, err := lireTrame(conn)
				if err != nil {
					t.Errorf("lecture requete : %v", err)
					return
				}
				if op != protocol.OpIsFileExist {
					t.Errorf("opcode = %d, attendu %d", op, protocol.OpIsFileExist)
				}
				if want := append([]byte{1}, nom...); !bytes.Equal(payload, want) {
					t.Errorf("payload = %x, attendu %x", payload, want)
				}
				repondre(t, conn, protocol.OpIsFileExist, tc.reponse)
			})
			c := connecter(t, addr)
			present, identique, err := c.IsFileExist(nom, tc.sha)
			c.Close()
			attendre()
			if tc.erreurAttend {
				if err == nil {
					t.Fatal("attendu une erreur")
				}
				return
			}
			if err != nil {
				t.Fatalf("IsFileExist : %v", err)
			}
			if present != tc.present || identique != tc.identique {
				t.Errorf("(present, identique) = (%v, %v), attendu (%v, %v)",
					present, identique, tc.present, tc.identique)
			}
		})
	}
}

func TestIsFileExistRejetCRC(t *testing.T) {
	addr, attendre := serveurEcriture(t, func(conn net.Conn) {
		lireTrame(conn)
		// Trame d'erreur 0xFF construite a la main (BuildFrame borne l'opcode a 33).
		body := []byte{protocol.OpErreur}
		frame := []byte{byte(len(body) + 4)}
		frame = append(frame, body...)
		frame = binary.LittleEndian.AppendUint32(frame, protocol.CRC32MPEG2(body))
		conn.Write(frame)
	})
	c := connecter(t, addr)
	_, _, err := c.IsFileExist("a.mp3", nil)
	c.Close()
	attendre()
	if err == nil {
		t.Fatal("attendu une erreur sur rejet CRC")
	}
}

func TestSupprimerFichier(t *testing.T) {
	cas := []struct {
		nom          string
		reponse      []byte
		erreurAttend bool
	}{
		// La boite repond 00 que le fichier existe ou non.
		{"ok", []byte{0}, false},
		{"statut refus", []byte{3}, true},
		{"reponse vide", nil, true},
	}
	nom := "0123abcd.jpg"
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			addr, attendre := serveurEcriture(t, func(conn net.Conn) {
				op, payload, err := lireTrame(conn)
				if err != nil {
					t.Errorf("lecture requete : %v", err)
					return
				}
				if op != protocol.OpDeleteFile {
					t.Errorf("opcode = %d, attendu %d", op, protocol.OpDeleteFile)
				}
				if string(payload) != nom {
					t.Errorf("payload = %q, attendu le nom brut %q", payload, nom)
				}
				repondre(t, conn, protocol.OpDeleteFile, tc.reponse)
			})
			c := connecter(t, addr)
			err := c.SupprimerFichier(nom)
			c.Close()
			attendre()
			if tc.erreurAttend && err == nil {
				t.Fatal("attendu une erreur")
			}
			if !tc.erreurAttend && err != nil {
				t.Fatalf("SupprimerFichier : %v", err)
			}
		})
	}
}

func TestSupprimerFichierNomsInvalidesSansTrafic(t *testing.T) {
	cas := []string{"", "dossier/a.mp3", "dossier\\a.mp3", strings.Repeat("n", MaxNomFichier+1)}
	for _, nom := range cas {
		addr, attendre := serveurEcriture(t, func(conn net.Conn) {
			attendreFermeture(t, conn)
		})
		c := connecter(t, addr)
		err := c.SupprimerFichier(nom)
		c.Close()
		attendre()
		if err == nil {
			t.Errorf("nom %q accepte", nom)
		}
	}
}

// serveurCleanSd repond a GetFile("clean_sd.cmd") : en-tete
// [statut][lenChemin][chemin][taille LE] puis le contenu brut.
func serveurCleanSd(t *testing.T, statut byte, contenu []byte) (string, func()) {
	t.Helper()
	return serveurEcriture(t, func(conn net.Conn) {
		op, payload, err := lireTrame(conn)
		if err != nil {
			t.Errorf("lecture requete : %v", err)
			return
		}
		if op != protocol.OpGetFile {
			t.Errorf("opcode = %d, attendu %d", op, protocol.OpGetFile)
		}
		if string(payload) != "clean_sd.cmd" {
			t.Errorf("payload = %q, attendu \"clean_sd.cmd\"", payload)
		}
		hdr := []byte{statut, byte(len(payload))}
		hdr = append(hdr, payload...)
		hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(contenu)))
		repondre(t, conn, protocol.OpGetFile, hdr)
		if statut == 0 {
			conn.Write(contenu)
		}
	})
}

func TestNettoyerCarte(t *testing.T) {
	cas := []struct {
		nom          string
		statut       byte
		contenu      []byte
		erreurAttend bool
	}{
		{"menage fait", 0, []byte{0}, false},
		{"playlist non chargee", 0, []byte{1}, true},
		{"reponse vide", 0, nil, true},
		{"getfile refuse", 2, nil, true},
	}
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			addr, attendre := serveurCleanSd(t, tc.statut, tc.contenu)
			c := connecter(t, addr)
			err := c.NettoyerCarte()
			c.Close()
			attendre()
			if tc.erreurAttend && err == nil {
				t.Fatal("attendu une erreur")
			}
			if !tc.erreurAttend && err != nil {
				t.Fatalf("NettoyerCarte : %v", err)
			}
		})
	}
}

// serveurCmd : GetFile d'une commande virtuelle `nom` repondant `contenu`.
func serveurCmd(t *testing.T, nom string, contenu []byte) (string, func()) {
	t.Helper()
	return serveurEcriture(t, func(conn net.Conn) {
		op, payload, err := lireTrame(conn)
		if err != nil {
			t.Errorf("lecture requete : %v", err)
			return
		}
		if op != protocol.OpGetFile || string(payload) != nom {
			t.Errorf("requete op=%d payload=%q, attendu GetFile %q", op, payload, nom)
		}
		hdr := []byte{0, byte(len(payload))}
		hdr = append(hdr, payload...)
		hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(contenu)))
		repondre(t, conn, protocol.OpGetFile, hdr)
		conn.Write(contenu)
	})
}

func TestAdopterPlaylist(t *testing.T) {
	for _, c := range []struct {
		nom     string
		contenu []byte
		statut  int
	}{
		{"adoptee", []byte{0, 65, 0}, 0},
		{"refusee", []byte{3}, 3},
		{"reponse vide", nil, -1},
	} {
		t.Run(c.nom, func(t *testing.T) {
			addr, attendre := serveurCmd(t, "check_playlist.cmd", c.contenu)
			cl := connecter(t, addr)
			st, err := cl.AdopterPlaylist()
			cl.Close()
			attendre()
			if err != nil || st != c.statut {
				t.Errorf("statut %d err %v, attendu %d", st, err, c.statut)
			}
		})
	}
}

func TestSupprimerFichierVerifie(t *testing.T) {
	for _, c := range []struct {
		nom       string
		reponse31 []byte
		erreur    bool
	}{
		{"supprime", []byte{1}, false},
		{"encore present", enregistrementOp31("x.mp3", 3, make([]byte, 32)), true},
		{"op31 en erreur", []byte{5}, true},
	} {
		t.Run(c.nom, func(t *testing.T) {
			addr, attendre := serveurEcriture(t, func(conn net.Conn) {
				op, payload, _ := lireTrame(conn)
				if op != protocol.OpDeleteFile || string(payload) != "x.mp3" {
					t.Errorf("1re requete op=%d payload=%q", op, payload)
				}
				repondre(t, conn, protocol.OpDeleteFile, []byte{0})
				op, _, _ = lireTrame(conn)
				if op != protocol.OpIsFileExist {
					t.Errorf("2e requete op=%d, attendu op31", op)
				}
				repondre(t, conn, protocol.OpIsFileExist, c.reponse31)
			})
			cl := connecter(t, addr)
			err := cl.SupprimerFichierVerifie("x.mp3")
			cl.Close()
			attendre()
			if (err != nil) != c.erreur {
				t.Errorf("err = %v", err)
			}
		})
	}
}

// PurgerEcoutes : televersement de clean_listening.cmd avec la charge {0x00},
// comme l'app officielle (serveurUpload verifie nom, taille et contenu).
func TestPurgerEcoutes(t *testing.T) {
	addr, attendre := serveurUpload(t, "clean_listening.cmd", []byte{0}, 0, protocol.OpUploadFile, []byte{1})
	c := connecter(t, addr)
	err := c.PurgerEcoutes()
	c.Close()
	attendre()
	if err != nil {
		t.Fatalf("PurgerEcoutes : %v", err)
	}
}
