package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// cleErreur renvoie la cle i18n d'une ErreurIHM ("" sinon).
func cleErreur(err error) string {
	var e *ErreurIHM
	if errors.As(err, &e) {
		return e.Cle
	}
	return ""
}

func TestErreurIHMFormat(t *testing.T) {
	err := errIHM("err_espace", "besoinOctets", int64(10), "libreOctets", int64(5))
	if got := err.Error(); got != `ihm:{"cle":"err_espace","params":{"besoinOctets":10,"libreOctets":5}}` {
		t.Errorf("Error() = %s", got)
	}
	if cleErreur(err) != "err_espace" {
		t.Error("cle")
	}
}

// Toute cle i18n emise par le Go (erreurs, libelles de progression) existe dans
// les deux dictionnaires du front.
func TestClesI18nDuGo(t *testing.T) {
	re := regexp.MustCompile(`(?:errIHM|errDetail)\("([a-z_]+)"|"(prog_[a-z_]+)"`)
	fichiers, _ := filepath.Glob("*.go")
	cles := map[string]bool{}
	for _, f := range fichiers {
		b, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if m[1] != "" && m[1] != "err_espace_test" {
				cles[m[1]] = true
			}
			if m[2] != "" {
				cles[m[2]] = true
			}
		}
	}
	if len(cles) < 40 {
		t.Fatalf("seulement %d cles trouvees : extraction cassee ?", len(cles))
	}
	for _, lang := range []string{"fr", "en"} {
		b, err := os.ReadFile("../../frontend/src/locales/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var dict map[string]string
		if err := json.Unmarshal(b, &dict); err != nil {
			t.Fatal(err)
		}
		for c := range cles {
			if dict[c] == "" {
				t.Errorf("%s.json : cle %q absente", lang, c)
			}
		}
	}
}
