// Package webradio recupere le catalogue des webradios Merlin depuis le backend
// Bayard. La boite resout le flux cote cloud a partir de l'UUID du noeud ; on ne
// pousse donc jamais d'URL, seulement {uuid, titre} + le logo. Voir
// docs/ANALYSE-RADIOS.md.
//
// Auth : en-tete X-Mac-Address = MAC STATION DE BASE de la boite (pas la SoftAP
// renvoyee par op30 ; base = SoftAP - 1 sur le dernier octet). Aucun login.
package webradio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const urlCatalogue = "https://api.hello-merlin.com/api/v1/webradios/speaker/all"

// Radio : une station du catalogue. StreamURL est informatif (jamais pousse).
type Radio struct {
	UUID      string `json:"uuid"`
	Titre     string `json:"titre"`
	ImageURL  string `json:"imageUrl"`
	StreamURL string `json:"streamUrl"`
}

type rawRadio struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Stream   string `json:"stream_url"`
	ImageURL string `json:"image_url"`
}

// MacBase derive le MAC station de base a partir du MAC SoftAP (op30) en
// decrementant le dernier octet. Renvoie la forme "XX:XX:XX:XX:XX:XX" majuscule.
func MacBase(macSoftAP string) (string, error) {
	parts := strings.Split(strings.TrimSpace(macSoftAP), ":")
	if len(parts) != 6 {
		return "", fmt.Errorf("MAC mal forme : %q", macSoftAP)
	}
	var n uint64 // MAC assemble en entier 48 bits
	for _, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return "", fmt.Errorf("octet MAC invalide %q : %w", p, err)
		}
		n = n<<8 | v
	}
	n = (n - 1) & 0xFFFFFFFFFFFF // SoftAP = base + 1 ; retenue propagee sur 48 bits
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		byte(n>>40), byte(n>>32), byte(n>>24), byte(n>>16), byte(n>>8), byte(n)), nil
}

// Catalogue interroge le backend et renvoie les stations disponibles pour cette
// boite (macSoftAP = MAC renvoye par op30).
func Catalogue(macSoftAP string) ([]Radio, error) {
	base, err := MacBase(macSoftAP)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, urlCatalogue, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Mac-Address", base)
	req.Header.Set("Accept", "application/json")

	cl := &http.Client{Timeout: 20 * time.Second}
	var resp *http.Response
	for essai := 0; essai < 3; essai++ {
		resp, err = cl.Do(req)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(essai+1) * 500 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("catalogue webradios injoignable : %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalogue webradios : HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var brut []rawRadio
	if err := json.Unmarshal(body, &brut); err != nil {
		return nil, fmt.Errorf("catalogue webradios illisible : %w", err)
	}
	radios := make([]Radio, 0, len(brut))
	for _, r := range brut {
		if r.ID == "" || r.Title == "" {
			continue
		}
		radios = append(radios, Radio{UUID: r.ID, Titre: r.Title, ImageURL: r.ImageURL, StreamURL: r.Stream})
	}
	return radios, nil
}
