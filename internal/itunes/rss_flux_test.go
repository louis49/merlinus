package itunes

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servirFlux expose un flux RSS fixe sur un serveur httptest local (127.0.0.1).
// La requete recue est conservee pour verifier les en-tetes envoyes.
func servirFlux(t *testing.T, statut int, corps []byte) (string, *http.Request) {
	t.Helper()
	recu := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*recu = *r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(statut)
		_, _ = w.Write(corps)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/flux.xml", recu
}

const fluxComplet = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
<channel>
  <title>Histoires du soir</title>
  <description><![CDATA[<p>Des histoires <b>pour</b> les enfants &amp; les parents.</p>]]></description>
  <itunes:summary>Resume iTunes (ignore car description presente)</itunes:summary>
  <itunes:image href="https://exemple.test/serie-itunes.jpg"/>
  <image><url>https://exemple.test/serie-rss.jpg</url><title>x</title></image>
  <item>
    <title>Retrouvez tous les episodes sur l'appli Radio France</title>
    <enclosure url="https://exemple.test/promo.mp3" type="audio/mpeg"/>
  </item>
  <item>
    <title>  Episode 1 : le loup  </title>
    <guid isPermaLink="false">guid-ep-1</guid>
    <pubDate>Mon, 01 Jan 2024 08:00:00 +0000</pubDate>
    <itunes:duration>01:02:03</itunes:duration>
    <description><![CDATA[<p>Le loup arrive.</p> Vous aimez ce podcast ? Pour ecouter tous les episodes...]]></description>
    <enclosure url="https://exemple.test/ep1.mp3" length="123" type="audio/mpeg"/>
    <itunes:image href="https://exemple.test/ep1.jpg"/>
  </item>
  <item>
    <title>Episode 2 &amp;amp; la suite</title>
    <itunes:duration>754</itunes:duration>
    <itunes:summary>Resume prioritaire</itunes:summary>
    <description>Description secondaire</description>
    <enclosure url="https://exemple.test/ep2.m4a" type=""/>
  </item>
  <item>
    <title>Video ecartee</title>
    <enclosure url="https://exemple.test/ep3.mp4" type="video/mp4"/>
  </item>
  <item>
    <title>Sans piece jointe</title>
  </item>
</channel>
</rss>`

func TestParseFeedCompletNominal(t *testing.T) {
	url, recu := servirFlux(t, http.StatusOK, []byte(fluxComplet))
	f, err := ParseFeedComplet(http.DefaultClient, url)
	if err != nil {
		t.Fatalf("ParseFeedComplet : %v", err)
	}
	if ua := recu.Header.Get("User-Agent"); ua != "Merlinus/0.1" {
		t.Errorf("User-Agent envoye = %q", ua)
	}
	if f.Description != "Des histoires pour les enfants & les parents." {
		t.Errorf("description de chaine = %q", f.Description)
	}
	// itunes:image (attribut href) prioritaire sur image/url.
	if f.Image != "https://exemple.test/serie-itunes.jpg" {
		t.Errorf("image de chaine = %q", f.Image)
	}
	// Retenus : episodes 1 et 2 ; ecartes : promo RF, video, item sans enclosure.
	if len(f.Episodes) != 2 {
		t.Fatalf("%d episodes, attendu 2 : %+v", len(f.Episodes), f.Episodes)
	}

	e1 := f.Episodes[0]
	if e1.ID != "guid-ep-1" || e1.UUID != UUIDDeterministe("itunes:guid:guid-ep-1") {
		t.Errorf("ep1 : ID=%q UUID=%q", e1.ID, e1.UUID)
	}
	if e1.Title != "Episode 1 : le loup" {
		t.Errorf("ep1 : titre = %q (espaces de bord attendus retires)", e1.Title)
	}
	if e1.Image != "https://exemple.test/ep1.jpg" {
		t.Errorf("ep1 : image propre attendue, obtenu %q", e1.Image)
	}
	if e1.Desc != "Le loup arrive." {
		t.Errorf("ep1 : desc = %q (balises et encart promo RF attendus retires)", e1.Desc)
	}
	if e1.Duration != 3723 || e1.Published != "Mon, 01 Jan 2024 08:00:00 +0000" {
		t.Errorf("ep1 : duree=%d pub=%q", e1.Duration, e1.Published)
	}
	if e1.AudioSrc != "https://exemple.test/ep1.mp3" || !e1.Playable {
		t.Errorf("ep1 : audio=%q playable=%v", e1.AudioSrc, e1.Playable)
	}

	e2 := f.Episodes[1]
	// Sans guid : identifiant derive (sha1 tronque) de l'URL audio, stable.
	if len(e2.ID) != 16 || e2.UUID != UUIDDeterministe("itunes:guid:"+e2.ID) {
		t.Errorf("ep2 : ID=%q UUID=%q", e2.ID, e2.UUID)
	}
	sum := sha1.Sum([]byte("https://exemple.test/ep2.m4a"))
	if e2.ID != hex.EncodeToString(sum[:8]) {
		t.Errorf("ep2 : ID non derive de l'URL audio")
	}
	// Double echappement (&amp;amp;) : l'unescape HTML finit le travail.
	if e2.Title != "Episode 2 & la suite" {
		t.Errorf("ep2 : titre = %q", e2.Title)
	}
	// Sans itunes:image d'item : repli sur l'image de chaine.
	if e2.Image != "https://exemple.test/serie-itunes.jpg" {
		t.Errorf("ep2 : image = %q (repli sur la chaine attendu)", e2.Image)
	}
	// itunes:summary prioritaire sur description ; type vide = audio accepte.
	if e2.Desc != "Resume prioritaire" || e2.Duration != 754 {
		t.Errorf("ep2 : desc=%q duree=%d", e2.Desc, e2.Duration)
	}
}

// Sans itunes:image, l'image de chaine vient de <image><url>. Sans
// <description> de chaine, repli sur itunes:summary.
func TestParseFeedCompletImageRSSEtResume(t *testing.T) {
	flux := `<rss><channel>
  <itunes:summary>Resume &lt;i&gt;de&lt;/i&gt; la serie</itunes:summary>
  <image><url>https://exemple.test/serie-rss.jpg</url></image>
  <item><title>A</title><enclosure url="https://exemple.test/a.mp3" type="AUDIO/MPEG"/></item>
</channel></rss>`
	url, _ := servirFlux(t, http.StatusOK, []byte(flux))
	f, err := ParseFeedComplet(http.DefaultClient, url)
	if err != nil {
		t.Fatal(err)
	}
	if f.Image != "https://exemple.test/serie-rss.jpg" {
		t.Errorf("image = %q", f.Image)
	}
	if f.Description != "Resume de la serie" {
		t.Errorf("description = %q", f.Description)
	}
	// Type MIME en majuscules : accepte (comparaison insensible a la casse).
	if len(f.Episodes) != 1 || f.Episodes[0].Image != "https://exemple.test/serie-rss.jpg" {
		t.Errorf("episodes = %+v", f.Episodes)
	}
}

// L'ordre des deux balises image n'influe pas : itunes:image reste prioritaire.
func TestParseFeedCompletOrdreImages(t *testing.T) {
	flux := `<rss><channel>
  <image><url>https://exemple.test/rss.jpg</url></image>
  <itunes:image href="https://exemple.test/itunes.jpg"/>
</channel></rss>`
	url, _ := servirFlux(t, http.StatusOK, []byte(flux))
	f, err := ParseFeedComplet(http.DefaultClient, url)
	if err != nil {
		t.Fatal(err)
	}
	if f.Image != "https://exemple.test/itunes.jpg" {
		t.Errorf("image = %q", f.Image)
	}
	if f.Episodes == nil || len(f.Episodes) != 0 {
		t.Errorf("liste vide non nil attendue, obtenu %#v", f.Episodes)
	}
}

// Flux declare en ISO-8859-1 : les octets latin1 sont transcodes en UTF-8.
func TestParseFeedCompletISO88591(t *testing.T) {
	var b []byte
	b = append(b, `<?xml version="1.0" encoding="ISO-8859-1"?>
<rss><channel><description>Contes de f`...)
	b = append(b, 0xE9, 0x65) // "ee" avec accent aigu sur le premier e
	b = append(b, `s</description>
<item><title>L'`...)
	b = append(b, 0xE9, 't', 0xE9) // "ete" accentue
	b = append(b, `</title><enclosure url="https://exemple.test/e.mp3" type="audio/mpeg"/></item>
</channel></rss>`...)
	url, _ := servirFlux(t, http.StatusOK, b)
	f, err := ParseFeedComplet(http.DefaultClient, url)
	if err != nil {
		t.Fatalf("ParseFeedComplet : %v", err)
	}
	if f.Description != "Contes de f\u00e9es" {
		t.Errorf("description = %q", f.Description)
	}
	if len(f.Episodes) != 1 || f.Episodes[0].Title != "L'\u00e9t\u00e9" {
		t.Errorf("episodes = %+v", f.Episodes)
	}
}

// Flux mal forme mais complet : entites HTML inconnues de XML, attribut sans
// guillemets, balise non fermee. Strict=false : les episodes lisibles sortent.
func TestParseFeedCompletMalForme(t *testing.T) {
	flux := `<rss><channel>
  <description>Caf&eacute; &amp; contes &nbsp;<br></description>
  <item>
    <title>Tom & Jerry</title>
    <guid isPermaLink=false>tj</guid>
    <enclosure url="https://exemple.test/tj.mp3" type="audio/mpeg">
  </item>
  <item>
    <title>Le &eacute;l&eacute;phant</title>
    <enclosure url="https://exemple.test/el.mp3" type="audio/mpeg"/>
  </item>
</channel></rss>`
	url, _ := servirFlux(t, http.StatusOK, []byte(flux))
	f, err := ParseFeedComplet(http.DefaultClient, url)
	if err != nil {
		t.Fatalf("flux mal forme : erreur %v, attendu tolere", err)
	}
	if !strings.HasPrefix(f.Description, "Caf\u00e9 & contes") {
		t.Errorf("description = %q", f.Description)
	}
	var titres []string
	for _, e := range f.Episodes {
		titres = append(titres, e.Title+" | "+e.AudioSrc)
	}
	attendu := []string{
		"Tom & Jerry | https://exemple.test/tj.mp3",
		"Le \u00e9l\u00e9phant | https://exemple.test/el.mp3",
	}
	if strings.Join(titres, "\n") != strings.Join(attendu, "\n") {
		t.Errorf("episodes :\n%s\nattendu :\n%s", strings.Join(titres, "\n"), strings.Join(attendu, "\n"))
	}
}

// Erreurs : serveur injoignable, corps qui n'est pas du XML.
func TestParseFeedCompletErreurs(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close() // port ferme : erreur de connexion
	if _, err := ParseFeedComplet(http.DefaultClient, u+"/flux.xml"); err == nil {
		t.Error("serveur ferme : erreur attendue")
	}

	url, _ := servirFlux(t, http.StatusOK, []byte("ceci n'est pas du XML"))
	if _, err := ParseFeedComplet(http.DefaultClient, url); err == nil {
		t.Error("corps non XML : erreur attendue")
	}
	url, _ = servirFlux(t, http.StatusOK, nil)
	if _, err := ParseFeedComplet(http.DefaultClient, url); err == nil {
		t.Error("corps vide : erreur attendue")
	}
}

// servirCorps sert un corps fixe avec le statut donne.
func servirCorps(t *testing.T, statut int, corps string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statut)
		w.Write([]byte(corps))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// B20 : une page d'erreur HTTP est une erreur, pas un flux vide.
func TestFluxStatutHTTP(t *testing.T) {
	u := servirCorps(t, 404, "<html><body>Introuvable</body></html>")
	if _, err := ParseFeedComplet(http.DefaultClient, u); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 : err = %v", err)
	}
	// Page HTML servie en 200 : pas de <channel> -> erreur.
	u = servirCorps(t, 200, "<html><body>Pas un flux</body></html>")
	if _, err := ParseFeedComplet(http.DefaultClient, u); err == nil {
		t.Error("page HTML en 200 acceptee comme flux vide")
	}
}

// B20 : un flux tronque garde les items complets lus avant la coupure.
func TestFluxTronqueGardeLesItemsComplets(t *testing.T) {
	corps := `<?xml version="1.0"?><rss><channel><title>S</title>
<item><title>Un</title><enclosure url="https://exemple.test/1.mp3" type="audio/mpeg"/></item>
<item><title>Deux</title><enclosure url="https://exemple.test/2.mp3" type="audio/mpeg"/></item>
<item><title>Trois</title><enclosure url="https://exem`
	f, err := ParseFeedComplet(http.DefaultClient, servirCorps(t, 200, corps))
	if err != nil {
		t.Fatalf("flux tronque avec items complets : err = %v", err)
	}
	if len(f.Episodes) != 2 || f.Episodes[0].Title != "Un" || f.Episodes[1].Title != "Deux" {
		t.Errorf("episodes = %+v", f.Episodes)
	}
	// Tronque avant tout item complet : erreur.
	if _, err := ParseFeedComplet(http.DefaultClient, servirCorps(t, 200, `<rss><channel><item><title>X`)); err == nil {
		t.Error("flux tronque sans item complet : erreur attendue")
	}
}

// B20 : premiere enclosure AUDIO retenue, quel que soit l'ordre.
func TestFluxPremiereEnclosureAudio(t *testing.T) {
	corps := `<rss><channel><item><title>E</title>
<enclosure url="https://exemple.test/a.mp3" type="audio/mpeg"/>
<enclosure url="https://exemple.test/couv.jpg" type="image/jpeg"/>
</item><item><title>F</title>
<enclosure url="https://exemple.test/couv2.jpg" type="image/jpeg"/>
<enclosure url="https://exemple.test/b.mp3" type="audio/mpeg"/>
</item></channel></rss>`
	f, err := ParseFeedComplet(http.DefaultClient, servirCorps(t, 200, corps))
	if err != nil || len(f.Episodes) != 2 {
		t.Fatalf("err=%v episodes=%+v", err, f.Episodes)
	}
	if f.Episodes[0].AudioSrc != "https://exemple.test/a.mp3" || f.Episodes[1].AudioSrc != "https://exemple.test/b.mp3" {
		t.Errorf("audio = %q, %q", f.Episodes[0].AudioSrc, f.Episodes[1].AudioSrc)
	}
}

// B20 : itunes:title prioritaire sur title, quel que soit l'ordre ; title sinon.
func TestFluxTitreItunesPrioritaire(t *testing.T) {
	corps := `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
<item><itunes:title>Le loup</itunes:title><title>Ep 12 - Le loup</title><enclosure url="https://exemple.test/1.mp3"/></item>
<item><title>Ep 13 - Le renard</title><itunes:title>Le renard</itunes:title><enclosure url="https://exemple.test/2.mp3"/></item>
<item><title>Ep 14 - Sans titre itunes</title><enclosure url="https://exemple.test/3.mp3"/></item>
</channel></rss>`
	f, err := ParseFeedComplet(http.DefaultClient, servirCorps(t, 200, corps))
	if err != nil || len(f.Episodes) != 3 {
		t.Fatalf("err=%v episodes=%+v", err, f.Episodes)
	}
	for i, want := range []string{"Le loup", "Le renard", "Ep 14 - Sans titre itunes"} {
		if f.Episodes[i].Title != want {
			t.Errorf("episode %d : titre %q, attendu %q", i, f.Episodes[i].Title, want)
		}
	}
}
