// Package radiofrance recupere les podcasts jeunesse de Radio France (France
// Inter, France Culture...) et leurs episodes.
//
// Radio France n'expose pas d'API publique sans cle : ce paquet s'appuie donc
// sur des pages web publiques (listing jeunesse et page de chaque podcast). Cette
// extraction est FRAGILE par nature : elle depend de la structure HTML du site
// et peut casser si Radio France la modifie. Le format RSS, lui, est stable.
package radiofrance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/louis49/merlinus/internal/itunes"

	"github.com/louis49/merlinus/internal/httpx"
)

const listingURL = "https://www.radiofrance.fr/podcasts/enfants/__data.json"

type Client struct {
	HTTP *http.Client

	hashMu  sync.Mutex // protège le cache du hash de la remote function
	hash    string
	hashExp time.Time
}

func New() *Client { return &Client{HTTP: &http.Client{Timeout: 25 * time.Second}} }

const rfBase = "https://www.radiofrance.fr"

var reHashRecherche = regexp.MustCompile(`"([a-z0-9]{5,10})/getSearchItems"`)

var (
	reFeed = regexp.MustCompile(`https?://radiofrance-podcast\.net/[a-z0-9]+/(?:podcast_[0-9a-f-]+|rss_\d+)\.xml`)
	reHref = regexp.MustCompile(`^/[a-z]+/podcasts/[a-z0-9-]+$`)
)

// backoffsRF : delais avant chaque reessai. La longueur fixe le nombre de
// reessais (2 ici, soit 3 tentatives au total).
var backoffsRF = []time.Duration{200 * time.Millisecond, 500 * time.Millisecond}

// requete effectue un GET avec reessais sur echec transitoire (erreur reseau
// ou statut 5xx/429). Un 4xx est deterministe : on rend immediatement et
// l'appelant decide. Le statut rendu vaut 0 en cas d'erreur reseau.
func (c *Client) requete(u string) (body string, status int, err error) {
	b, st, err := httpx.Get(c.HTTP, u, httpx.Options{UA: "Mozilla/5.0 Merlinus/0.1", Plafond: 8 << 20, Delais: backoffsRF})
	return string(b), st, err
}

func (c *Client) fetch(u string) (string, error) {
	body, status, err := c.requete(u)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", status)
	}
	return body, nil
}

// List renvoie les podcasts jeunesse depuis le __data.json de la page enfants
// (donnees SvelteKit). L'identifiant est le chemin du podcast (ex.
// "franceinter/podcasts/bestioles"). Les series ("serie") sont incluses : leurs
// episodes se resolvent via aod. Les regroupements ("selection") sont ecartes.
func (c *Client) List() ([]itunes.Podcast, error) {
	raw, err := c.fetch(listingURL)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Nodes []struct {
			Data json.RawMessage `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	vus := map[string]bool{}
	var out []itunes.Podcast
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
			if tn, _ := o["__typename"].(string); tn != "Concept" {
				return
			}
			typ, _ := o["type"].(string)
			if typ == "selection" { // regroupement, pas un podcast ecoutable
				return
			}
			href, _ := o["href"].(string)
			titre, _ := o["title"].(string)
			if href == "" || titre == "" || !reHref.MatchString(href) || vus[href] {
				return
			}
			vus[href] = true
			ed := editeur(o["brand"])
			out = append(out, itunes.Podcast{
				ID: strings.TrimPrefix(href, "/"), UUID: itunes.UUIDDeterministe("rf:" + strings.TrimPrefix(href, "/")),
				Title:  html.UnescapeString(titre),
				Artist: ed, Category: ed, Image: imageCarte(o),
			})
		})
	}
	return out, nil
}

// editeur traduit le code de station Radio France en libelle affichable.
func editeur(v any) string {
	code, _ := v.(string)
	switch code {
	case "franceinter":
		return "France Inter"
	case "francemusique":
		return "France Musique"
	case "franceculture":
		return "France Culture"
	case "francebleu":
		return "France Bleu"
	case "franceinfo":
		return "France Info"
	case "mouv":
		return "Mouv'"
	case "fip":
		return "FIP"
	case "savoirsplus":
		return "Radio France - Savoirs"
	default:
		return "Radio France"
	}
}

// imageCarte extrait l'illustration carree d'une carte podcast, avec repli sur
// les autres formats.
func imageCarte(o map[string]any) string {
	for _, k := range []string{"square", "visual", "portrait", "poster"} {
		if v, ok := o[k].(map[string]any); ok {
			if id, ok := v["id"].(string); ok && id != "" {
				return "https://www.radiofrance.fr/pikapi/images/" + id + "/400x400?webp=true"
			}
		}
	}
	return ""
}

// Feed resout la serie complete (description + image + episodes). La description
// vient du <channel> du flux RSS quand il existe ; sans flux (serie resolue via
// __data.json), il n'y a pas de description de channel.
func (c *Client) Feed(path string) (itunes.Feed, error) {
	page, err := c.fetch("https://www.radiofrance.fr/" + strings.Trim(path, "/"))
	if err != nil {
		return itunes.Feed{}, err
	}
	feeds := reFeed.FindAllString(page, -1)

	// Episodes : toujours via la voie paginee (__data.json + aod). Elle est plus
	// complete que le flux RSS de RF, qui plafonne (~170 episodes meme quand la
	// serie en compte davantage), et elle privilegie le mp3 (evite le remux m4a).
	eps, errS := c.serieEpisodes(path)

	// Choix du flux RSS (forme https podcast_<uuid>.xml de preference) : sert la
	// description/image de la serie, et sert de repli si la pagination echoue.
	feed := ""
	if len(feeds) > 0 {
		feed = feeds[0]
		for _, f := range feeds {
			if strings.HasPrefix(f, "https://") && strings.Contains(f, "/podcast_") {
				feed = f
				break
			}
		}
	}

	if errS != nil || len(eps) == 0 {
		// Pagination indisponible : repli sur le flux RSS complet s'il existe.
		if feed != "" {
			f, err := itunes.ParseFeedComplet(c.HTTP, feed)
			f.Description = tronquerPromo(f.Description)
			return f, err
		}
		return itunes.Feed{}, errS
	}

	f := itunes.Feed{Episodes: eps}
	if feed != "" {
		if rss, err := itunes.ParseFeedComplet(c.HTTP, feed); err == nil {
			f.Description = tronquerPromo(rss.Description)
			f.Image = rss.Image
		}
	}
	return f, nil
}

// tronquerPromo retire l'encart promotionnel que Radio France ajoute en fin de
// description ("Vous aimez ce podcast ? Pour écouter tous les épisodes sans
// limite, rendez-vous sur Radio France"). Le marqueur en tête est constant.
func tronquerPromo(desc string) string {
	if i := strings.Index(desc, "Vous aimez ce podcast"); i >= 0 {
		desc = desc[:i]
	}
	return strings.TrimSpace(desc)
}

// searchItemsHash extrait le hash de la "remote function" SvelteKit getSearchItems
// du build courant du site RF. Ce hash change à chaque déploiement : on le
// (re)découvre en HTTP simple (page recherche -> script app -> scan des chunks
// pour le littéral "<hash>/getSearchItems"), et on le met en cache 6 h. Pas de
// navigateur : conforme au binaire unique.
func (c *Client) searchItemsHash() (string, error) {
	c.hashMu.Lock()
	if c.hash != "" && time.Now().Before(c.hashExp) {
		h := c.hash
		c.hashMu.Unlock()
		return h, nil
	}
	c.hashMu.Unlock()

	html, err := c.fetch(rfBase + "/recherche?q=a&results=true")
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`/_app/immutable/entry/app\.[A-Za-z0-9]+\.js`).FindString(html)
	if m == "" {
		return "", fmt.Errorf("script app introuvable")
	}
	appjs, err := c.fetch(rfBase + m)
	if err != nil {
		return "", err
	}
	// Noms de chunks référencés par l'app -> URLs à scanner.
	noms := map[string]bool{}
	for _, n := range regexp.MustCompile(`[A-Za-z0-9_-]{6,12}\.js`).FindAllString(appjs, -1) {
		noms[n] = true
	}
	var urls []string
	for n := range noms {
		urls = append(urls, rfBase+"/_app/immutable/chunks/"+n)
	}
	h := c.scanHash(urls)
	if h == "" {
		return "", fmt.Errorf("hash getSearchItems introuvable")
	}
	c.hashMu.Lock()
	c.hash, c.hashExp = h, time.Now().Add(6*time.Hour)
	c.hashMu.Unlock()
	return h, nil
}

// scanHash télécharge les chunks en parallèle (borné) et renvoie le 1er hash
// trouvé (littéral "<hash>/getSearchItems").
func (c *Client) scanHash(urls []string) string {
	var mu sync.Mutex
	found := ""
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, u := range urls {
		mu.Lock()
		fini := found != ""
		mu.Unlock()
		if fini {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			mu.Lock()
			d := found != ""
			mu.Unlock()
			if d {
				return
			}
			js, err := c.fetch(u)
			if err != nil {
				return
			}
			if sm := reHashRecherche.FindStringSubmatch(js); sm != nil {
				mu.Lock()
				if found == "" {
					found = sm[1]
				}
				mu.Unlock()
			}
		}(u)
	}
	wg.Wait()
	return found
}

// payloadRecherche construit l'argument (devalue SvelteKit, base64) : seule la
// valeur du terme change dans une structure fixe.
func payloadRecherche(term string) string {
	tb, _ := json.Marshal(term)
	j := `[["__skrao",1],{"durations":2,"entities":3,"globalAggregations":4,"limitConcepts":5,"sort":-1,"stations":6,"term":7,"years":8},[],[],false,6,[],` + string(tb) + `,[]]`
	return base64.StdEncoding.EncodeToString([]byte(j))
}

// Search interroge la recherche plein-catalogue de Radio France (au-delà de la
// page enfants). Réutilise le résolveur devalue de List. Un hash périmé (404)
// déclenche une ré-extraction unique.
func (c *Client) Search(term string) ([]itunes.Podcast, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return []itunes.Podcast{}, nil
	}
	hash, err := c.searchItemsHash()
	if err != nil {
		return nil, err
	}
	appel := func(h string) (string, int, error) {
		// Payload en base64 STANDARD : un "+" non echappe serait relu comme une
		// espace par le site, qui rejette alors le JSON (verifie en reel, ex. "¾conte").
		return c.requete(rfBase + "/_app/remote/" + h + "/getSearchItems?payload=" + url.QueryEscape(payloadRecherche(term)))
	}
	body, code, err := appel(hash)
	if code == 404 { // hash périmé : ré-extraire une fois
		c.hashMu.Lock()
		c.hash = ""
		c.hashMu.Unlock()
		if hash, err = c.searchItemsHash(); err == nil {
			body, code, err = appel(hash)
		}
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("recherche RF : HTTP %d", code)
	}
	var env struct {
		Result string `json:"result"`
	}
	if json.Unmarshal([]byte(body), &env) != nil || env.Result == "" {
		return nil, fmt.Errorf("réponse de recherche illisible")
	}
	var arr []any
	if json.Unmarshal([]byte(env.Result), &arr) != nil {
		return nil, fmt.Errorf("réponse de recherche illisible")
	}
	root := resoudre(arr, 0, map[int]any{}, map[int]bool{})
	vus := map[string]bool{}
	out := []itunes.Podcast{}
	parcourir(root, func(o map[string]any) {
		if tn, _ := o["__typename"].(string); tn != "Concept" {
			return
		}
		href, _ := o["href"].(string)
		if href == "" || !reHref.MatchString(href) || vus[href] {
			return
		}
		if strings.Contains(href, "/podcasts/selection-") {
			return // groupement de podcasts, pas un podcast écoutable
		}
		titre := texteRecherche(o["text"])
		if titre == "" {
			return
		}
		vus[href] = true
		ed := editeur(o["brand"])
		out = append(out, itunes.Podcast{
			ID: strings.TrimPrefix(href, "/"), UUID: itunes.UUIDDeterministe("rf:" + strings.TrimPrefix(href, "/")),
			Title:  titre,
			Artist: ed, Category: ed, Image: imageCarte(o),
		})
	})
	return out, nil
}

// texteRecherche nettoie le libellé d'un résultat (le champ text porte le titre,
// parfois avec un surlignage <em> et des espaces insécables).
func texteRecherche(v any) string {
	s, _ := v.(string)
	var b strings.Builder
	dansBalise := false
	for _, r := range s {
		switch {
		case r == '<':
			dansBalise = true
		case r == '>':
			dansBalise = false
		case !dansBalise:
			b.WriteRune(r)
		}
	}
	// strings.Fields découpe sur toute espace Unicode (dont l'insécable) et
	// supprime les doublons laissés par le retrait des balises de surlignage.
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}
