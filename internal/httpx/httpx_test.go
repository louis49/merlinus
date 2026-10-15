package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetReessais(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Test/1" {
			t.Errorf("UA = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/transitoire":
			if n.Add(1) < 3 {
				w.WriteHeader(503)
				return
			}
			w.Write([]byte("ok"))
		case "/absent":
			n.Add(1)
			w.WriteHeader(404)
		case "/gros":
			w.Write(make([]byte, 100))
		}
	}))
	defer srv.Close()
	o := Options{UA: "Test/1", Plafond: 10, Delais: []time.Duration{time.Millisecond, time.Millisecond}}

	b, st, err := Get(srv.Client(), srv.URL+"/transitoire", o)
	if err != nil || st != 200 || string(b) != "ok" || n.Load() != 3 {
		t.Errorf("transitoire : %q %d %v (essais %d)", b, st, err, n.Load())
	}
	n.Store(0)
	if _, st, _ := Get(srv.Client(), srv.URL+"/absent", o); st != 404 || n.Load() != 1 {
		t.Errorf("404 : statut %d, essais %d (attendu 1)", st, n.Load())
	}
	if b, _, _ := Get(srv.Client(), srv.URL+"/gros", o); len(b) != 10 {
		t.Errorf("plafond : %d octets lus", len(b))
	}
	if _, _, err := Get(srv.Client(), "https://exa mple.com/x", o); err == nil {
		t.Error("URL invalide acceptee")
	}
}
