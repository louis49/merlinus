package itunes

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// Feed regroupe les métadonnées d'un flux (série) et ses épisodes.
type Feed struct {
	Description string    `json:"description"`
	Image       string    `json:"image"`
	Episodes    []Episode `json:"episodes"`
}

// ParseFeedComplet analyse un flux et renvoie AUSSI la description et l'image de
// la SÉRIE (elements du <channel>, absents de l'API iTunes Search). Tolerant : un
// flux mal forme ou tronque (coupure reseau) produit les episodes complets lus
// avant l'erreur ; l'erreur n'est rendue que si aucun episode n'a pu etre lu.
func ParseFeedComplet(hc *http.Client, feedURL string) (Feed, error) {
	req, err := http.NewRequest(http.MethodGet, feedURL, nil)
	if err != nil {
		return Feed{}, err // feedUrl externe (iTunes) mal forme
	}
	req.Header.Set("User-Agent", "Merlinus/0.1")
	resp, err := hc.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Une page d'erreur HTML se decoderait en "flux vide" : l'IHM afficherait
		// "aucun episode" au lieu d'un echec de chargement.
		return Feed{}, fmt.Errorf("flux RSS : HTTP %d", resp.StatusCode)
	}

	doc, err := lireFlux(resp.Body)
	if err != nil && len(doc.Channel.Items) == 0 {
		return Feed{}, err
	}

	imgFlux := doc.Channel.Image.Href
	if imgFlux == "" {
		imgFlux = doc.Channel.Image.URL
	}

	out := make([]Episode, 0, len(doc.Channel.Items))
	for _, it := range doc.Channel.Items {
		audio := it.audio()
		if audio == "" {
			continue // un item sans piste audio n'a rien a jouer
		}
		if estFauxEpisode(it.titre()) {
			continue // pseudo-episode promotionnel de Radio France
		}
		img := it.Image.Href
		if img == "" {
			img = imgFlux
		}
		out = append(out, Episode{
			ID:        identifiant(it),
			UUID:      UUIDDeterministe("itunes:guid:" + identifiant(it)),
			Title:     html.UnescapeString(strings.TrimSpace(it.titre())),
			Image:     img,
			Desc:      resume(it),
			Duration:  dureeSecondes(it.Duration),
			Published: strings.TrimSpace(it.PubDate),
			AudioSrc:  audio,
			Playable:  true,
		})
	}
	return Feed{Description: descriptionCanal(doc), Image: imgFlux, Episodes: out}, nil
}

// descriptionCanal renvoie la description de la série (nettoyée du HTML) depuis
// le <channel> : <description> en priorité, sinon <itunes:summary>.
func descriptionCanal(doc rssDoc) string {
	for _, s := range []string{doc.Channel.Description, doc.Channel.Summary} {
		if s = strings.TrimSpace(s); s != "" {
			return html.UnescapeString(sansBalises(s))
		}
	}
	return ""
}

// estFauxEpisode ecarte le pseudo-episode promotionnel que Radio France ajoute
// en tete de ses flux ("Retrouvez tous les episodes sur l'appli Radio France").
func estFauxEpisode(titre string) bool {
	t := strings.ToLower(titre)
	return strings.Contains(t, "retrouvez tous les épisodes") ||
		strings.Contains(t, "retrouvez tous les episodes") ||
		strings.Contains(t, "sur l'appli radio france") ||
		strings.Contains(t, "sur l\u2019appli radio france")
}

func estAudio(t string) bool {
	t = strings.ToLower(t)
	return t == "" || strings.HasPrefix(t, "audio/")
}

func identifiant(it rssItem) string {
	if id := strings.TrimSpace(it.GUID.Value); id != "" {
		return id
	}
	sum := sha1.Sum([]byte(it.audio()))
	return hex.EncodeToString(sum[:8])
}

func resume(it rssItem) string {
	for _, s := range []string{it.Summary, it.Description} {
		if s = strings.TrimSpace(s); s != "" {
			return retirerPromoRF(html.UnescapeString(sansBalises(s)))
		}
	}
	return ""
}

// retirerPromoRF coupe l'encart promotionnel que Radio France ajoute en fin de
// description d'episode ("Vous aimez ce podcast ? Pour ecouter tous les episodes
// sans limite, rendez-vous sur Radio France"). Sans effet sur les autres flux.
func retirerPromoRF(s string) string {
	if i := strings.Index(s, "Vous aimez ce podcast"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sansBalises retire un balisage HTML simple des descriptions.
func sansBalises(s string) string {
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
	return strings.TrimSpace(b.String())
}

// charsetReaderRSS decode un flux declare dans un encodage non UTF-8 (ex.
// iso-8859-1, windows-1252) pour eviter le mojibake dans les titres pousses sur
// la boite. UTF-8 (ou absence de declaration) et encodage inconnu : passe-plat
// (comportement anterieur), pour ne jamais casser un flux valide.
func charsetReaderRSS(charset string, input io.Reader) (io.Reader, error) {
	c := strings.ToLower(strings.TrimSpace(charset))
	if c == "" || c == "utf-8" || c == "utf8" || c == "us-ascii" || c == "ascii" {
		return input, nil
	}
	enc, err := htmlindex.Get(c)
	if err != nil {
		return input, nil
	}
	return enc.NewDecoder().Reader(input), nil
}

// --- structures RSS (les tags matchent le nom local, namespace ignore) ---

type rssDoc struct {
	Channel struct {
		Description string
		Summary     string // itunes:summary (namespace ignoré)
		Image       struct {
			Href string `xml:"href,attr"` // itunes:image
			URL  string `xml:"url"`       // <image><url> RSS
		}
		Items []rssItem
	}
}

type rssItem struct {
	// Titres : <title> et <itunes:title> ont le meme nom local ; on les garde
	// tous avec leur namespace pour choisir explicitement (voir titre()).
	Titres []struct {
		XMLName xml.Name
		Valeur  string `xml:",chardata"`
	} `xml:"title"`
	PubDate     string `xml:"pubDate"`
	Duration    string `xml:"duration"`
	Summary     string `xml:"summary"`
	Description string `xml:"description"`
	GUID        struct {
		Value string `xml:",chardata"`
	} `xml:"guid"`
	// Toutes les enclosures : la premiere AUDIO est retenue (voir audio()).
	Enclosures []struct {
		URL  string `xml:"url,attr"`
		Type string `xml:"type,attr"`
	} `xml:"enclosure"`
	Image struct {
		Href string `xml:"href,attr"`
	} `xml:"image"`
}

// titre : <itunes:title> s'il est renseigne (titre propre de l'episode), sinon
// <title>. Avec Strict=false, un prefixe non declare reste dans Space ("itunes").
func (it rssItem) titre() string {
	simple := ""
	for _, t := range it.Titres {
		v := strings.TrimSpace(t.Valeur)
		if v == "" {
			continue
		}
		if strings.Contains(strings.ToLower(t.XMLName.Space), "itunes") {
			return v
		}
		if simple == "" {
			simple = v
		}
	}
	return simple
}

// audio : URL de la premiere enclosure audio (type audio/* ou absent), "" sinon.
func (it rssItem) audio() string {
	for _, e := range it.Enclosures {
		if e.URL != "" && estAudio(e.Type) {
			return e.URL
		}
	}
	return ""
}

// lireFlux decode le flux element par element : une erreur (flux tronque, XML
// casse au milieu) arrete la lecture mais garde les items COMPLETS deja lus.
// Un document sans <channel> (page HTML servie en 200...) est une erreur.
func lireFlux(r io.Reader) (rssDoc, error) {
	var doc rssDoc
	dec := xml.NewDecoder(io.LimitReader(r, 16<<20))
	dec.Strict = false
	dec.CharsetReader = charsetReaderRSS
	ch := &doc.Channel
	dansCanal, vuCanal := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return doc, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !dansCanal {
				if t.Name.Local == "channel" {
					dansCanal, vuCanal = true, true
				}
				continue
			}
			var e error
			switch t.Name.Local {
			case "item":
				var it rssItem
				if e = dec.DecodeElement(&it, &t); e == nil {
					ch.Items = append(ch.Items, it)
				}
			case "description":
				e = dec.DecodeElement(&ch.Description, &t)
			case "summary":
				e = dec.DecodeElement(&ch.Summary, &t)
			case "image":
				// itunes:image (href) et <image><url> RSS : on garde ce que chacun apporte.
				var im struct {
					Href string `xml:"href,attr"`
					URL  string `xml:"url"`
				}
				if e = dec.DecodeElement(&im, &t); e == nil {
					if im.Href != "" {
						ch.Image.Href = im.Href
					}
					if im.URL != "" {
						ch.Image.URL = im.URL
					}
				}
			default:
				e = dec.Skip()
			}
			if e != nil {
				return doc, e
			}
		case xml.EndElement:
			if dansCanal && t.Name.Local == "channel" {
				dansCanal = false
			}
		}
	}
	if !vuCanal {
		return doc, fmt.Errorf("flux RSS : pas d'element <channel>")
	}
	return doc, nil
}
