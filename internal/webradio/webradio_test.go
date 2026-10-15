package webradio

import "testing"

func TestMacBase(t *testing.T) {
	cas := []struct{ softAP, base string }{
		{"38:18:2b:93:ef:6d", "38:18:2B:93:EF:6C"},
		{"38:18:2B:93:EF:6D", "38:18:2B:93:EF:6C"},
		{"00:11:22:33:44:01", "00:11:22:33:44:00"},
		{"38:18:2B:93:EF:00", "38:18:2B:93:EE:FF"}, // retenue sur l'octet precedent
	}
	for _, c := range cas {
		got, err := MacBase(c.softAP)
		if err != nil {
			t.Fatalf("%s : %v", c.softAP, err)
		}
		if got != c.base {
			t.Errorf("MacBase(%s) = %s, attendu %s", c.softAP, got, c.base)
		}
	}
	if _, err := MacBase("pas-un-mac"); err == nil {
		t.Errorf("un MAC mal forme devrait etre rejete")
	}
}
