package app

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestAdressePublique(t *testing.T) {
	for ip, ok := range map[string]bool{
		"17.253.144.10":      true, // CDN public
		"2a02:26f0::1":       true,
		"127.0.0.1":          false,
		"::1":                false,
		"192.168.4.1":        false, // la boite elle-meme
		"10.0.0.1":           false,
		"172.16.5.4":         false,
		"169.254.169.254":    false,
		"0.0.0.0":            false,
		"fd00::1":            false,
		"fe80::1":            false,
		"::ffff:192.168.1.1": false, // IPv4 mappee
	} {
		if adressePublique(netip.MustParseAddr(ip)) != ok {
			t.Errorf("adressePublique(%s) != %v", ip, ok)
		}
	}
}

// Un serveur local (loopback) doit etre injoignable par le client de /catimg.
func TestTelechargerImageCatalogueRefuseLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("\x89PNG\r\n\x1a\n0000"))
	}))
	defer srv.Close()
	if _, err := telechargerImageCatalogue(srv.URL + "/a.png"); err == nil {
		t.Fatal("serveur loopback joint : le filtre d'adresse ne s'applique pas")
	}
}

func TestEstImage(t *testing.T) {
	for nom, c := range map[string]struct {
		b  string
		ok bool
	}{
		"png":  {"\x89PNG\r\n\x1a\n0000", true},
		"jpeg": {"\xff\xd8\xff\xe0", true},
		"webp": {"RIFF0000WEBPVP8 ", true},
		"html": {"<html><body>admin</body></html>", false},
		"vide": {"", false},
	} {
		if estImage([]byte(c.b)) != c.ok {
			t.Errorf("estImage(%s) != %v", nom, c.ok)
		}
	}
}
