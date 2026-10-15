package radiofrance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/louis49/merlinus/internal/itunes"
)

// Les URL de Radio France sont des constantes du paquet (rfBase, listingURL,
// /transistor/aod...). Plutot que de modifier le code de production, on injecte
// un http.Client dont le transport redirige TOUTE requete vers un serveur
// httptest local (127.0.0.1), en conservant l'hote d'origine dans un en-tete.
// Aucun acces reseau reel.

const enteteHote = "X-Hote-Origine"

type redirection struct{ cible *url.URL }

func (r redirection) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.Header.Set(enteteHote, req.URL.Host)
	c.URL.Scheme = r.cible.Scheme
	c.URL.Host = r.cible.Host
	c.Host = r.cible.Host
	return http.DefaultTransport.RoundTrip(c)
}

// faux site : routes par "hote+chemin?requete" -> reponse. Compte les appels.
type fauxSite struct {
	mu      sync.Mutex
	routes  map[string]func(w http.ResponseWriter, r *http.Request)
	appels  map[string]int
	inconnu []string
}

func (f *fauxSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cle := r.Header.Get(enteteHote) + r.URL.Path
	if r.URL.RawQuery != "" {
		cle += "?" + r.URL.RawQuery
	}
	f.mu.Lock()
	f.appels[cle]++
	h, ok := f.routes[cle]
	if !ok {
		// Recherche sans la requete (payload variable).
		h, ok = f.routes[r.Header.Get(enteteHote)+r.URL.Path+"?*"]
	}
	if !ok {
		f.inconnu = append(f.inconnu, cle)
	}
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (f *fauxSite) nbAppels(cle string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appels[cle]
}

func texte(s string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(s)) }
}

func statut(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// nouveauClient demarre le faux site et renvoie un Client qui y est branche.
// Les delais de reessai sont reduits pour la duree du test.
func nouveauClient(t *testing.T) (*Client, *fauxSite) {
	t.Helper()
	f := &fauxSite{routes: map[string]func(http.ResponseWriter, *http.Request){}, appels: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cible, _ := url.Parse(srv.URL)
	anciens := backoffsRF
	backoffsRF = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { backoffsRF = anciens })
	c := New()
	c.HTTP = &http.Client{Transport: redirection{cible}, Timeout: 5 * time.Second}
	return c, f
}

const hote = "www.radiofrance.fr"

func TestRequeteReessais(t *testing.T) {
	c, f := nouveauClient(t)
	n := 0
	f.routes[hote+"/instable"] = func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}
	f.routes[hote+"/limite"] = statut(http.StatusTooManyRequests)
	f.routes[hote+"/absent"] = statut(http.StatusNotFound)

	// 2 echecs 503 puis succes : 3 tentatives au total.
	if body, err := c.fetch("https://" + hote + "/instable"); err != nil || body != "ok" {
		t.Errorf("instable : %q, %v", body, err)
	}
	// 429 persistant : 3 tentatives puis erreur.
	if _, err := c.fetch("https://" + hote + "/limite"); err == nil {
		t.Error("429 : erreur attendue")
	}
	if got := f.nbAppels(hote + "/limite"); got != 3 {
		t.Errorf("429 : %d tentatives, attendu 3", got)
	}
	// 404 : deterministe, une seule tentative.
	if _, err := c.fetch("https://" + hote + "/absent"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 : erreur %v", err)
	}
	if got := f.nbAppels(hote + "/absent"); got != 1 {
		t.Errorf("404 : %d tentatives, attendu 1", got)
	}
}

func TestList(t *testing.T) {
	c, f := nouveauClient(t)
	concept := func(href, titre, typ, marque string) map[string]any {
		o := map[string]any{"__typename": "Concept", "href": href, "title": titre, "brand": marque,
			"square": map[string]any{"id": "img"}}
		if typ != "" {
			o["type"] = typ
		}
		return o
	}
	f.routes[hote+"/podcasts/enfants/__data.json"] = texte(pageData(t,
		map[string]any{"concepts": []any{
			concept("/franceinter/podcasts/bestioles", "Bestioles &amp; Cie", "", "franceinter"),
			concept("/franceculture/podcasts/serie-oli", "Oli", "serie", "franceculture"),
			concept("/franceinter/podcasts/selection-noel", "Selection", "selection", "franceinter"),
			concept("/franceinter/podcasts/bestioles", "Doublon", "", "franceinter"),
			concept("https://ailleurs.test/podcasts/x", "Externe", "", "franceinter"),
			concept("/franceinter/podcasts/a/b", "Trop profond", "", "franceinter"),
			concept("/franceinter/podcasts/sans-titre", "", "", "franceinter"),
			map[string]any{"__typename": "Expression", "href": "/franceinter/podcasts/autre", "title": "Pas un concept"},
		}},
	))
	pods, err := c.List()
	if err != nil {
		t.Fatalf("List : %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("%d podcasts, attendu 2 : %+v", len(pods), pods)
	}
	p := pods[0]
	if p.ID != "franceinter/podcasts/bestioles" || p.UUID != itunes.UUIDDeterministe("rf:franceinter/podcasts/bestioles") {
		t.Errorf("ID/UUID : %q %q", p.ID, p.UUID)
	}
	if p.Title != "Bestioles & Cie" || p.Artist != "France Inter" || p.Category != "France Inter" {
		t.Errorf("podcast : %+v", p)
	}
	if p.Image != "https://www.radiofrance.fr/pikapi/images/img/400x400?webp=true" {
		t.Errorf("image : %q", p.Image)
	}
	if pods[1].ID != "franceculture/podcasts/serie-oli" || pods[1].Artist != "France Culture" {
		t.Errorf("serie : %+v", pods[1])
	}

	// Listing indisponible ou illisible : erreur.
	f.routes[hote+"/podcasts/enfants/__data.json"] = texte("pas du json")
	if _, err := c.List(); err == nil {
		t.Error("JSON illisible : erreur attendue")
	}
	f.routes[hote+"/podcasts/enfants/__data.json"] = statut(http.StatusNotFound)
	if _, err := c.List(); err == nil {
		t.Error("404 : erreur attendue")
	}
}

// fluxRSS : flux minimal servi pour la description et l'image de serie.
const fluxRSS = `<rss><channel>
<description>Une serie pour les petits. Vous aimez ce podcast ? Pour ecouter...</description>
<itunes:image href="https://exemple.test/serie.jpg"/>
<item><title>Episode RSS</title><enclosure url="https://exemple.test/rss.mp3" type="audio/mpeg"/></item>
</channel></rss>`

func aod(urls ...string) func(http.ResponseWriter, *http.Request) {
	var s []map[string]string
	for _, u := range urls {
		s = append(s, map[string]string{"url": u})
	}
	b, _ := json.Marshal(map[string]any{"sources": s})
	return texte(string(b))
}

// Feed nominal : episodes via la pagination __data.json + aod, description et
// image de serie via le flux RSS (forme https podcast_<uuid> preferee).
func TestFeedVoiePaginee(t *testing.T) {
	c, f := nouveauClient(t)
	chemin := "franceinter/podcasts/serie-test"
	f.routes[hote+"/"+chemin] = texte(`<html>
<a href="http://radiofrance-podcast.net/podcast09/rss_12345.xml">ancien</a>
<a href="https://radiofrance-podcast.net/podcast09/podcast_0a1b2c3d-1111-2222-3333-444455556666.xml">rss</a>
</html>`)
	page1 := pageData(t, map[string]any{"items": []any{
		episodeRF(idEp1, "Episode 10", "dix", 900, "v10", "2024-01-10"),
		episodeRF(idEp2, "Episode 2", "deux", 800, "", "2024-01-02"),
	}})
	page2 := pageData(t, map[string]any{"items": []any{
		episodeRF(idEp3, "Episode 1", "un", 700, "", "2024-01-01"),
	}})
	f.routes[hote+"/"+chemin+"/__data.json"] = texte(page1)
	f.routes[hote+"/"+chemin+"/__data.json?p=2"] = texte(page2)
	f.routes[hote+"/"+chemin+"/__data.json?p=3"] = texte(page2) // rien de nouveau : arret
	f.routes[hote+"/transistor/aod/"+idEp1] = aod("https://cdn.test/a/ITEMA_10.m4a", "https://cdn.test/a/ITEMA_10.mp3")
	f.routes[hote+"/transistor/aod/"+idEp2] = aod("https://cdn.test/a/ITEMA_2.m4a")
	// idEp3 : aod en 404 -> episode non jouable, ecarte.
	f.routes["radiofrance-podcast.net/podcast09/podcast_0a1b2c3d-1111-2222-3333-444455556666.xml"] = texte(fluxRSS)

	feed, err := c.Feed("/" + chemin + "/")
	if err != nil {
		t.Fatalf("Feed : %v", err)
	}
	if feed.Description != "Une serie pour les petits." || feed.Image != "https://exemple.test/serie.jpg" {
		t.Errorf("description/image : %q %q", feed.Description, feed.Image)
	}
	if len(feed.Episodes) != 2 {
		t.Fatalf("%d episodes, attendu 2 : %+v", len(feed.Episodes), feed.Episodes)
	}
	// Tri naturel : "Episode 2" avant "Episode 10".
	e2, e10 := feed.Episodes[0], feed.Episodes[1]
	if e2.Title != "Episode 2" || e10.Title != "Episode 10" {
		t.Fatalf("ordre : %q puis %q", e2.Title, e10.Title)
	}
	// mp3 prefere au m4a ; m4a retenu a defaut.
	if e10.AudioSrc != "https://cdn.test/a/ITEMA_10.mp3" || e2.AudioSrc != "https://cdn.test/a/ITEMA_2.m4a" {
		t.Errorf("audio : %q / %q", e10.AudioSrc, e2.AudioSrc)
	}
	if e10.UUID != itunes.UUIDDeterministe("rf:file:ITEMA_10") || e10.ID != idEp1 {
		t.Errorf("ep10 : ID=%q UUID=%q", e10.ID, e10.UUID)
	}
	if e10.Image != "https://www.radiofrance.fr/pikapi/images/v10/560x315?webp=true" || e2.Image != "" {
		t.Errorf("images : %q / %q", e10.Image, e2.Image)
	}
	if e10.Desc != "dix" || e10.Duration != 900 || e10.Published != "2024-01-10" || !e10.Playable {
		t.Errorf("ep10 : %+v", e10)
	}
	// Le flux "rss_12345" (non prefere) n'a pas ete consulte.
	if n := f.nbAppels("radiofrance-podcast.net/podcast09/rss_12345.xml"); n != 0 {
		t.Errorf("flux non prefere consulte %d fois", n)
	}
	// Pagination : la page 3 (identique a la 2) a ete lue puis a arrete la boucle.
	if f.nbAppels(hote+"/"+chemin+"/__data.json?p=3") != 1 || f.nbAppels(hote+"/"+chemin+"/__data.json?p=4") != 0 {
		t.Error("pagination : arret attendu apres la page 3")
	}
}

// Pagination indisponible : repli sur le flux RSS (premier trouve si aucun n'a
// la forme https podcast_<uuid>).
func TestFeedRepliRSS(t *testing.T) {
	c, f := nouveauClient(t)
	chemin := "franceinter/podcasts/ancien"
	f.routes[hote+"/"+chemin] = texte(`x "https://radiofrance-podcast.net/podcast09/rss_777.xml" y`)
	// __data.json absent (404).
	f.routes["radiofrance-podcast.net/podcast09/rss_777.xml"] = texte(fluxRSS)
	feed, err := c.Feed(chemin)
	if err != nil {
		t.Fatalf("Feed : %v", err)
	}
	if feed.Description != "Une serie pour les petits." {
		t.Errorf("description : %q", feed.Description)
	}
	if len(feed.Episodes) != 1 || feed.Episodes[0].AudioSrc != "https://exemple.test/rss.mp3" {
		t.Errorf("episodes : %+v", feed.Episodes)
	}
}

func TestFeedErreurs(t *testing.T) {
	c, f := nouveauClient(t)
	// Page de la serie introuvable.
	if _, err := c.Feed("franceinter/podcasts/absent"); err == nil {
		t.Error("page absente : erreur attendue")
	}
	// Page presente, sans flux ni episodes : erreur de la voie paginee.
	f.routes[hote+"/franceinter/podcasts/vide"] = texte("<html></html>")
	f.routes[hote+"/franceinter/podcasts/vide/__data.json"] = texte(pageData(t, map[string]any{"items": []any{}}))
	if _, err := c.Feed("franceinter/podcasts/vide"); err == nil || !strings.Contains(err.Error(), "aucun episode") {
		t.Errorf("serie vide : erreur %v", err)
	}
}

// Recherche : decouverte du hash (page -> script app -> chunks), appel de la
// remote function, cache du hash, et re-extraction unique sur 404.
func TestSearch(t *testing.T) {
	c, f := nouveauClient(t)
	var mu sync.Mutex
	hashCourant := "abc12"
	f.routes[hote+"/recherche?q=a&results=true"] = texte(`<script src="/_app/immutable/entry/app.Xy12Ab.js"></script>`)
	f.routes[hote+"/_app/immutable/entry/app.Xy12Ab.js"] = texte(`import("./chunkA1.js");import("./chunkB2.js")`)
	f.routes[hote+"/_app/immutable/chunks/chunkA1.js"] = texte(`const x = 1;`)
	f.routes[hote+"/_app/immutable/chunks/chunkB2.js"] = func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		h := hashCourant
		mu.Unlock()
		_, _ = w.Write([]byte(`fetch("` + h + `/getSearchItems")`))
	}
	resultat := devalue(map[string]any{"items": []any{
		map[string]any{"__typename": "Concept", "href": "/franceinter/podcasts/le-loup", "text": "<em>Le</em>&nbsp;loup", "brand": "franceinter",
			"visual": map[string]any{"id": "vis"}},
		map[string]any{"__typename": "Concept", "href": "/franceinter/podcasts/selection-loups", "text": "Selection"},
		map[string]any{"__typename": "Concept", "href": "/franceinter/podcasts/sans-texte", "text": ""},
		map[string]any{"__typename": "Expression", "href": "/franceinter/podcasts/episode", "text": "Episode"},
	}})
	brut, _ := json.Marshal(resultat)
	env, _ := json.Marshal(map[string]string{"type": "result", "result": string(brut)})
	f.routes[hote+"/_app/remote/abc12/getSearchItems?*"] = func(w http.ResponseWriter, r *http.Request) {
		// Le payload est echappe dans l'URL (B22) : on compare la valeur decodee,
		// et la requete brute ne doit porter aucun "+" (relu comme une espace).
		if p := r.URL.Query().Get("payload"); p != payloadRecherche("loup") {
			t.Errorf("payload inattendu : %q", p)
		}
		if strings.Contains(r.URL.RawQuery, "+") {
			t.Errorf("payload non echappe : %q", r.URL.RawQuery)
		}
		_, _ = w.Write(env)
	}

	if pods, err := c.Search("   "); err != nil || len(pods) != 0 || pods == nil {
		t.Errorf("terme vide : %#v, %v", pods, err)
	}
	pods, err := c.Search(" loup ")
	if err != nil {
		t.Fatalf("Search : %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("%d resultats, attendu 1 : %+v", len(pods), pods)
	}
	if p := pods[0]; p.Title != "Le loup" || p.ID != "franceinter/podcasts/le-loup" || p.Artist != "France Inter" ||
		p.Image != "https://www.radiofrance.fr/pikapi/images/vis/400x400?webp=true" {
		t.Errorf("resultat : %+v", p)
	}

	// Second appel : hash en cache, la page de recherche n'est pas relue.
	if _, err := c.Search("loup"); err != nil {
		t.Fatal(err)
	}
	if n := f.nbAppels(hote + "/recherche?q=a&results=true"); n != 1 {
		t.Errorf("page de recherche lue %d fois, attendu 1 (cache)", n)
	}

	// Nouveau deploiement : l'ancien hash repond 404, le nouveau est re-extrait.
	mu.Lock()
	hashCourant = "def34"
	mu.Unlock()
	f.routes[hote+"/_app/remote/abc12/getSearchItems?*"] = statut(http.StatusNotFound)
	f.routes[hote+"/_app/remote/def34/getSearchItems?*"] = texte(string(env))
	pods, err = c.Search("loup")
	if err != nil || len(pods) != 1 {
		t.Fatalf("apres re-extraction : %+v, %v", pods, err)
	}
	if n := f.nbAppels(hote + "/recherche?q=a&results=true"); n != 2 {
		t.Errorf("page de recherche lue %d fois, attendu 2", n)
	}

	// Reponse illisible.
	f.routes[hote+"/_app/remote/def34/getSearchItems?*"] = texte(`{"result":""}`)
	if _, err := c.Search("loup"); err == nil {
		t.Error("reponse vide : erreur attendue")
	}
	f.routes[hote+"/_app/remote/def34/getSearchItems?*"] = texte(`{"result":"pas un tableau"}`)
	if _, err := c.Search("loup"); err == nil {
		t.Error("result non JSON : erreur attendue")
	}
}

func TestSearchHashIntrouvable(t *testing.T) {
	c, f := nouveauClient(t)
	f.routes[hote+"/recherche?q=a&results=true"] = texte(`<html>pas de script</html>`)
	if _, err := c.Search("loup"); err == nil || !strings.Contains(err.Error(), "script app") {
		t.Errorf("erreur %v, attendu script app introuvable", err)
	}
	f.routes[hote+"/recherche?q=a&results=true"] = texte(`/_app/immutable/entry/app.Q1.js`)
	f.routes[hote+"/_app/immutable/entry/app.Q1.js"] = texte(`import("./chunkZZ.js")`)
	if _, err := c.Search("loup"); err == nil || !strings.Contains(err.Error(), "getSearchItems introuvable") {
		t.Errorf("erreur %v, attendu hash introuvable", err)
	}
}

// B22 : un payload base64 contenant "+" (verifie en reel avec "¾conte", que le
// site rejetait) survit a l'aller-retour dans la query string.
func TestPayloadRechercheEchappe(t *testing.T) {
	p := payloadRecherche("\u00beconte")
	if !strings.Contains(p, "+") {
		t.Fatal("le cas de test doit produire un '+' en base64")
	}
	u, err := url.Parse("https://exemple.test/x?payload=" + url.QueryEscape(p))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("payload"); got != p {
		t.Errorf("payload relu %q, attendu %q", got, p)
	}
}
