package radiofrance

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/louis49/merlinus/internal/itunes"
)

var reUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// cleEpisodeRF derive la clef stable d'un episode RF du NOM DE FICHIER de son
// archive audio (sans dossier ni extension) : c'est le fichier reellement stocke
// sur le CDN (ex. ITEMA_22989809-2022M44415E0002-21, ou _RSCE-21-<hash>). Il est
// a la fois stable (identifiant d'archive, pas l'UUID interne volatile du
// __data.json) et UNIQUE par episode.
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

// serieEpisodes traite les podcasts "serie-" de Radio France (sans flux RSS).
// Les episodes sont pagines dans le __data.json de la page (SvelteKit) : le
// concept expose expressionCount (total) et expressions.next (offset base64 de
// la page suivante). On parcourt ?p=1, 2, 3... jusqu'a ce qu'une page n'apporte
// aucun nouvel episode. L'audio se resout ensuite via /transistor/aod/{id}.
func (c *Client) serieEpisodes(path string) ([]itunes.Episode, error) {
	base := "https://www.radiofrance.fr/" + strings.Trim(path, "/") + "/__data.json"
	var eps []epSerie
	vus := map[string]bool{}
	const maxPages = 60 // garde-fou : ~1200 episodes au plus
	for page := 1; page <= maxPages; page++ {
		url := base
		if page > 1 {
			url = base + "?p=" + strconv.Itoa(page)
		}
		raw, err := c.fetch(url)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break // une page en trop a echoue : on s'arrete sur ce qu'on a
		}
		avant := len(eps)
		eps = parserPageSerie(raw, eps, vus)
		if len(eps) == avant {
			break // page sans nouvel episode : fin de la pagination
		}
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("aucun episode dans %s", path)
	}

	// Resoudre l'audio de chaque episode via aod, en parallele borne.
	out := make([]itunes.Episode, len(eps))
	type res struct {
		i   int
		url string
	}
	ch := make(chan res, len(eps))
	sem := make(chan struct{}, 6)
	for i, e := range eps {
		go func(i int, id string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- res{i, c.aodMP3(id)}
		}(i, e.id)
	}
	for range eps {
		r := <-ch
		e := eps[r.i]
		img := ""
		if e.visual != "" {
			img = "https://www.radiofrance.fr/pikapi/images/" + e.visual + "/560x315?webp=true"
		}
		out[r.i] = itunes.Episode{
			ID: e.id, UUID: itunes.UUIDDeterministe(cleEpisodeRF(r.url, e.id)),
			Title: strings.TrimSpace(e.title), Desc: strings.TrimSpace(e.desc), Image: img,
			Duration: e.dur, Published: e.pub, AudioSrc: r.url, Playable: r.url != "",
		}
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
	// Ecarter ceux dont l'audio n'a pas pu etre resolu.
	filtre := out[:0]
	for _, e := range out {
		if e.Playable {
			filtre = append(filtre, e)
		}
	}
	return filtre, nil
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

// aodMP3 resout l'URL mp3 d'un episode via /transistor/aod/{id}.
func (c *Client) aodMP3(id string) string {
	raw, err := c.fetch("https://www.radiofrance.fr/transistor/aod/" + id)
	if err != nil {
		return ""
	}
	var d struct {
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return ""
	}
	var m4a string
	for _, s := range d.Sources {
		if strings.HasSuffix(s.URL, ".mp3") {
			return s.URL // mp3 prefere : format accepte par l'enceinte
		}
		if strings.HasSuffix(s.URL, ".m4a") {
			m4a = s.URL
		}
	}
	return m4a
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
