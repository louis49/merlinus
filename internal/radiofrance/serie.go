package radiofrance

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/louis49/merlinus/internal/itunes"
)

var reUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// cleEpisodeRF derive la clef d'un episode RF NOUVEAU du NOM DE FICHIER de son
// archive audio (sans dossier ni extension) : c'est le fichier reellement stocke
// sur le CDN (ex. ITEMA_22989809-2022M44415E0002-21, ou _RSCE-21-<hash>). Il est
// UNIQUE par episode, mais PAS stable : RF le change quand il republie le media
// (BUGS.md B6). L'uuid ainsi calcule est donc fige a la premiere vue dans le
// registre (registre.go), indexe par l'id d'expression de __data.json, lui
// stable. Le calcul n'est pas change pour ne modifier aucun uuid deja publie.
//
// Piege corrige : le seul numero ITEMA ne suffit PAS. Plusieurs episodes
// partagent un meme ITEMA_<n>, distingues par le suffixe ...E000N -> extraire le
// seul numero les faisait collisionner (23 episodes -> 6 UUID). On garde donc le
// nom de fichier complet.
func cleEpisodeRF(mp3URL, dataID string) string {
	u := mp3URL
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		u = u[i+1:]
	}
	u = strings.TrimSuffix(u, ".mp3")
	u = strings.TrimSuffix(u, ".m4a")
	if u != "" {
		return "rf:file:" + u
	}
	return "rf:aod:" + dataID // repli si l'audio n'a pas pu etre resolu
}

// serieEpisodes traite les podcasts "serie-" de Radio France, qui n'ont pas de
// flux RSS : leurs episodes sont decrits dans le __data.json de la page (format
// SvelteKit), et l'audio se resout via l'endpoint /transistor/aod/{id}.
//
// Cette voie est plus fragile que le RSS : elle depend du format interne du site.
type epSerie struct {
	id, title, desc, visual, pub string
	dur                          int
}

// serieEpisodes lit les episodes d'une serie de Radio France. Ils sont pagines
// dans le __data.json de la page (SvelteKit) : le concept expose
// expressionCount (total) et expressions.next (offset base64 de la page
// suivante). On parcourt ?p=1, 2, 3... jusqu'a ce qu'une page n'apporte aucun
// nouvel episode. L'audio et l'uuid viennent du registre (c.Reg) ; seuls les
// episodes inconnus, indisponibles a revoir ou a revalider passent par
// /transistor/aod/{id}, au plus paralleleAod a la fois. Rend aussi l'URL du
// flux RSS de la serie (champ rssFeed, vide si absent).
//
// Jamais de liste partielle (BUGS.md B4) : un refus, une erreur serveur ou
// reseau sur une page ou un aod fait echouer toute la serie. Seul un episode
// sans audio (aod 404 "No available source", comme le site l'ecarte) est omis.
func (c *Client) serieEpisodes(path string, paralleleAod int) ([]itunes.Episode, string, error) {
	chemin := strings.Trim(path, "/")
	base := rfBase + "/" + chemin + "/__data.json"
	var eps []epSerie
	vus := map[string]bool{}
	rss := ""
	const maxPages = 60 // garde-fou : ~1200 episodes au plus
	for page := 1; page <= maxPages; page++ {
		url := base
		if page > 1 {
			url = base + "?p=" + strconv.Itoa(page)
		}
		raw, st, err := c.requete(url)
		if err != nil {
			return nil, "", err
		}
		if st == http.StatusNotFound && page > 1 {
			break // page au-dela de la derniere
		}
		if st != http.StatusOK {
			return nil, "", fmt.Errorf("HTTP %d", st)
		}
		if page == 1 {
			rss = rssSerie(raw, chemin)
		}
		avant := len(eps)
		eps = parserPageSerie(raw, eps, vus)
		if len(eps) == avant {
			break // page sans nouvel episode : fin de la pagination
		}
	}
	if len(eps) == 0 {
		return nil, "", fmt.Errorf("aucun episode dans %s", chemin)
	}

	maintenant := time.Now()
	ids := make([]string, len(eps))
	for i, e := range eps {
		ids[i] = e.id
	}
	p := c.Reg.planifier(ids, maintenant)
	urls := make([]string, len(eps))
	uuids := make([]string, len(eps))
	for i, e := range p.connus {
		urls[i], uuids[i] = e.URL, e.UUID
	}

	// Resolutions aod, en parallele borne. Au premier echec bloquant, celles pas
	// encore lancees sont abandonnees.
	resURL := make([]string, len(eps))
	resErr := make([]error, len(eps))
	lance := make([]bool, len(eps))
	sem := make(chan struct{}, max(paralleleAod, 1))
	var abandon atomic.Bool
	var wg sync.WaitGroup
	for _, i := range p.aResoudre {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if abandon.Load() {
				return
			}
			lance[i] = true
			resURL[i], resErr[i] = c.aodMP3(eps[i].id)
			if resErr[i] != nil && !errors.Is(resErr[i], errIndisponible) {
				abandon.Store(true)
			}
		}(i)
	}
	wg.Wait()

	// Bilan : les resolutions reussies sont enregistrees meme si la serie
	// echoue (elles ne seront pas a refaire).
	var fatale error
	nouveaux, indispo := 0, 0
	for _, i := range p.aResoudre {
		e := eps[i]
		titre := strings.TrimSpace(e.title)
		switch err := resErr[i]; {
		case !lance[i]:
		case err == nil:
			if _, connu := p.connus[i]; !connu {
				nouveaux++
			}
			urls[i] = resURL[i]
			uuids[i] = itunes.UUIDDeterministe(cleEpisodeRF(resURL[i], e.id))
			if c.Reg != nil {
				uuids[i] = c.Reg.resolu(e.id, titre, resURL[i], uuids[i], maintenant)
			}
		case errors.Is(err, errIndisponible):
			urls[i], uuids[i] = "", ""
			indispo++
			if c.Reg != nil {
				c.Reg.indisponible(e.id, titre, maintenant)
			}
		case fatale == nil:
			fatale = err
		}
	}
	if c.Reg != nil {
		if err := c.Reg.Sauver(); err != nil {
			log.Printf("radiofrance : registre non sauvegarde (%v)", err)
		}
	}
	if len(p.aResoudre) > 0 || len(p.indispo) > 0 {
		log.Printf("radiofrance %s : %d episode(s), %d connu(s), %d aod (%d nouveau(x), %d revalidation(s)), %d sans audio",
			chemin, len(eps), len(p.connus), len(p.aResoudre), nouveaux, p.revalidions, indispo+len(p.indispo))
	}
	if fatale != nil {
		return nil, "", fatale
	}

	var out []itunes.Episode
	for i, e := range eps {
		if urls[i] == "" || uuids[i] == "" {
			continue // sans audio : ecarte, comme sur le site
		}
		img := ""
		if e.visual != "" {
			img = "https://www.radiofrance.fr/pikapi/images/" + e.visual + "/560x315?webp=true"
		}
		out = append(out, itunes.Episode{
			ID: e.id, UUID: uuids[i],
			Title: strings.TrimSpace(e.title), Desc: strings.TrimSpace(e.desc), Image: img,
			Duration: e.dur, Published: e.pub, AudioSrc: urls[i], Playable: true,
		})
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("aucun episode disponible dans %s", chemin)
	}
	// Ordre naturel : par titre, en comparant les nombres comme des nombres
	// ("Episode 2/12" avant "Episode 10/12", et non l'inverse). Repli sur la date
	// de publication quand les titres ne departagent pas.
	sort.SliceStable(out, func(i, j int) bool {
		if c := compareNaturel(out[i].Title, out[j].Title); c != 0 {
			return c < 0
		}
		return out[i].Published < out[j].Published
	})
	return out, rss, nil
}

// URLAudio resout a nouveau l'URL audio d'un episode connu (avant son
// telechargement : l'URL du registre a pu perimer si RF a republie le media).
// Met le registre a jour. errIndisponible si l'episode n'a plus d'audio.
func (c *Client) URLAudio(id string) (string, error) {
	u, err := c.aodMP3(id)
	if c.Reg != nil {
		t := time.Now()
		switch {
		case err == nil:
			if e, ok := c.Reg.Entree(id); ok && e.UUID != "" {
				c.Reg.resolu(id, "", u, e.UUID, t)
			}
		case errors.Is(err, errIndisponible):
			if _, ok := c.Reg.Entree(id); ok {
				c.Reg.indisponible(id, "", t)
			}
		}
		if e := c.Reg.Sauver(); e != nil {
			log.Printf("radiofrance : registre non sauvegarde (%v)", e)
		}
	}
	return u, err
}

// EstIndisponible indique si err signale un episode sans audio.
func EstIndisponible(err error) bool { return errors.Is(err, errIndisponible) }

// rssSerie extrait de la premiere page __data.json l'URL du flux RSS de la
// serie (concept dont le chemin est chemin), vide si absente.
func rssSerie(raw, chemin string) string {
	var doc struct {
		Nodes []struct {
			Data json.RawMessage `json:"data"`
		} `json:"nodes"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return ""
	}
	rss := ""
	for _, n := range doc.Nodes {
		if rss != "" || len(n.Data) == 0 || string(n.Data) == "null" {
			continue
		}
		var arr []any
		if json.Unmarshal(n.Data, &arr) != nil {
			continue
		}
		parcourir(resoudre(arr, 0, map[int]any{}, map[int]bool{}), func(o map[string]any) {
			if rss != "" {
				return
			}
			if tn, _ := o["__typename"].(string); tn != "Concept" {
				return
			}
			href, _ := o["href"].(string)
			chem, _ := o["path"].(string)
			if href != "/"+chemin && chem != chemin {
				return
			}
			if f, _ := o["rssFeed"].(string); strings.HasPrefix(f, "https://") || strings.HasPrefix(f, "http://") {
				rss = f
			}
		})
	}
	return rss
}

// compareNaturel compare deux chaines en traitant chaque suite de chiffres comme
// un nombre entier ("episode 2" < "episode 10"). Insensible a la casse. Renvoie
// -1, 0 ou 1.
func compareNaturel(a, b string) int {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	chiffre := func(r rune) bool { return r >= '0' && r <= '9' }
	for i < len(ra) && j < len(rb) {
		if chiffre(ra[i]) && chiffre(rb[j]) {
			di, dj := i, j
			for i < len(ra) && chiffre(ra[i]) {
				i++
			}
			for j < len(rb) && chiffre(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[di:i]), "0")
			nb := strings.TrimLeft(string(rb[dj:j]), "0")
			if len(na) != len(nb) { // nombre le plus court = plus petit
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue // nombres egaux : on poursuit
		}
		if ra[i] != rb[j] {
			if ra[i] < rb[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case i < len(ra):
		return 1
	case j < len(rb):
		return -1
	default:
		return 0
	}
}

// parserPageSerie extrait les episodes d'une page __data.json et les ajoute a
// eps (dedup par id via vus). Marche le graphe devalue et retient les objets
// type == "episode".
func parserPageSerie(raw string, eps []epSerie, vus map[string]bool) []epSerie {
	var doc struct {
		Nodes []struct {
			Data json.RawMessage `json:"data"`
		} `json:"nodes"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return eps
	}
	for _, n := range doc.Nodes {
		if len(n.Data) == 0 || string(n.Data) == "null" {
			continue
		}
		var arr []any
		if json.Unmarshal(n.Data, &arr) != nil {
			continue
		}
		root := resoudre(arr, 0, map[int]any{}, map[int]bool{})
		parcourir(root, func(o map[string]any) {
			if s, _ := o["type"].(string); s != "episode" {
				return
			}
			id, _ := o["id"].(string)
			if id == "" || !reUUID.MatchString(id) || vus[id] {
				return
			}
			vus[id] = true
			e := epSerie{id: id}
			if tp, ok := o["titleProps"].(map[string]any); ok {
				t, _ := tp["title"].(string)
				e.title = html.UnescapeString(t)
				// titleProps.text porte la description de l'episode (le titre est
				// dans .title). RF ne fournit pas de champ "description" distinct.
				tx, _ := tp["text"].(string)
				e.desc = html.UnescapeString(tx)
			}
			if man, ok := o["manifestation"].(map[string]any); ok {
				if d, ok := man["duration"].(float64); ok {
					e.dur = int(d)
				}
			}
			if estPromoRF(e.title, e.desc, e.dur) {
				return // bande-annonce / encart promo : pas une histoire a retenir
			}
			if v, ok := o["visual"].(map[string]any); ok {
				e.visual, _ = v["id"].(string)
			}
			if p, ok := o["publishedDate"].(string); ok {
				e.pub = p
			}
			eps = append(eps, e)
		})
	}
	return eps
}

// estPromoRF ecarte les pseudo-episodes promotionnels de Radio France, qui ne
// sont pas toujours marques type != "episode" (le cas Oli l'etait, pas Zinstrus).
// Deux signaux : des mots-cles explicites (bande-annonce, teaser, encart "appli
// Radio France" filtre par la V3), et le motif "tres court + presentation"
// (ex. "Decouvrez ... le nouveau podcast", ~50 s), corrobore par la duree.
func estPromoRF(titre, desc string, dur int) bool {
	s := strings.ToLower(titre + " " + desc)
	for _, m := range []string{
		"bande annonce", "bande-annonce", "teaser", "trailer",
		"retrouvez tous les épisodes", "retrouvez tous les episodes",
		"sur l'appli radio france", "sur l’appli radio france",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	if dur > 0 && dur < 120 &&
		(strings.Contains(s, "découvrez") || strings.Contains(s, "decouvrez") ||
			strings.Contains(s, "nouveau podcast")) {
		return true
	}
	return false
}

// errIndisponible : l'episode n'a pas d'audio ecoutable (aod 404 "No available
// source", ou aucune source mp3/m4a). Il est ecarte, comme sur le site.
var errIndisponible = errors.New("audio indisponible")

// aodMP3 resout l'URL mp3 (a defaut m4a) d'un episode via /transistor/aod/{id}.
// Erreur errIndisponible si l'episode n'a pas d'audio ; toute autre erreur
// (refus, serveur, reseau, reponse illisible) est bloquante pour la serie.
func (c *Client) aodMP3(id string) (string, error) {
	raw, st, err := c.requete("https://www.radiofrance.fr/transistor/aod/" + id)
	if err != nil {
		return "", err
	}
	if st == http.StatusNotFound {
		return "", errIndisponible
	}
	if st != http.StatusOK {
		return "", fmt.Errorf("aod %s : HTTP %d", id, st)
	}
	var d struct {
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return "", fmt.Errorf("aod %s : reponse illisible", id)
	}
	var m4a string
	for _, s := range d.Sources {
		if strings.HasSuffix(s.URL, ".mp3") {
			return s.URL, nil // mp3 prefere : format accepte par l'enceinte
		}
		if strings.HasSuffix(s.URL, ".m4a") {
			m4a = s.URL
		}
	}
	if m4a == "" {
		return "", errIndisponible
	}
	return m4a, nil
}

// resoudre reconstitue le graphe devalue de SvelteKit : les valeurs des champs
// et des tableaux sont des index pointant dans le tableau plat.
func resoudre(arr []any, i int, memo map[int]any, seen map[int]bool) any {
	if i < 0 || i >= len(arr) || seen[i] {
		return nil
	}
	if v, ok := memo[i]; ok {
		return v
	}
	seen[i] = true
	defer delete(seen, i)

	switch v := arr[i].(type) {
	case []any:
		out := make([]any, len(v))
		for k, x := range v {
			out[k] = resoudreVal(arr, x, memo, seen)
		}
		memo[i] = out
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = resoudreVal(arr, x, memo, seen)
		}
		memo[i] = out
		return out
	default:
		memo[i] = v
		return v
	}
}

// resoudreVal resout une valeur : un nombre est un index, tout le reste est
// litteral.
func resoudreVal(arr []any, x any, memo map[int]any, seen map[int]bool) any {
	if f, ok := x.(float64); ok {
		return resoudre(arr, int(f), memo, seen)
	}
	return x
}

// parcourir applique fn a chaque objet du graphe.
func parcourir(o any, fn func(map[string]any)) {
	switch v := o.(type) {
	case map[string]any:
		fn(v)
		for _, x := range v {
			parcourir(x, fn)
		}
	case []any:
		for _, x := range v {
			parcourir(x, fn)
		}
	}
}
