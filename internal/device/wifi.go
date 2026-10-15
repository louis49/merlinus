// Configuration du Wi-Fi STATION de la boite (le reseau domestique que la boite
// rejoint pour atteindre Internet). A ne PAS confondre avec le point d'acces de
// la boite (cf. docs/WIFI.md).
//
// Format reproduit a l'identique de l'app officielle (confirme par lecture reelle
// et dechiffrement local) :
//
//	sta_wifi.json = [ { "ssid": "<reseau>", "pwd": "<hexa>", "encode": true } ]
//
// ou pwd = hexadecimal MAJUSCULE de  IV(12) || AES-256-GCM(clair) || tag(16),
// cle = SHA-256(6 octets du MAC SoftAP, renvoye par op30).
//
// Le mot de passe en clair ne doit JAMAIS etre journalise.
package device

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Contraintes Wi-Fi usuelles (WPA/WPA2). La boite verifie aussi de son cote.
const (
	maxOctetsSSID    = 32
	minOctetsMotPass = 8
	maxOctetsMotPass = 63
)

// entreeStaWifi est une entree du tableau sta_wifi.json.
type entreeStaWifi struct {
	SSID   string `json:"ssid"`
	Pwd    string `json:"pwd"`
	Encode bool   `json:"encode"`
}

// chiffrerMotDePasseWifi chiffre le mot de passe comme la boite l'attend :
// AES-256-GCM (cle = SHA256(macBytes)), IV aleatoire de 12 octets, puis hexa
// majuscule de IV || chiffre || tag. `alea` permet d'injecter une source
// deterministe en test ; nil -> crypto/rand.
func chiffrerMotDePasseWifi(macBytes []byte, motDePasse string, alea io.Reader) (string, error) {
	if len(macBytes) != 6 {
		return "", fmt.Errorf("MAC attendu sur 6 octets, recu %d", len(macBytes))
	}
	if alea == nil {
		alea = rand.Reader
	}
	cle := sha256.Sum256(macBytes)
	bloc, err := aes.NewCipher(cle[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(bloc)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize()) // 12
	if _, err := io.ReadFull(alea, iv); err != nil {
		return "", fmt.Errorf("tirage de l'IV : %w", err)
	}
	scelle := gcm.Seal(nil, iv, []byte(motDePasse), nil) // chiffre || tag
	blob := append(append([]byte(nil), iv...), scelle...)
	return strings.ToUpper(hex.EncodeToString(blob)), nil
}

// dechiffrerMotDePasseWifi fait l'operation inverse (utilise en test et pour
// verifier un aller-retour). Renvoie le clair ; l'appelant ne doit pas le logger.
func dechiffrerMotDePasseWifi(macBytes []byte, pwdHex string) (string, error) {
	if len(macBytes) != 6 {
		return "", fmt.Errorf("MAC attendu sur 6 octets, recu %d", len(macBytes))
	}
	blob, err := hex.DecodeString(pwdHex)
	if err != nil {
		return "", fmt.Errorf("pwd non hexadecimal : %w", err)
	}
	cle := sha256.Sum256(macBytes)
	bloc, err := aes.NewCipher(cle[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(bloc)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return "", fmt.Errorf("blob trop court (%d octets)", len(blob))
	}
	clair, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("dechiffrement GCM : %w", err)
	}
	return string(clair), nil
}

// DechiffrerPlaylist dechiffre le playlist chiffre pl_enc.bin, que l'app
// officielle pose a la place de pl.bin (observe sur firmware 2.1.23 apres une
// synchro officielle). Schema identique au mot de passe Wi-Fi, CONFIRME par tag
// GCM valide : blob = IV(12) || AES-256-GCM(clair) || tag(16), cle =
// SHA-256(MAC SoftAP = 6 octets d'op30). Le clair est au format DecodeV2 (96 o
// par enregistrement). Entierement reproductible hors-ligne (la cle derive du
// MAC qu'on lit nous-memes), aucun backend.
func DechiffrerPlaylist(macBytes, blob []byte) ([]byte, error) {
	if len(macBytes) != 6 {
		return nil, fmt.Errorf("MAC attendu sur 6 octets, recu %d", len(macBytes))
	}
	cle := sha256.Sum256(macBytes)
	bloc, err := aes.NewCipher(cle[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(bloc)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("pl_enc.bin trop court (%d octets)", len(blob))
	}
	clair, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("dechiffrement GCM (cle = SHA-256 du MAC) : %w", err)
	}
	return clair, nil
}

// ChiffrerPlaylist est l'INVERSE de DechiffrerPlaylist : chiffre un pl.bin clair
// (format DecodeV2) en pl_enc.bin, meme schema que l'app officielle : IV(12)
// aleatoire || AES-256-GCM(clair) || tag(16), cle = SHA-256(MAC SoftAP op30).
// `alea` permet d'injecter une source deterministe en test ; nil -> crypto/rand.
func ChiffrerPlaylist(macBytes, clair []byte, alea io.Reader) ([]byte, error) {
	if len(macBytes) != 6 {
		return nil, fmt.Errorf("MAC attendu sur 6 octets, recu %d", len(macBytes))
	}
	if alea == nil {
		alea = rand.Reader
	}
	cle := sha256.Sum256(macBytes)
	bloc, err := aes.NewCipher(cle[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(bloc)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(alea, iv); err != nil {
		return nil, fmt.Errorf("tirage de l'IV : %w", err)
	}
	scelle := gcm.Seal(nil, iv, clair, nil) // chiffre || tag
	return append(append([]byte(nil), iv...), scelle...), nil
}

// contenuStaWifi serialise le tableau sta_wifi.json pour un unique reseau.
func contenuStaWifi(ssid, pwdHex string) ([]byte, error) {
	return json.Marshal([]entreeStaWifi{{SSID: ssid, Pwd: pwdHex, Encode: true}})
}

// DefinirWifi pose la configuration du Wi-Fi station sur la boite : lit le MAC
// SoftAP (op30) pour deriver la cle, chiffre le mot de passe, et televerse
// sta_wifi.json (op1). La boite doit etre en mode point d'acces.
//
// Ne touche ni a la playlist ni aux medias : on depose seulement le fichier. Le moment ou la boite relit cette config (reboot / prochain cycle)
// reste a confirmer en reel.
func (c *Client) DefinirWifi(ssid, motDePasse string) error {
	if n := len([]byte(ssid)); n == 0 || n > maxOctetsSSID {
		return fmt.Errorf("SSID invalide (%d octets, attendu 1 a %d)", n, maxOctetsSSID)
	}
	if n := len([]byte(motDePasse)); n < minOctetsMotPass || n > maxOctetsMotPass {
		return fmt.Errorf("mot de passe invalide (%d octets, attendu %d a %d)", n, minOctetsMotPass, maxOctetsMotPass)
	}
	macBytes, err := c.MACBytes()
	if err != nil {
		return fmt.Errorf("lecture du MAC : %w", err)
	}
	pwdHex, err := chiffrerMotDePasseWifi(macBytes, motDePasse, nil)
	if err != nil {
		return fmt.Errorf("chiffrement du mot de passe : %w", err)
	}
	contenu, err := contenuStaWifi(ssid, pwdHex)
	if err != nil {
		return fmt.Errorf("serialisation sta_wifi.json : %w", err)
	}
	if err := c.UploadFile("sta_wifi.json", contenu, nil); err != nil {
		return fmt.Errorf("televersement de sta_wifi.json : %w", err)
	}
	return nil
}

// SSIDWifiMaison lit le SSID du Wi-Fi domestique (STA) depose sur la boite
// (sta_wifi.json, op13). Le mot de passe (chiffre) n'est ni lu ni dechiffre.
// "" si la boite n'a pas de config Wi-Fi domestique (fichier absent ou vide).
func SSIDWifiMaison(contenu []byte) string {
	var l []entreeStaWifi
	if json.Unmarshal(contenu, &l) != nil {
		return ""
	}
	for _, e := range l {
		if e.SSID != "" {
			return e.SSID
		}
	}
	return ""
}
