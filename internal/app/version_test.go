package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionDepuisConfig(t *testing.T) {
	t.Setenv("MERLINUS_VERSION", "")
	if v := versionDepuisConfig([]byte(`{"info":{"productName":"Merlinus","productVersion":"1.0.3"}}`)); v != "1.0.3" {
		t.Errorf("version = %q, attendu 1.0.3", v)
	}
	if v := versionDepuisConfig([]byte(`{"info":{"productName":"Merlinus"}}`)); v != versionDev {
		t.Errorf("sans productVersion : %q, attendu %q", v, versionDev)
	}
	t.Setenv("MERLINUS_VERSION", "1.0.0")
	if v := versionDepuisConfig([]byte(`{"info":{"productVersion":"1.0.3"}}`)); v != "1.0.0" {
		t.Errorf("MERLINUS_VERSION ignoree : %q", v)
	}
}

func TestPlusRecente(t *testing.T) {
	cas := []struct {
		a, b string
		want bool
	}{
		{"1.0.3", "1.0.2", true},
		{"1.0.10", "1.0.9", true}, // comparaison numerique, pas lexicale
		{"2.0.0", "1.9.9", true},
		{"1.0.2", "1.0.2", false},
		{"1.0.1", "1.0.2", false},
		{"1.0.3", "dev", false},
		{"v1.1.0", "1.0.0", true},
	}
	for _, c := range cas {
		if got := plusRecente(c.a, c.b); got != c.want {
			t.Errorf("plusRecente(%q, %q) = %v, attendu %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDerniereRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "Merlinus/1.0.2" {
			t.Errorf("User-Agent = %q", ua)
		}
		w.Write([]byte(`{"tag_name":"v1.0.3","html_url":"https://example.invalid/ignore"}`))
	}))
	defer srv.Close()
	tag, err := derniereRelease(srv.Client(), srv.URL, "1.0.2")
	if err != nil || tag != "1.0.3" {
		t.Fatalf("tag = %q, err = %v", tag, err)
	}

	mauvais := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"nightly"}`))
	}))
	defer mauvais.Close()
	if _, err := derniereRelease(mauvais.Client(), mauvais.URL, "1.0.2"); err == nil {
		t.Error("tag hors format accepte")
	}
}
