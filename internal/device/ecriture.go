package device

// Commandes d'ECRITURE du protocole local (televersement op1, suppression op15,
// presence op31, commandes virtuelles .cmd). Chemin de publication 2.1.23
// (docs/PROTOCOLE-2.1.23.md section 5) : pl_enc.bin televerse, adopte par
// check_playlist.cmd (sans rien supprimer), puis clean_sd.cmd efface tout media
// que l'arbre adopte ne reference plus. Toujours publier l'arbre COMPLET
// (existant + ajouts), jamais un delta.
//
// UpdatePlaylist (op6, chemin JSON herite) n'est plus utilise par l'application,
// seulement par des outils de constructeur/.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/protocol"
)

// MaxNomFichier : le tampon de destination du firmware tient sous 94 octets.
const MaxNomFichier = 93

// IsFileExist (op 31) interroge la presence d'un fichier a la racine. Renvoie
// (present, sha_identique). Sert a eviter de re-televerser un fichier deja bon.
func (c *Client) IsFileExist(name string, sha []byte) (present, matching bool, err error) {
	payload := append([]byte{1}, []byte(name)...) // withSha=1 : demande le sha en retour
	resp, err := c.command(protocol.OpIsFileExist, payload)
	if err != nil {
		return false, false, err
	}
	if len(resp) < 1 {
		return false, false, fmt.Errorf("reponse vide pour op 31")
	}
	switch resp[0] {
	case 1:
		return false, false, nil // absent
	case 0:
		// enregistrement : [nameLen:1][name][size:4 LE][sha256:32]. La vraie boite
		// le renvoie complet (sonde du 2026-10-14) : tronque = reponse corrompue.
		rec := resp[1:]
		if len(rec) < 1 || len(rec) < 1+int(rec[0])+4+32 {
			return false, false, fmt.Errorf("op 31 : enregistrement tronque pour %q (%d octets)", name, len(rec))
		}
		off := 1 + int(rec[0])
		// Forme exacte du nom renvoye non documentee : un ecart est seulement
		// journalise (a durcir une fois la forme confirmee en reel).
		if nom := string(rec[1:off]); nom != name {
			log.Printf("op 31 : nom renvoye %q different du nom demande %q", nom, name)
		}
		fileSha := rec[off+4 : off+4+32]
		return true, sha != nil && bytes.Equal(fileSha, sha), nil
	default:
		return false, false, fmt.Errorf("op 31 : statut %d", resp[0])
	}
}

// UploadFile (op 1) ecrit un fichier a la racine de la carte : trame d'en-tete
// [nameLen:1][name][size:4 LE][sha256:32], statut 0, puis EXACTEMENT size octets
// bruts sur la meme socket, puis statut 1 (SHA valide).
//
// Refuse les noms en .cmd : cote enceinte ce sont des APPELS DE PROCEDURE
// (commandes RAM), pas de simples fichiers. Pour en invoquer un sciemment,
// passer par EnvoyerCommandeRAM.
func (c *Client) UploadFile(name string, data []byte, progress func(sent, total int)) error {
	if strings.HasSuffix(name, ".cmd") {
		return fmt.Errorf("nom en .cmd interdit via UploadFile (utiliser EnvoyerCommandeRAM)")
	}
	return c.uploadBrut(name, data, progress)
}

// EnvoyerCommandeRAM televerse un fichier de commande virtuel (.cmd) : l'enceinte
// le reconnait au nom et execute la procedure correspondante avec le payload
// (ex. "set_enabling_hours.cmd"). Meme transport que UploadFile (op 1).
func (c *Client) EnvoyerCommandeRAM(nomCmd string, payload []byte) error {
	if !strings.HasSuffix(nomCmd, ".cmd") {
		return fmt.Errorf("commande RAM : le nom doit finir par .cmd")
	}
	return c.uploadBrut(nomCmd, payload, nil)
}

// uploadBrut realise le televersement op 1 sans juger du nom (coeur partage par
// UploadFile et EnvoyerCommandeRAM).
func (c *Client) uploadBrut(name string, data []byte, progress func(sent, total int)) error {
	raw := []byte(name)
	if len(raw) == 0 || len(raw) > MaxNomFichier {
		return fmt.Errorf("nom de fichier de %d octets (max %d)", len(raw), MaxNomFichier)
	}
	sum := sha256.Sum256(data)
	header := make([]byte, 0, 1+len(raw)+4+32)
	header = append(header, byte(len(raw)))
	header = append(header, raw...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(data)))
	header = append(header, sum[:]...)

	st, err := c.statut(protocol.OpUploadFile, header)
	if err != nil {
		return fmt.Errorf("en-tete upload %q : %w", name, err)
	}
	if st != 0 {
		return fmt.Errorf("en-tete upload %q refuse (statut %s)", name, nomStatutUpload(st))
	}

	// Flux brut : exactement len(data) octets, sans trame ni CRC.
	c.conn.SetDeadline(time.Now().Add(c.timeout + time.Duration(len(data)/50000)*time.Second))
	const bloc = 102400
	for sent := 0; sent < len(data); {
		fin := sent + bloc
		if fin > len(data) {
			fin = len(data)
		}
		n, err := c.conn.Write(data[sent:fin])
		if err != nil {
			return fmt.Errorf("flux %q : %w", name, err)
		}
		sent += n
		if progress != nil {
			progress(sent, len(data))
		}
	}

	op, resp, err := c.readFrame()
	if err != nil {
		return fmt.Errorf("acquittement upload %q : %w", name, err)
	}
	if op == protocol.OpErreur {
		return fmt.Errorf("acquittement upload %q : trame rejetee par l'enceinte (CRC)", name)
	}
	if op != protocol.OpUploadFile || len(resp) < 1 {
		return fmt.Errorf("acquittement upload %q inattendu", name)
	}
	if resp[0] != 1 {
		return fmt.Errorf("transfert %q refuse (statut %s)", name, nomStatutUpload(resp[0]))
	}
	return nil
}

// UpdatePlaylist (op 6, chemin JSON herite, hors application) fait relire par
// l'enceinte le JSON deja televerse et efface les medias qu'il ne reference pas. `name` doit finir par .json et faire >= 6 octets.
func (c *Client) UpdatePlaylist(name string) error {
	if !strings.HasSuffix(name, ".json") {
		return fmt.Errorf("le nom de playlist doit finir par .json")
	}
	if len([]byte(name)) < 6 {
		return fmt.Errorf("nom de playlist trop court")
	}
	resp, err := c.command(protocol.OpUpdatePlaylist, []byte(name))
	if err != nil {
		return err
	}
	if len(resp) < 1 {
		return fmt.Errorf("reponse vide pour op 6")
	}
	if resp[0] != 0 {
		detail := ""
		if (resp[0] == 17 || resp[0] == 18) && len(resp) > 1 {
			detail = " (" + string(resp[1:]) + ")"
		}
		return fmt.Errorf("update_playlist refuse : statut %s%s", nomStatutUpdate(resp[0]), detail)
	}
	return nil
}

// EndSync (op 9) termine proprement la session ; l'enceinte quitte le mode AP.
func (c *Client) EndSync() error {
	_, err := c.command(protocol.OpEndSync, nil)
	return err
}

// statut envoie une trame et renvoie le premier octet de la reponse.
func (c *Client) statut(op byte, payload []byte) (byte, error) {
	resp, err := c.command(op, payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 1 {
		return 0, fmt.Errorf("reponse sans statut pour op %d", op)
	}
	return resp[0], nil
}

func nomStatutUpload(s byte) string {
	noms := map[byte]string{0: "SUCCESS", 1: "SHA256_VALID", 2: "NOT_ENOUGH_SPACE",
		3: "FILENAME_TOO_LARGE", 4: "SHA256_INVALID", 5: "BAD_LENGTH_CMD",
		6: "TIMEOUT", 7: "FAIL_CREATE_FILE", 8: "FAIL_WRITE"}
	if n, ok := noms[s]; ok {
		return n
	}
	return fmt.Sprintf("%d", s)
}

func nomStatutUpdate(s byte) string {
	noms := map[byte]string{0: "SUCCESS", 13: "TITLE_TOO_LARGE",
		17: "IMAGE_NOT_FOUND", 18: "MUSIC_NOT_FOUND"}
	if n, ok := noms[s]; ok {
		return n
	}
	return fmt.Sprintf("%d", s)
}

// SupprimerFichier (op 15, DELETE_FILE) efface un fichier a la racine de la
// carte. Charge utile : le nom brut. La boite repond 00 MEME si le fichier
// n'existe pas (valide en reel, cf. constructeur/sonde-op15) : l'appelant doit
// verifier la disparition par IsFileExist.
func (c *Client) SupprimerFichier(name string) error {
	if name == "" || len(name) > MaxNomFichier || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("nom de fichier invalide : %q", name)
	}
	st, err := c.statut(protocol.OpDeleteFile, []byte(name))
	if err != nil {
		return err
	}
	if st != 0 {
		return fmt.Errorf("op 15 : statut %d", st)
	}
	return nil
}

// ErrNonSupprime : op15 a repondu sans erreur mais le fichier est encore
// present (op15 repond 00 meme sans effet : seule op31 fait foi).
var ErrNonSupprime = errors.New("fichier toujours present apres suppression")

// SupprimerFichierVerifie supprime name (op15) puis verifie son absence (op31).
func (c *Client) SupprimerFichierVerifie(name string) error {
	if err := c.SupprimerFichier(name); err != nil {
		return err
	}
	present, _, err := c.IsFileExist(name, nil)
	if err != nil {
		return err
	}
	if present {
		return ErrNonSupprime
	}
	return nil
}

// PurgerEcoutes vide l'historique d'ecoute de la boite, comme l'app officielle
// juste apres l'avoir lu (i74.java : sendCommandFile("clean_listening.cmd") =
// televersement op1 de ce .cmd avec la charge utile {0x00}).
func (c *Client) PurgerEcoutes() error {
	return c.EnvoyerCommandeRAM("clean_listening.cmd", []byte{0})
}

// AdopterPlaylist fait charger par la boite la playlist qu'on vient de
// televerser (GetFile "check_playlist.cmd" : lecture, dechiffrement, chargement,
// SANS rien supprimer). Reponse [status:1][count:u16 LE] : renvoie le statut
// (0 = adoptee ; -1 si la reponse est vide) ; err = echec de transport.
func (c *Client) AdopterPlaylist() (statut int, err error) {
	body, err := c.GetFileBytes("check_playlist.cmd")
	if err != nil {
		return -1, err
	}
	if len(body) < 1 {
		return -1, nil
	}
	return int(body[0]), nil
}

// NettoyerCarte declenche le menage de la boite : GetFile("clean_sd.cmd")
// recharge la playlist EN PLACE puis supprime tout fichier qu'elle ne reference
// pas (fichiers systeme proteges par le firmware). Reponse 1 octet : 0 = fait,
// 1 = playlist non chargee (rien supprime). Valide en reel le 2026-10-14.
// A n'appeler qu'apres l'adoption d'un arbre COMPLET : un arbre appauvri ferait
// effacer tout ce qu'il ne reference plus.
func (c *Client) NettoyerCarte() error {
	resp, err := c.GetFileBytes("clean_sd.cmd")
	if err != nil {
		return err
	}
	if len(resp) < 1 || resp[0] != 0 {
		return fmt.Errorf("clean_sd.cmd : reponse %x", resp)
	}
	return nil
}
