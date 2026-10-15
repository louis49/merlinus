// Package itunes interroge l'API publique iTunes/Apple Podcasts : classement
// enfants, recherche, et resolution des episodes via le flux RSS du podcast.
// Aucune authentification. Cible le genre "Enfants et parents" (1305).
package itunes

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/httpx"
)

// UUIDDeterministe derive un uuid (v5-like) stable d'une clef arbitraire.
// Meme clef -> meme uuid : reference pérenne independante des ids volatils des
// sources. Cf. le schema de cles (itunes:collection / itunes:guid / rf: / rf:itema).
func UUIDDeterministe(clef string) string {
	h := sha1.Sum([]byte(clef))
	h[6] = (h[6] & 0x0f) | 0x50
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

const (
	genreEnfants = "1305"
	pays         = "FR"
)

type Client struct{ HTTP *http.Client }

func New() *Client { return &Client{HTTP: &http.Client{Timeout: 25 * time.Second}} }

// Podcast est une emission. Sert de "serie" a l'affichage.
type Podcast struct {
	ID       string `json:"id"`
	UUID     string `json:"uuid"` // reference stable de boite (deterministe)
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Image    string `json:"image"`
	Category string `json:"category"`
	FeedURL  string `json:"-"`
	Explicit bool   `json:"explicit"`
	Count    int    `json:"nbEpisode"`
	// EpisodeMatch : renseigne quand la collection est remontee parce qu'un de ses
	// EPISODES correspond au terme recherche (cf. RechercheParEpisode). Sert a
	// afficher "contient : <titre>". Vide pour une correspondance de serie.
	EpisodeMatch string `json:"episodeMatch,omitempty"`
}

// Episode est un episode de podcast. Sert d'"histoire" a l'affichage.
type Episode struct {
	ID        string `json:"id"`
	UUID      string `json:"uuid"` // reference stable de boite (deterministe)
	Title     string `json:"title"`
	Image     string `json:"image"`
	Desc      string `json:"description"`
	Duration  int    `json:"duration"` // secondes
	Published string `json:"published"`
	AudioSrc  string `json:"audioSrc"` // URL de l'enclosure, jouee directement
	Playable  bool   `json:"playable"` // audio resolu (sert de filtre cote Radio France)
}

// backoffsIT : delais avant chaque reessai (2 reessais, 3 tentatives au total).
var backoffsIT = []time.Duration{200 * time.Millisecond, 500 * time.Millisecond}

// requete effectue un GET avec reessais sur echec transitoire (erreur reseau
// ou statut 5xx/429). Un 4xx est rendu immediatement (deterministe).
func (c *Client) requete(u string) (body []byte, status int, err error) {
	return httpx.Get(c.HTTP, u, httpx.Options{UA: "Merlinus/0.1", Plafond: 16 << 20, Delais: backoffsIT})
}

func (c *Client) get(u string, out any) error {
	body, status, err := c.requete(u)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}
	return json.Unmarshal(body, out)
}

// Chart renvoie le classement des meilleurs podcasts enfants (jusqu'a 200).
func (c *Client) Chart() ([]Podcast, error) {
	u := fmt.Sprintf("https://itunes.apple.com/%s/rss/toppodcasts/genre=%s/limit=200/json",
		strings.ToLower(pays), genreEnfants)
	var d struct {
		Feed struct {
			Entry []struct {
				Name   label       `json:"im:name"`
				Artist label       `json:"im:artist"`
				Images []label     `json:"im:image"`
				ID     idAttr      `json:"id"`
				Cat    categoryTag `json:"category"`
			} `json:"entry"`
		} `json:"feed"`
	}
	if err := c.get(u, &d); err != nil {
		return nil, err
	}
	out := make([]Podcast, 0, len(d.Feed.Entry))
	ids := make([]string, 0, len(d.Feed.Entry))
	for _, e := range d.Feed.Entry {
		img := ""
		if len(e.Images) > 0 {
			img = e.Images[len(e.Images)-1].Label // la plus grande
		}
		out = append(out, Podcast{
			ID: e.ID.Attributes.ID, UUID: UUIDDeterministe("itunes:collection:" + e.ID.Attributes.ID),
			Title: e.Name.Label, Artist: e.Artist.Label,
			Image: img, Category: e.Cat.Attributes.Label,
		})
		ids = append(ids, e.ID.Attributes.ID)
	}
	// Ne garder que les podcasts qui exposent un flux RSS : les autres (podcasts
	// Radio France exclusifs a Apple) ne menent a rien ici et sont couverts par
	// l'onglet Radio France.
	infos := c.lookupFeeds(ids)
	filtre := out[:0]
	for _, p := range out {
		if info := infos[p.ID]; info.feed != "" {
			p.FeedURL = info.feed
			p.Count = info.count
			filtre = append(filtre, p)
		}
	}
	return filtre, nil
}

type feedInfo struct {
	feed  string
	count int
}

// lookupFeeds resout le feedUrl de plusieurs podcasts par lots (l'endpoint
// lookup accepte plusieurs ids separes par des virgules).
func (c *Client) lookupFeeds(ids []string) map[string]feedInfo {
	res := map[string]feedInfo{}
	for i := 0; i < len(ids); i += 100 {
		fin := i + 100
		if fin > len(ids) {
			fin = len(ids)
		}
		var d struct {
			Results []struct {
				ID    int    `json:"collectionId"`
				Feed  string `json:"feedUrl"`
				Count int    `json:"trackCount"`
			} `json:"results"`
		}
		u := "https://itunes.apple.com/lookup?id=" + strings.Join(ids[i:fin], ",") + "&country=" + pays
		if err := c.get(u, &d); err != nil {
			continue
		}
		for _, r := range d.Results {
			if r.Feed != "" {
				res[strconv.Itoa(r.ID)] = feedInfo{feed: r.Feed, count: r.Count}
			}
		}
	}
	return res
}

// Search cherche des podcasts, restreint aux emissions enfants et non explicites.
func (c *Client) Search(term string) ([]Podcast, error) {
	q := url.Values{
		"media": {"podcast"}, "country": {pays}, "limit": {"100"},
		"term": {term}, "lang": {"fr_fr"},
	}
	var d struct {
		Results []struct {
			ID       int      `json:"collectionId"`
			Name     string   `json:"collectionName"`
			Artist   string   `json:"artistName"`
			Image    string   `json:"artworkUrl600"`
			Feed     string   `json:"feedUrl"`
			GenreIDs []string `json:"genreIds"`
			Explicit string   `json:"collectionExplicitness"`
			Count    int      `json:"trackCount"`
		} `json:"results"`
	}
	if err := c.get("https://itunes.apple.com/search?"+q.Encode(), &d); err != nil {
		return nil, err
	}
	out := []Podcast{}
	for _, r := range d.Results {
		// En RECHERCHE, l'intention de l'utilisateur prime : on ne restreint pas au
		// genre "Enfants & famille" (1305). Beaucoup de contenus pour enfants sont
		// classes ailleurs sur iTunes (Science, Education...) et seraient sinon
		// invisibles. On garde deux garde-fous : un flux RSS exploitable, et
		// l'exclusion du contenu explicite. Le Chart (navigation), lui, reste borne
		// au genre enfants.
		if r.Feed == "" || r.Explicit == "explicit" {
			continue
		}
		out = append(out, Podcast{
			ID: strconv.Itoa(r.ID), UUID: UUIDDeterministe("itunes:collection:" + strconv.Itoa(r.ID)),
			Title: r.Name, Artist: r.Artist, Image: r.Image,
			FeedURL: r.Feed, Explicit: r.Explicit == "explicit", Count: r.Count,
		})
	}
	return out, nil
}

// motsSignificatifs decompose un terme en mots pertinents (minuscule, sans mots
// vides courts) pour le filtre de pertinence de RechercheParEpisode.
func motsSignificatifs(term string) []string {
	vides := map[string]bool{"le": true, "la": true, "les": true, "de": true, "des": true, "du": true, "un": true, "une": true, "et": true, "au": true, "aux": true, "l": true, "d": true}
	var out []string
	for _, m := range strings.FieldsFunc(strings.ToLower(term), func(r rune) bool { return r == ' ' || r == '\'' || r == '’' || r == '-' }) {
		if len(m) >= 2 && !vides[m] {
			out = append(out, m)
		}
	}
	return out
}

// RechercheParEpisode cherche des EPISODES dont le titre correspond au terme, et
// renvoie les COLLECTIONS qui les contiennent (dedupliquees), avec l'episode
// correspondant renseigne dans EpisodeMatch. Complete Search (match sur le nom de
// la serie) par un match sur le CONTENU. Filtre de pertinence : tous les mots
// significatifs du terme doivent figurer dans le titre de l'episode.
func (c *Client) RechercheParEpisode(term string) ([]Podcast, error) {
	mots := motsSignificatifs(term)
	if len(mots) == 0 {
		return []Podcast{}, nil
	}
	q := url.Values{
		"media": {"podcast"}, "entity": {"podcastEpisode"}, "country": {pays},
		"limit": {"100"}, "term": {term}, "lang": {"fr_fr"},
	}
	var d struct {
		Results []struct {
			CollID   int    `json:"collectionId"`
			CollName string `json:"collectionName"`
			Artist   string `json:"artistName"`
			Image    string `json:"artworkUrl600"`
			Feed     string `json:"feedUrl"`
			Track    string `json:"trackName"`
			Explicit string `json:"collectionExplicitness"`
		} `json:"results"`
	}
	if err := c.get("https://itunes.apple.com/search?"+q.Encode(), &d); err != nil {
		return nil, err
	}
	out := []Podcast{}
	vus := map[int]bool{}
	for _, r := range d.Results {
		if r.Feed == "" || r.Explicit == "explicit" || r.CollID == 0 || vus[r.CollID] {
			continue
		}
		titre := strings.ToLower(r.Track)
		pertinent := true
		for _, m := range mots {
			if !strings.Contains(titre, m) {
				pertinent = false
				break
			}
		}
		if !pertinent {
			continue
		}
		vus[r.CollID] = true
		out = append(out, Podcast{
			ID: strconv.Itoa(r.CollID), UUID: UUIDDeterministe("itunes:collection:" + strconv.Itoa(r.CollID)),
			Title: r.CollName, Artist: r.Artist, Image: r.Image, FeedURL: r.Feed,
			EpisodeMatch: r.Track,
		})
	}
	return out, nil
}

// Feed renvoie la série complète (description + image + épisodes) depuis le flux.
func (c *Client) Feed(id string) (Feed, error) {
	feed, err := c.feedURL(id)
	if err != nil {
		return Feed{}, err
	}
	return ParseFeedComplet(c.HTTP, feed)
}

func (c *Client) feedURL(id string) (string, error) {
	var d struct {
		Results []struct {
			Feed string `json:"feedUrl"`
		} `json:"results"`
	}
	if err := c.get("https://itunes.apple.com/lookup?id="+url.QueryEscape(id)+"&country="+pays, &d); err != nil {
		return "", err
	}
	if len(d.Results) == 0 || d.Results[0].Feed == "" {
		return "", fmt.Errorf("flux introuvable pour %s", id)
	}
	return d.Results[0].Feed, nil
}

// --- petits types de decodage du JSON iTunes ---

type label struct {
	Label string `json:"label"`
}
type idAttr struct {
	Attributes struct {
		ID string `json:"im:id"`
	} `json:"attributes"`
}
type categoryTag struct {
	Attributes struct {
		Label string `json:"label"`
	} `json:"attributes"`
}

// dureeSecondes tolere "SS", "MM:SS", "HH:MM:SS" et un entier de secondes.
func dureeSecondes(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if !strings.Contains(s, ":") {
		n, _ := strconv.Atoi(s)
		return n
	}
	parts := strings.Split(s, ":")
	total := 0
	for _, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		total = total*60 + n
	}
	return total
}
