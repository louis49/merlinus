package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSaveAtomiqueRoundtrip(t *testing.T) {
	dir := fmt.Sprintf("test-atomique-%d", time.Now().UnixNano())
	nom := dir + "/fichier.bin"
	defer RemoveDir(dir)
	data := []byte("contenu-test-atomique")
	if err := Save(nom, data); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(nom)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("round-trip: err=%v got=%q", err, got)
	}
	// Un seul fichier dans le dossier : pas de .tmp residuel (CreateTemp cree un
	// fichier cache ".<nom>.tmp-*", mais ListerDir inclut tout fichier non-dir).
	if noms := ListerDir(dir); len(noms) != 1 || noms[0] != "fichier.bin" {
		t.Errorf("contenu du dossier inattendu: %v", noms)
	}
	if err := Save(nom, []byte("v2")); err != nil {
		t.Fatalf("Save v2: %v", err)
	}
	if got, _ := Load(nom); string(got) != "v2" {
		t.Errorf("ecrasement: got=%q", got)
	}
}

func TestGarderRecentsDir(t *testing.T) {
	dir := fmt.Sprintf("test-gr-%d", time.Now().UnixNano())
	defer RemoveDir(dir)
	noms := []string{
		"20260101-000001.bin",
		"20260101-000002.bin",
		"20260101-000003.bin",
		"20260101-000004.bin",
	}
	for _, n := range noms {
		_ = Save(dir+"/"+n, []byte("x"))
	}
	GarderRecentsDir(dir, 2)
	rest := ListerDir(dir)
	want := []string{noms[2], noms[3]} // les 2 plus recents
	if len(rest) != 2 || rest[0] != want[0] || rest[1] != want[1] {
		t.Fatalf("GarderRecentsDir : restants=%v, attendu %v", rest, want)
	}
}

func TestConfiner(t *testing.T) {
	d := filepath.Join(string(filepath.Separator)+"donnees", "merlinus")
	for rel, ok := range map[string]bool{
		"travail/boites/abc/travail.json":                true,
		"travail/bibliotheque/1111-22.mp3":               true,
		"cache/boites/x/../y/arbre.json":                 true, // reste dedans
		"travail/bibliotheque/../../../../Users/x/a.mp3": false,
		"../a":        false,
		"/etc/passwd": false,
		"":            false, // le dossier lui-meme
		".":           false,
		"cache/..":    false,
	} {
		p, err := confiner(d, rel)
		if ok && (err != nil || !strings.HasPrefix(p, d+string(filepath.Separator))) {
			t.Errorf("%q : refuse a tort (%q, %v)", rel, p, err)
		}
		if !ok && err == nil {
			t.Errorf("%q : accepte a tort -> %q", rel, p)
		}
	}
}

// FT1.3 : emplacement des donnees selon le systeme.
func TestDossierBase(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	home := func() (string, error) { return "/home/lou", nil }
	conf := func() (string, error) { return "/conf", nil }
	for _, c := range []struct {
		goos    string
		vars    map[string]string
		attendu string
	}{
		{"darwin", nil, "/conf"},
		{"windows", map[string]string{"LOCALAPPDATA": `C:\Users\lou\AppData\Local`}, `C:\Users\lou\AppData\Local`},
		{"windows", nil, "/conf"},
		{"linux", map[string]string{"XDG_DATA_HOME": "/donnees"}, "/donnees"},
		{"linux", map[string]string{"XDG_DATA_HOME": "relatif"}, "/home/lou/.local/share"},
		{"linux", nil, "/home/lou/.local/share"},
	} {
		got, err := dossierBase(c.goos, env(c.vars), home, conf)
		if err != nil || got != c.attendu {
			t.Errorf("%s %v : %q (%v), attendu %q", c.goos, c.vars, got, err, c.attendu)
		}
	}
}

// FT1.4 : sous Windows, un verrou bref (fichier ouvert) est reessaye ; une
// autre erreur ne l'est pas ; hors Windows, aucun reessai.
func TestReessayer(t *testing.T) {
	anciens := []any{surWindows, pauseReessai}
	defer func() { surWindows, pauseReessai = anciens[0].(bool), anciens[1].(time.Duration) }()
	pauseReessai = time.Millisecond

	compteur := func(echecs int, e error) (func() error, *int) {
		n := 0
		return func() error {
			n++
			if n <= echecs {
				return &os.PathError{Op: "rename", Path: "x", Err: e}
			}
			return nil
		}, &n
	}
	surWindows = true
	op, n := compteur(2, syscall.Errno(32)) // ERROR_SHARING_VIOLATION
	if err := reessayer(op); err != nil || *n != 3 {
		t.Errorf("verrou bref : err=%v essais=%d", err, *n)
	}
	op, n = compteur(99, syscall.Errno(32))
	if err := reessayer(op); err == nil || *n != essaisMaximum {
		t.Errorf("verrou durable : err=%v essais=%d", err, *n)
	}
	op, n = compteur(1, syscall.Errno(2)) // fichier absent : pas de reessai
	if err := reessayer(op); err == nil || *n != 1 {
		t.Errorf("autre erreur : err=%v essais=%d", err, *n)
	}
	surWindows = false
	op, n = compteur(1, syscall.Errno(32))
	if err := reessayer(op); err == nil || *n != 1 {
		t.Errorf("hors Windows : err=%v essais=%d", err, *n)
	}
}
