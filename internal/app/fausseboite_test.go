package app

// Fausse boite TCP pour tester les sequences reseau de l'application
// (sauvegarde, identite, publication, restauration) sans materiel. Elle tient
// une carte SD en memoire et repond aux opcodes utilises par internal/device,
// au format documente dans docs/PROTOCOLE-2.1.23.md. clean_sd.cmd est simule :
// il efface les medias que la playlist adoptee ne reference pas.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/protocol"
)

type fausseBoite struct {
	t   *testing.T
	mac []byte

	mu       sync.Mutex
	fichiers map[string][]byte
	libre    uint32
	journal  []string // operations recues, dans l'ordre ("upload x", "get x", "suppr x")

	// Pannes injectees.
	refusUpload  map[string]bool  // acquittement d'upload en echec (statut FAIL_WRITE)
	coupeGet     map[string]bool  // la connexion est coupee au GetFile de ce nom
	tailleFausse map[string]int   // l'inventaire (op12) annonce cette taille
	statutAdopte byte             // reponse de check_playlist.cmd (0 = adoptee)
	surGet       func(nom string) // appele a chaque GetFile (effets de bord pendant la sequence)
	ecoutes      []byte           // historique (get_listening.cmd) ; nil = vide (reponse "02")
	sansMAC      bool             // op30 repond trop court
}

var macTest = []byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}

const macTestTexte = "02:11:22:33:44:55"

func nouvelleFausseBoite(t *testing.T) *fausseBoite {
	return &fausseBoite{
		t: t, mac: macTest, fichiers: map[string][]byte{}, libre: 1 << 30,
		refusUpload: map[string]bool{}, coupeGet: map[string]bool{}, tailleFausse: map[string]int{},
	}
}

// connecter demarre la fausse boite et renvoie un client connecte. La boite
// sert une seule connexion, jusqu'a sa fermeture par le client.
func (b *fausseBoite) connecter() *device.Client {
	b.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.t.Fatal(err)
	}
	fini := make(chan struct{})
	go func() {
		defer close(fini)
		conn, err := ln.Accept()
		ln.Close()
		if err != nil {
			return
		}
		defer conn.Close()
		for b.servir(conn) {
		}
	}()
	c, err := device.Dial(ln.Addr().String(), time.Second)
	if err != nil {
		b.t.Fatal(err)
	}
	c.SetTimeout(2 * time.Second)
	b.t.Cleanup(func() {
		c.Close()
		select {
		case <-fini:
		case <-time.After(2 * time.Second):
			b.t.Error("la fausse boite ne termine pas")
		}
	})
	return c
}

// operations renvoie le journal filtre par prefixe ("" = tout).
func (b *fausseBoite) operations(prefixe string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, o := range b.journal {
		if strings.HasPrefix(o, prefixe) {
			out = append(out, o)
		}
	}
	return out
}

func (b *fausseBoite) noms() []string {
	var n []string
	for k := range b.fichiers {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

func (b *fausseBoite) repondre(conn net.Conn, op byte, payload []byte) bool {
	f, err := protocol.BuildFrame(op, payload)
	if err != nil {
		b.t.Errorf("trame de reponse : %v", err)
		return false
	}
	_, err = conn.Write(f)
	return err == nil
}

// servir traite une commande ; faux quand la connexion est terminee.
func (b *fausseBoite) servir(conn net.Conn) bool {
	tete := make([]byte, 1)
	if _, err := io.ReadFull(conn, tete); err != nil {
		return false
	}
	reste := make([]byte, int(tete[0]))
	if _, err := io.ReadFull(conn, reste); err != nil {
		return false
	}
	op, p, err := protocol.ParseFrame(append(tete, reste...))
	if err != nil {
		return b.repondre(conn, protocol.OpErreur, nil)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch op {
	case protocol.OpPing, protocol.OpEndSync:
		return b.repondre(conn, op, nil)
	case protocol.OpSizeAvailable:
		return b.repondre(conn, op, binary.LittleEndian.AppendUint32(nil, b.libre))
	case protocol.OpSDSize:
		return b.repondre(conn, op, binary.LittleEndian.AppendUint32(nil, 4<<30-1))
	case protocol.OpMAC:
		if b.sansMAC {
			return b.repondre(conn, op, []byte{1})
		}
		return b.repondre(conn, op, b.mac)
	case protocol.OpNumberOfFiles:
		return b.repondre(conn, op, binary.LittleEndian.AppendUint16(nil, uint16(len(b.fichiers))))
	case protocol.OpFileInformation:
		noms := b.noms()
		i := int(binary.LittleEndian.Uint16(p))
		if i >= len(noms) {
			return b.repondre(conn, op, []byte{1})
		}
		taille := len(b.fichiers[noms[i]])
		if t, ok := b.tailleFausse[noms[i]]; ok {
			taille = t
		}
		r := append([]byte{0, byte(len(noms[i]))}, noms[i]...)
		r = binary.LittleEndian.AppendUint32(r, uint32(taille))
		return b.repondre(conn, op, r)
	case protocol.OpIsFileExist:
		nom := string(p[1:])
		d, ok := b.fichiers[nom]
		if !ok {
			return b.repondre(conn, op, []byte{1})
		}
		sum := sha256.Sum256(d)
		r := append([]byte{0, byte(len(nom))}, nom...)
		r = binary.LittleEndian.AppendUint32(r, uint32(len(d)))
		return b.repondre(conn, op, append(r, sum[:]...))
	case protocol.OpDeleteFile:
		b.journal = append(b.journal, "suppr "+string(p))
		delete(b.fichiers, string(p))
		return b.repondre(conn, op, []byte{0})
	case protocol.OpGetFile:
		return b.envoyerFichier(conn, string(p))
	case protocol.OpUploadFile:
		return b.recevoirFichier(conn, p)
	}
	b.t.Errorf("opcode %d non simule", op)
	return false
}

// envoyerFichier repond a op13 : en-tete [statut][len][nom][taille] puis le
// contenu brut. Les commandes virtuelles .cmd sont executees.
func (b *fausseBoite) envoyerFichier(conn net.Conn, nom string) bool {
	b.journal = append(b.journal, "get "+nom)
	if b.surGet != nil {
		b.surGet(nom)
	}
	var corps []byte
	switch nom {
	case "check_playlist.cmd":
		corps = []byte{b.statutAdopte, 0, 0}
	case "clean_sd.cmd":
		b.nettoyer()
		corps = []byte{0}
	case "get_listening.cmd":
		if b.ecoutes == nil {
			return b.repondre(conn, protocol.OpGetFile, []byte{2}) // vide : statut seul
		}
		corps = b.ecoutes
	default:
		if b.coupeGet[nom] {
			return false
		}
		d, ok := b.fichiers[nom]
		if !ok {
			return b.repondre(conn, protocol.OpGetFile, append([]byte{1, byte(len(nom))}, append([]byte(nom), 0, 0, 0, 0)...))
		}
		corps = d
	}
	tete := append([]byte{0, byte(len(nom))}, nom...)
	tete = binary.LittleEndian.AppendUint32(tete, uint32(len(corps)))
	if !b.repondre(conn, protocol.OpGetFile, tete) {
		return false
	}
	_, err := conn.Write(corps)
	return err == nil
}

// recevoirFichier repond a op1 : en-tete, statut 0, flux brut, acquittement.
func (b *fausseBoite) recevoirFichier(conn net.Conn, p []byte) bool {
	n := int(p[0])
	nom := string(p[1 : 1+n])
	taille := int(binary.LittleEndian.Uint32(p[1+n:]))
	sha := p[1+n+4 : 1+n+4+32]
	if !b.repondre(conn, protocol.OpUploadFile, []byte{0}) {
		return false
	}
	d := make([]byte, taille)
	if _, err := io.ReadFull(conn, d); err != nil {
		return false
	}
	b.journal = append(b.journal, "upload "+nom)
	if taille == 0 && !strings.HasSuffix(nom, ".cmd") {
		b.t.Errorf("televersement de 0 octet (%s) : la vraie boite ne l'acquitte pas", nom)
	}
	sum := sha256.Sum256(d)
	if b.refusUpload[nom] || !bytes.Equal(sum[:], sha) {
		return b.repondre(conn, protocol.OpUploadFile, []byte{8})
	}
	if !strings.HasSuffix(nom, ".cmd") {
		b.fichiers[nom] = d
	}
	return b.repondre(conn, protocol.OpUploadFile, []byte{1})
}

// nettoyer simule clean_sd.cmd : tout media (uuid.ext) que la playlist en place
// ne reference pas est efface ; les fichiers systeme restent.
func (b *fausseBoite) nettoyer() {
	enc, ok := b.fichiers["pl_enc.bin"]
	if !ok {
		return
	}
	clair, err := device.DechiffrerPlaylist(b.mac, enc)
	if err != nil {
		b.t.Errorf("clean_sd simule : %v", err)
		return
	}
	items, err := playlist.DecodeV2(clair)
	if err != nil {
		b.t.Errorf("clean_sd simule : %v", err)
		return
	}
	ref := map[string]bool{}
	for _, it := range items {
		ref[it.FileName] = true
	}
	for nom := range b.fichiers {
		if reMedia.MatchString(nom) && !ref[strings.SplitN(nom, ".", 2)[0]] {
			delete(b.fichiers, nom)
		}
	}
}

// poserPlaylist chiffre l'arbre pour la fausse boite et le pose en pl_enc.bin.
func (b *fausseBoite) poserPlaylist(arbre []*playlist.Node) []byte {
	b.t.Helper()
	clair := playlist.SerialiserV2(playlist.SerialiserArbreV2(arbre, nil))
	enc, err := device.ChiffrerPlaylist(b.mac, clair, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	b.fichiers["pl_enc.bin"] = enc
	return enc
}

// arbreAdopte relit l'arbre en place sur la fausse boite.
func (b *fausseBoite) arbreAdopte() []*playlist.Node {
	b.t.Helper()
	clair, err := device.DechiffrerPlaylist(b.mac, b.fichiers["pl_enc.bin"])
	if err != nil {
		b.t.Fatal(err)
	}
	items, err := playlist.DecodeV2(clair)
	if err != nil {
		b.t.Fatal(err)
	}
	arbre, err := playlist.BuildTree(items)
	if err != nil {
		b.t.Fatal(err)
	}
	return arbre
}
