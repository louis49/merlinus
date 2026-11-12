package radiofrance

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// serieAod : serie de n episodes sur une page, sans flux RSS ; chaque aod est
// servi par repAod(i).
func serieAod(t *testing.T, f *fauxSite, chemin string, n int, repAod func(i int) func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	var items []any
	for i := 0; i < n; i++ {
		id := "0a1b2c3d-0000-4000-8000-0000000001" + string(rune('a'+i/10)) + string(rune('0'+i%10))
		items = append(items, episodeRF(id, "Episode "+string(rune('A'+i)), "", 600, "", "2024-01-01"))
		f.routes[hote+"/transistor/aod/"+id] = repAod(i)
	}
	f.routes[hote+"/"+chemin] = texte("<html></html>")
	f.routes[hote+"/"+chemin+"/__data.json"] = texte(pageData(t, map[string]any{"items": items}))
	f.routes[hote+"/"+chemin+"/__data.json?p=2"] = texte(pageData(t, map[string]any{"items": []any{}}))
}

func nbAppelsPrefixe(f *fauxSite, prefixe string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, v := range f.appels {
		if strings.HasPrefix(k, prefixe) {
			n += v
		}
	}
	return n
}

// Un refus (429 ici) n'est pas reessaye, rend un *ErrRefus detaille et ouvre le
// disjoncteur : plus aucune requete envoyee jusqu'a la fin de la suspension.
func TestRefusDisjoncteur(t *testing.T) {
	c, f := nouveauClient(t)
	anc := dureeSuspension
	dureeSuspension = 80 * time.Millisecond
	t.Cleanup(func() { dureeSuspension = anc })
	f.routes[hote+"/limite"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "AkamaiGHost")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Access\n  Denied"))
	}
	f.routes[hote+"/ok"] = texte("ok")

	_, err := c.fetch("https://" + hote + "/limite")
	r, ok := EstRefus(err)
	if !ok || r.Suspendu || r.Statut != 429 || r.Serveur != "AkamaiGHost" || r.Extrait != "Access Denied" {
		t.Fatalf("refus : %#v (%v)", r, err)
	}
	if n := f.nbAppels(hote + "/limite"); n != 1 {
		t.Errorf("429 : %d tentatives, attendu 1 (pas de reessai)", n)
	}
	// Disjoncteur ouvert : erreur immediate, rien n'est envoye.
	_, err = c.fetch("https://" + hote + "/ok")
	if r, ok := EstRefus(err); !ok || !r.Suspendu || r.Statut != 429 {
		t.Fatalf("pendant la suspension : %v", err)
	}
	if n := f.nbAppels(hote + "/ok"); n != 0 {
		t.Errorf("%d requete(s) envoyee(s) pendant la suspension", n)
	}
	// Fin de la suspension : les requetes repartent.
	time.Sleep(100 * time.Millisecond)
	if b, err := c.fetch("https://" + hote + "/ok"); err != nil || b != "ok" {
		t.Errorf("apres la suspension : %q %v", b, err)
	}
}

// Retry-After fixe la duree de la suspension (bornee).
func TestRefusRetryAfter(t *testing.T) {
	c, f := nouveauClient(t)
	f.routes[hote+"/refus"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusForbidden)
	}
	avant := time.Now()
	_, err := c.fetch("https://" + hote + "/refus")
	r, ok := EstRefus(err)
	if !ok || r.Statut != 403 {
		t.Fatalf("refus attendu : %v", err)
	}
	if d := r.Jusqua.Sub(avant); d < 119*time.Second || d > 121*time.Second {
		t.Errorf("suspension de %v, attendu 120 s", d)
	}
	maintenant := time.Now()
	for _, c := range []struct {
		v  string
		d  time.Duration
		ok bool
	}{
		{"", 0, false}, {"abc", 0, false}, {"7", 7 * time.Second, true},
		{maintenant.Add(time.Hour).UTC().Format(http.TimeFormat), time.Hour, true},
	} {
		d, ok := delaiRetryAfter(c.v, maintenant)
		if ok != c.ok || (ok && (d < c.d-time.Second || d > c.d+time.Second)) {
			t.Errorf("Retry-After %q : %v %v", c.v, d, ok)
		}
	}
}

// Jamais plus de maxSimultanees requetes en vol, tous appels confondus.
func TestLimiteur(t *testing.T) {
	c, f := nouveauClient(t)
	var enVol, pic atomic.Int32
	f.routes[hote+"/lent"] = func(w http.ResponseWriter, _ *http.Request) {
		n := enVol.Add(1)
		for {
			p := pic.Load()
			if n <= p || pic.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		enVol.Add(-1)
		_, _ = w.Write([]byte("ok"))
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.fetch("https://" + hote + "/lent")
		}()
	}
	wg.Wait()
	if p := pic.Load(); p > maxSimultanees || p < 2 {
		t.Errorf("pic de %d requetes simultanees (plafond %d)", p, maxSimultanees)
	}
}

// Un aod refuse fait echouer toute la serie (pas de liste partielle) et les
// resolutions restantes ne sont pas envoyees.
func TestFeedAodRefuse(t *testing.T) {
	c, f := nouveauClient(t)
	serieAod(t, f, "franceinter/podcasts/serie-x", 40, func(i int) func(http.ResponseWriter, *http.Request) {
		if i == 0 {
			return statut(http.StatusForbidden)
		}
		return aod("https://cdn.test/x/ITEMA_" + string(rune('a'+i)) + ".mp3")
	})
	eps, err := c.EpisodesArrierePlan("franceinter/podcasts/serie-x")
	if _, ok := EstRefus(err); !ok {
		t.Fatalf("refus attendu : %v (%d episodes)", err, len(eps))
	}
	if n := nbAppelsPrefixe(f, hote+"/transistor/aod/"); n >= 40 {
		t.Errorf("%d aod envoyes malgre le refus", n)
	}
}

// Une erreur serveur persistante sur un aod fait echouer la serie.
func TestFeedAodErreurServeur(t *testing.T) {
	c, f := nouveauClient(t)
	serieAod(t, f, "franceinter/podcasts/serie-y", 3, func(i int) func(http.ResponseWriter, *http.Request) {
		if i == 1 {
			return statut(http.StatusServiceUnavailable)
		}
		return aod("https://cdn.test/y/ITEMA_" + string(rune('a'+i)) + ".mp3")
	})
	if _, err := c.Feed("franceinter/podcasts/serie-y"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("erreur 503 attendue : %v", err)
	}
}

// Aod en 404 ou sans source : episode ecarte ; si plus rien n'est ecoutable,
// la serie est en erreur (pas de liste vide mise en cache).
func TestFeedToutIndisponible(t *testing.T) {
	c, f := nouveauClient(t)
	serieAod(t, f, "franceinter/podcasts/serie-z", 2, func(i int) func(http.ResponseWriter, *http.Request) {
		if i == 0 {
			return statut(http.StatusNotFound)
		}
		return aod() // aucune source
	})
	if _, err := c.Feed("franceinter/podcasts/serie-z"); err == nil || !strings.Contains(err.Error(), "aucun episode disponible") {
		t.Errorf("erreur attendue : %v", err)
	}
}

// Une page de pagination refusee fait echouer la serie (avant : liste
// tronquee aux pages deja lues).
func TestFeedPageRefusee(t *testing.T) {
	c, f := nouveauClient(t)
	serieAod(t, f, "franceinter/podcasts/serie-p", 2, func(i int) func(http.ResponseWriter, *http.Request) {
		return aod("https://cdn.test/p/ITEMA_" + string(rune('a'+i)) + ".mp3")
	})
	f.routes[hote+"/franceinter/podcasts/serie-p/__data.json?p=2"] = statut(http.StatusForbidden)
	if _, err := c.Feed("franceinter/podcasts/serie-p"); err == nil {
		t.Error("erreur attendue")
	} else if r, ok := EstRefus(err); !ok || r.Statut != 403 {
		t.Errorf("refus 403 attendu : %v", err)
	}
}
