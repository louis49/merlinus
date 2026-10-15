// Package device parle a une enceinte Merlin sur son reseau local. L'enceinte
// n'ouvre son serveur de commandes (TCP 192.168.4.1:50000) QUE lorsqu'elle est
// en mode point d'acces : la machine appelante doit etre connectee au Wi-Fi de
// l'enceinte, et perd donc son acces Internet.
//
// Ce fichier porte les commandes de lecture ; les ecritures (dont la
// suppression op15 et le menage clean_sd.cmd) sont dans ecriture.go, le Wi-Fi
// et le chiffrement de la playlist dans wifi.go.
package device

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/louis49/merlinus/internal/protocol"
)

// Adresse par defaut du serveur de commandes en mode AP.
const (
	DefaultHost = "192.168.4.1"
	DefaultPort = 50000
)

func DefaultAddr() string { return fmt.Sprintf("%s:%d", DefaultHost, DefaultPort) }

// Client detient la connexion TCP unique vers l'enceinte.
type Client struct {
	conn    net.Conn
	timeout time.Duration
}

// Dial ouvre la connexion. Un echec (port ferme) signifie en pratique que
// l'enceinte n'est pas en mode AP, ou que la machine n'est pas sur son Wi-Fi.
func Dial(addr string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, timeout: timeout}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// SetTimeout ajuste le delai de lecture/ecriture des commandes suivantes.
func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }

// command envoie une trame et lit la reponse tramee. L'enceinte reutilise
// l'opcode de la commande ; un opcode 0xFF signale un rejet CRC de son cote.
func (c *Client) command(op byte, payload []byte) ([]byte, error) {
	frame, err := protocol.BuildFrame(op, payload)
	if err != nil {
		return nil, err
	}
	c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.conn.Write(frame); err != nil {
		return nil, err
	}
	rop, rpayload, err := c.readFrame()
	if err != nil {
		return nil, err
	}
	if rop == protocol.OpErreur {
		return nil, fmt.Errorf("trame rejetee par l'enceinte (CRC)")
	}
	if rop != op {
		return nil, fmt.Errorf("opcode de reponse inattendu : %d au lieu de %d", rop, op)
	}
	return rpayload, nil
}

// CommandeBrute envoie une trame et renvoie la reponse telle quelle (opcode
// recu compris, sans controle). Reserve aux sondes de constructeur/ qui
// etudient une commande avant qu'elle ait sa methode dediee.
func (c *Client) CommandeBrute(op byte, payload []byte) (rop byte, resp []byte, err error) {
	frame, err := protocol.BuildFrame(op, payload)
	if err != nil {
		return 0, nil, err
	}
	c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.conn.Write(frame); err != nil {
		return 0, nil, err
	}
	return c.readFrame()
}

// readFrame lit une trame complete : octet de longueur puis `len` octets.
func (c *Client) readFrame() (op byte, payload []byte, err error) {
	head := make([]byte, 1)
	if _, err := io.ReadFull(c.conn, head); err != nil {
		return 0, nil, err
	}
	length := int(head[0])
	if length < 5 {
		return 0, nil, fmt.Errorf("octet de longueur aberrant : %d", length)
	}
	rest := make([]byte, length)
	if _, err := io.ReadFull(c.conn, rest); err != nil {
		return 0, nil, err
	}
	return protocol.ParseFrame(append(head, rest...))
}

// ---- Commandes de lecture ----

// Ping (op 2) : reponse vide. Sert d'indicateur de joignabilite.
func (c *Client) Ping() error {
	_, err := c.command(protocol.OpPing, nil)
	return err
}

// SizeAvailable (op 3) : octets libres sur la carte SD.
func (c *Client) SizeAvailable() (uint32, error) { return c.lireU32(protocol.OpSizeAvailable) }

// SDSize (op 4) : taille totale de la carte SD, en octets.
func (c *Client) SDSize() (uint32, error) { return c.lireU32(protocol.OpSDSize) }

func (c *Client) lireU32(op byte) (uint32, error) {
	p, err := c.command(op, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 4 {
		return 0, fmt.Errorf("reponse trop courte pour op %d", op)
	}
	return binary.LittleEndian.Uint32(p[:4]), nil
}

// Firmware regroupe une version d'enceinte.
type Firmware struct {
	Major uint8  `json:"major"`
	Minor uint8  `json:"minor"`
	Patch uint16 `json:"patch"`
}

func (f Firmware) String() string { return fmt.Sprintf("%d.%d.%d", f.Major, f.Minor, f.Patch) }

// FirmwareVersion (op 5) : [major:1][minor:1][patch:2 LE].
func (c *Client) FirmwareVersion() (Firmware, error) {
	p, err := c.command(protocol.OpFirmwareVersion, nil)
	if err != nil {
		return Firmware{}, err
	}
	if len(p) < 4 {
		return Firmware{}, fmt.Errorf("reponse trop courte pour op 5")
	}
	return Firmware{Major: p[0], Minor: p[1], Patch: binary.LittleEndian.Uint16(p[2:4])}, nil
}

// NumberOfFiles (op 11) : nombre de fichiers a la racine de la carte SD.
func (c *Client) NumberOfFiles() (uint16, error) {
	p, err := c.command(protocol.OpNumberOfFiles, nil)
	if err != nil {
		return 0, err
	}
	if len(p) < 2 {
		return 0, fmt.Errorf("reponse trop courte pour op 11")
	}
	return binary.LittleEndian.Uint16(p[:2]), nil
}

// FichierInfo : un fichier a la racine de la carte SD (nom + taille effective).
type FichierInfo struct {
	Nom    string `json:"nom"`
	Taille uint32 `json:"taille"` // octets, tels que rapportes par l'enceinte
}

// FileInformation (op 12) renvoie le nom et la taille du fichier a l'index donne.
// Requete [index:2 LE][withSha:1] ; on demande withSha=0 (le calcul du SHA est
// couteux et inutile ici). Reponse (miroir de l'en-tete d'upload op 1) :
// [status:1][nameLen:1][name][size:4 LE][sha256:32]. Valide en reel (fw 2.1.15).
func (c *Client) FileInformation(index uint16) (FichierInfo, error) {
	payload := []byte{byte(index), byte(index >> 8), 0} // index LE + withSha=0
	p, err := c.command(protocol.OpFileInformation, payload)
	if err != nil {
		return FichierInfo{}, err
	}
	if len(p) < 2 {
		return FichierInfo{}, fmt.Errorf("reponse op 12 trop courte (%d octets)", len(p))
	}
	if p[0] != 0 {
		return FichierInfo{}, fmt.Errorf("op 12 statut %d a l'index %d", p[0], index)
	}
	nameLen := int(p[1])
	if 2+nameLen+4 > len(p) {
		return FichierInfo{}, fmt.Errorf("reponse op 12 tronquee (nameLen=%d, %d octets)", nameLen, len(p))
	}
	nom := string(p[2 : 2+nameLen])
	taille := binary.LittleEndian.Uint32(p[2+nameLen : 2+nameLen+4])
	return FichierInfo{Nom: nom, Taille: taille}, nil
}

// ListerFichiers parcourt l'inventaire complet de la carte SD (op 11 pour le
// nombre, puis op 12 pour chaque index). Un echec en cours de route renvoie ce
// qui a deja ete lu avec l'erreur (l'appelant peut degrader gracieusement).
// progress, si non nil, est appele apres chaque fichier (fait, total) : l'op est
// lente (un aller-retour par fichier), l'IHM doit montrer l'avancement.
func (c *Client) ListerFichiers(progress func(fait, total int)) ([]FichierInfo, error) {
	n, err := c.NumberOfFiles()
	if err != nil {
		return nil, err
	}
	out := make([]FichierInfo, 0, n)
	for i := uint16(0); i < n; i++ {
		fi, err := c.FileInformation(i)
		if err != nil {
			return out, fmt.Errorf("fichier %d/%d : %w", i, n, err)
		}
		out = append(out, fi)
		if progress != nil {
			progress(int(i)+1, int(n))
		}
	}
	return out, nil
}

// Batterie decrit l'etat de la batterie de l'enceinte.
type Batterie struct {
	Niveau   uint8 `json:"niveau"`    // pourcentage
	EnCharge bool  `json:"en_charge"` // sur secteur / en charge
}

// Battery (op 14) : [niveau:1][charge:1].
func (c *Client) Battery() (Batterie, error) {
	p, err := c.command(protocol.OpBattery, nil)
	if err != nil {
		return Batterie{}, err
	}
	if len(p) < 2 {
		return Batterie{}, fmt.Errorf("reponse trop courte pour op 14")
	}
	return Batterie{Niveau: p[0], EnCharge: p[1] != 0}, nil
}

// MACBytes (op 30) : les 6 octets bruts du MAC SoftAP. Sert a la fois a
// l'affichage et a la derivation de cle du Wi-Fi station (cf. wifi.go).
func (c *Client) MACBytes() ([]byte, error) {
	p, err := c.command(protocol.OpMAC, nil)
	if err != nil {
		return nil, err
	}
	if len(p) < 6 {
		return nil, fmt.Errorf("reponse trop courte pour op 30")
	}
	return append([]byte(nil), p[:6]...), nil
}

// MAC (op 30) : MAC de la SoftAP au format "xx:xx:xx:xx:xx:xx".
func (c *Client) MAC() (string, error) {
	p, err := c.MACBytes()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", p[0], p[1], p[2], p[3], p[4], p[5]), nil
}

// WifiConfig est la configuration Wi-Fi stockee, renvoyee par l'op 17. Format
// observe : [flag:1][lenSSID:1][lenPass:1][ssid][password]. Sur la boite de
// reference, ce sont les identifiants de SON point d'acces (MERLIN_...).
//
// ATTENTION : cette commande renvoie aussi un mot de passe. L'appelant ne doit
// exposer que le SSID (identifiant du reseau), jamais Password.
type WifiConfig struct {
	SSID     string
	Password string // ne jamais journaliser ni transmettre a l'interface
	Flag     uint8
}

// WifiConfig (op 17) lit la configuration Wi-Fi stockee, sans authentification.
func (c *Client) WifiConfig() (WifiConfig, error) {
	p, err := c.command(protocol.OpGetWifiConfig, nil)
	if err != nil {
		return WifiConfig{}, err
	}
	return decoderWifiConfig(p)
}

// decoderWifiConfig decode la reponse d'op17 : [flag][lenSSID][lenPass][SSID][pass].
// Une reponse plus courte que ce qu'elle annonce est une erreur (et non un SSID
// vide), pour ne pas inscrire une valeur fausse.
func decoderWifiConfig(p []byte) (WifiConfig, error) {
	var w WifiConfig
	if len(p) < 3 {
		return w, fmt.Errorf("reponse Wi-Fi tronquee (%d octets)", len(p))
	}
	w.Flag = p[0]
	lenSSID, lenPass, off := int(p[1]), int(p[2]), 3
	if off+lenSSID+lenPass > len(p) {
		return WifiConfig{}, fmt.Errorf("reponse Wi-Fi tronquee (%d octets, %d annonces)", len(p), off+lenSSID+lenPass)
	}
	w.SSID = string(p[off : off+lenSSID])
	off += lenSSID
	w.Password = string(p[off : off+lenPass])
	return w, nil
}

// ErrFichierRefuse : la boite refuse un GetFile (statut non nul : fichier
// absent, ou commande virtuelle sans resultat).
type ErrFichierRefuse struct {
	Nom    string
	Statut byte
}

func (e *ErrFichierRefuse) Error() string {
	return fmt.Sprintf("l'enceinte refuse le fichier %q (statut %d)", e.Nom, e.Statut)
}

// GetFile (op 13 / 0x0d) telecharge un fichier de la carte SD dans w.
//
// Format : requete [op][chemin], puis une trame d'en-tete
// [status:1][lenChemin:1][chemin][taille:4 LE], suivie des `taille` octets
// bruts du fichier, ecrits directement sur la socket (ni trame, ni CRC, ni
// statut final). Transport porte de github.com/gawen/pinpin (GetFile).
func (c *Client) GetFile(name string, w io.Writer) (int64, error) {
	frame, err := protocol.BuildFrame(protocol.OpGetFile, []byte(name))
	if err != nil {
		return 0, err
	}
	c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.conn.Write(frame); err != nil {
		return 0, err
	}

	op, payload, err := c.readFrame()
	if err != nil {
		return 0, err
	}
	if op == protocol.OpErreur {
		return 0, fmt.Errorf("trame rejetee par l'enceinte (CRC)")
	}
	if op != protocol.OpGetFile {
		return 0, fmt.Errorf("opcode de reponse inattendu : %d au lieu de %d", op, protocol.OpGetFile)
	}
	// payload = [status:1][lenChemin:1][chemin][taille:4 LE]. Un refus peut se
	// reduire au seul statut (ex. get_listening.cmd sans historique : "02") :
	// le statut se lit donc avant toute exigence de longueur.
	if len(payload) < 1 {
		return 0, fmt.Errorf("en-tete GetFile vide")
	}
	if status := payload[0]; status != 0 {
		return 0, &ErrFichierRefuse{Nom: name, Statut: status}
	}
	if len(payload) < 2 {
		return 0, fmt.Errorf("en-tete GetFile trop courte")
	}
	pathLen := int(payload[1])
	if len(payload) < 2+pathLen+4 {
		return 0, fmt.Errorf("en-tete GetFile incomplete pour %q", name)
	}
	size := binary.LittleEndian.Uint32(payload[2+pathLen : 2+pathLen+4])

	// Plafond de securite : la taille est annoncee par la boite. Une valeur
	// aberrante (firmware corrompu, pair hostile) ferait gonfler le tampon
	// jusqu'a la deadline. 128 Mo est une marge tres large sur les fichiers
	// reels (pl.bin ~ 57 Ko, vignettes quelques Ko, .cmd 29 o).
	const maxGetFile = 128 << 20
	if size > maxGetFile {
		return 0, fmt.Errorf("taille annoncee pour %q trop grande : %d octets (max %d)", name, size, maxGetFile)
	}

	// Lecture du flux brut avec un IDLE timeout : la deadline est repoussee a
	// chaque bloc recu. Un gros fichier (audio de plusieurs Mo) qui transfere
	// regulierement ne time out jamais, quelle que soit sa taille ni la lenteur du
	// lien ; seul un vrai gel (idleTimeout sans un seul octet) coupe. Preferable a
	// un plafond total fixe, qui casse les gros fichiers sur lien lent.
	const idleTimeout = 30 * time.Second
	buf := make([]byte, 64*1024)
	var n int64
	for n < int64(size) {
		c.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		reste := int64(size) - n
		if reste < int64(len(buf)) {
			buf = buf[:reste]
		}
		lu, err := c.conn.Read(buf)
		if lu > 0 {
			if _, werr := w.Write(buf[:lu]); werr != nil {
				return n, fmt.Errorf("ecriture de %q : %w", name, werr)
			}
			n += int64(lu)
		}
		if err != nil {
			return n, fmt.Errorf("lecture de %q : %w", name, err)
		}
	}
	return n, nil
}

// GetFileBytes est le raccourci de GetFile vers un tampon memoire.
func (c *Client) GetFileBytes(name string) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := c.GetFile(name, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
