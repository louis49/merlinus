package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/louis49/merlinus/internal/httpx"
)

// Version de l'application et detection d'une version plus recente publiee sur
// GitHub.
//
// Source de la version : wails.json (info.productVersion), embarque par main.go.
// Le workflow de release y ecrit le numero du tag avant la compilation (le meme
// champ alimente Info.plist et les proprietes Windows). Hors release le champ
// est absent : version "dev", sans verification. MERLINUS_VERSION la remplace
// (simulation d'une ancienne version en developpement).

const (
	versionDev  = "dev"
	depotGitHub = "louis49/merlinus"
	urlDerniere = "https://api.github.com/repos/" + depotGitHub + "/releases/latest"
	urlReleases = "https://github.com/" + depotGitHub + "/releases/tag/"
)

var reVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// versionDepuisConfig lit info.productVersion du wails.json embarque.
func versionDepuisConfig(wailsJSON []byte) string {
	if v := os.Getenv("MERLINUS_VERSION"); v != "" {
		return v
	}
	var cfg struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if json.Unmarshal(wailsJSON, &cfg) != nil || cfg.Info.ProductVersion == "" {
		return versionDev
	}
	return cfg.Info.ProductVersion
}

// MiseAJour est le resultat de la verification, pour l'IHM.
type MiseAJour struct {
	Courante   string `json:"courante"`
	Derniere   string `json:"derniere,omitempty"`
	URL        string `json:"url,omitempty"`
	Disponible bool   `json:"disponible"`
	Verifiee   bool   `json:"verifiee"` // false : pas de reponse exploitable (hors ligne, AP de la boite...)
}

// Version renvoie la version de l'application ("dev" hors release).
func (a *App) Version() string { return a.version }

// VerifierMiseAJour interroge GitHub (derniere release publiee, hors brouillons
// et pre-versions) et compare au numero courant. Jamais d'erreur remontee :
// l'IHM n'affiche rien si la verification echoue.
func (a *App) VerifierMiseAJour() MiseAJour {
	r := MiseAJour{Courante: a.version}
	if _, ok := parserVersion(a.version); !ok {
		return r // version de developpement : rien a comparer
	}
	tag, err := derniereRelease(clientGitHub, urlDerniere, a.version)
	if err != nil {
		return r
	}
	r.Verifiee = true
	r.Derniere = tag
	r.URL = urlReleases + "v" + tag
	r.Disponible = plusRecente(tag, a.version)
	return r
}

// clientGitHub : une requete par verification, sans reessai (on retentera plus tard).
var clientGitHub = &http.Client{Timeout: 10 * time.Second}

// derniereRelease renvoie le numero (sans "v") de la derniere release.
func derniereRelease(c *http.Client, u, courante string) (string, error) {
	b, st, err := httpx.Get(c, u, httpx.Options{UA: "Merlinus/" + courante, Plafond: 1 << 20})
	if err != nil {
		return "", err
	}
	if st != http.StatusOK {
		return "", fmt.Errorf("releases GitHub : HTTP %d", st)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &rel); err != nil {
		return "", err
	}
	v, ok := parserVersion(rel.Tag)
	if !ok {
		return "", fmt.Errorf("tag de release hors format X.Y.Z : %q", rel.Tag)
	}
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2]), nil
}

// parserVersion lit "X.Y.Z" ou "vX.Y.Z".
func parserVersion(s string) ([3]int, bool) {
	m := reVersion.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

// plusRecente : a strictement plus recente que b (toutes deux au format X.Y.Z).
func plusRecente(a, b string) bool {
	va, oka := parserVersion(a)
	vb, okb := parserVersion(b)
	if !oka || !okb {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}
