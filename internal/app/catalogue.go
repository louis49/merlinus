package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/louis49/merlinus/internal/store"
)

// dirCacheCatalogue : dossier PURGEABLE des images du catalogue (resultats
// iTunes/Radio France, vignettes de flux RSS). Re-telechargeables : aucun risque
// a les supprimer.
const dirCacheCatalogue = "cache/catalogue"

// cheminCacheCatalogue construit le chemin de cache d'une image catalogue. La
// cle est le sha256 de l'URL distante (sans extension) : stable, insensible aux
// caracteres speciaux de l'URL, pas de collision pratique.
func cheminCacheCatalogue(u string) string {
	h := sha256.Sum256([]byte(u))
	return dirCacheCatalogue + "/" + hex.EncodeToString(h[:])
}

// uaNavCatalogue : User-Agent navigateur (certains CDN refusent un UA vide).
const uaNavCatalogue = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36"

// imageCatalogue sert une image du catalogue depuis un cache disque. L'URL
// distante est passee en query (?u=...). Au premier acces, on telecharge et on
// ecrit sous cache/catalogue ; ensuite on sert depuis le disque. Reservee aux
// schemes http/https : l'app est locale et le seul client de ce handler est son
// propre webview, mais on refuse tout le reste par precaution (file://, data:).
func imageCatalogue(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if raw == "" {
		http.NotFound(w, r)
		return
	}
	pu, err := url.Parse(raw)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		http.NotFound(w, r)
		return
	}
	rel := cheminCacheCatalogue(raw)
	data, err := store.Load(rel)
	if err != nil || !estImage(data) {
		data, err = telechargerImageCatalogue(raw)
		if err != nil || !estImage(data) {
			http.NotFound(w, r) // jamais de contenu arbitraire en cache ni servi
			return
		}
		_ = store.Save(rel, data) // best-effort : un echec de cache ne bloque pas l'affichage
	}
	w.Header().Set("Content-Type", typeImage(data))
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(data)
}

// telechargerImageCatalogue recupere une image distante (plafond 20 Mo, une
// image de catalogue est petite).
func telechargerImageCatalogue(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uaNavCatalogue)
	resp, err := clientCatalogue.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// clientCatalogue : client HTTP de /catimg, qui ne joint que des adresses
// publiques. Le controle porte sur l'IP RESOLUE au moment de la connexion : un nom
// pointant vers le reseau local, ou une redirection vers une IP interne, est
// refuse. Les CDN du catalogue (iTunes, Radio France) sont publics.
var clientCatalogue = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: refuserAdresseLocale}).DialContext,
	},
}

// refuserAdresseLocale (net.Dialer.Control) : refuse loopback, reseaux prives
// (RFC 1918, ULA), link-local, multicast et adresse non specifiee.
func refuserAdresseLocale(_, adresse string, _ syscall.RawConn) error {
	hote, _, err := net.SplitHostPort(adresse)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(hote)
	if err != nil {
		return err
	}
	if !adressePublique(ip) {
		return fmt.Errorf("adresse non publique refusee : %s", ip)
	}
	return nil
}

func adressePublique(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() &&
		!ip.IsMulticast()
}

// estImage : octets reconnus comme une image (WebP detecte a la main, voir
// typeImage). Seules les images sont mises en cache et servies par /catimg.
func estImage(b []byte) bool {
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return true
	}
	return len(b) > 0 && strings.HasPrefix(http.DetectContentType(b), "image/")
}

// typeImage devine le Content-Type par les octets. http.DetectContentType ne
// connait pas le WebP (servi par Radio France) : on le detecte a la main. Repli
// sur image/jpeg (le webview rend l'image d'apres ses octets de toute facon).
func typeImage(b []byte) string {
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp"
	}
	if ct := http.DetectContentType(b); strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "image/jpeg"
}
