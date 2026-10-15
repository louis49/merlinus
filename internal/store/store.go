// Package store persiste les donnees de l'application dans le dossier de
// configuration de l'utilisateur. Volontairement generique : il ecrit et relit
// des octets sous des chemins RELATIFS (separes par "/"), sans connaitre leur
// contenu ni la signification de l'arborescence. La structure des dossiers
// (cache/ purgeable, travail/ a conserver) est decidee par les paquets appelants
// (app, library), pas ici.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"time"
)

// racineImposee : dossier de donnees fixe par DefinirRacinePourTests (vide en
// fonctionnement normal).
var racineImposee string

// DefinirRacinePourTests remplace le dossier de donnees par d (un t.TempDir ou
// os.MkdirTemp), pour que les tests des paquets appelants n'ecrivent jamais
// dans les vraies donnees de l'utilisateur. A appeler depuis un TestMain, avant
// tout acces au store. Jamais appele par l'application.
func DefinirRacinePourTests(d string) { racineImposee = d }

// Dir renvoie le dossier racine des donnees de l'application, en le creant au
// besoin (voir dossierBase pour l'emplacement selon le systeme).
func Dir() (string, error) {
	if racineImposee != "" {
		if err := os.MkdirAll(racineImposee, 0o700); err != nil {
			return "", err
		}
		return racineImposee, nil
	}
	base, err := dossierBase(runtime.GOOS, os.Getenv, os.UserHomeDir, os.UserConfigDir)
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "merlinus")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// dossierBase : dossier parent des donnees, selon les conventions de chaque
// systeme pour des DONNEES volumineuses (bibliotheque audio, sauvegarde de la
// carte : plusieurs Go), pas des reglages :
//   - macOS   : ~/Library/Application Support (os.UserConfigDir) ;
//   - Windows : %LOCALAPPDATA% (et non %APPDATA%, profil itinerant synchronise) ;
//   - Linux   : $XDG_DATA_HOME, sinon ~/.local/share (et non ~/.config).
//
// Repli sur os.UserConfigDir si la variable attendue manque.
func dossierBase(goos string, getenv func(string) string, home func() (string, error), config func() (string, error)) (string, error) {
	switch goos {
	case "windows":
		if d := getenv("LOCALAPPDATA"); d != "" {
			return d, nil
		}
	case "linux", "freebsd", "openbsd", "netbsd":
		// path (et non filepath) : chemins Unix quel que soit le systeme qui
		// execute (le test simule Linux sous Windows).
		if d := getenv("XDG_DATA_HOME"); d != "" && path.IsAbs(d) {
			return d, nil
		}
		if h, err := home(); err == nil && h != "" {
			return path.Join(h, ".local", "share"), nil
		}
	}
	return config()
}

// chemin resout un chemin relatif ("travail/boites/xxx/travail.json") en chemin
// absolu sous le dossier de donnees. Les "/" du chemin logique sont convertis
// en separateur natif.
func chemin(rel string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return confiner(d, rel)
}

// confiner joint rel au dossier d en refusant d'en sortir : chemin absolu, ".."
// qui remonte au-dessus de d, ou chemin designant d lui-meme (un RemoveDir
// effacerait toutes les donnees). Les chemins viennent en partie du front
// (uuid, mac) et de la boite (noms de fichiers op12) : c'est la barriere commune.
func confiner(d, rel string) (string, error) {
	r := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(r) || r == "." {
		return "", fmt.Errorf("chemin hors du dossier de donnees refuse : %q", rel)
	}
	return filepath.Join(d, r), nil
}

// Save ecrit des octets sous un chemin relatif, de maniere ATOMIQUE : le dossier
// parent est cree au besoin, l'ecriture se fait dans un fichier temporaire du
// meme dossier puis os.Rename vers le nom final. Un lecteur voit donc toujours
// soit l'ancien fichier complet, soit le nouveau complet, jamais un contenu
// tronque (coupure, crash). Le .tmp est nettoye si l'ecriture echoue.
func Save(rel string, data []byte) error {
	p, err := chemin(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmpNom := tmp.Name()
	defer os.Remove(tmpNom) // no-op apres un Rename reussi
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return reessayer(func() error { return os.Rename(tmpNom, p) })
}

// Load relit les octets d'un fichier du dossier de donnees.
func Load(rel string) ([]byte, error) {
	p, err := chemin(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Size renvoie la taille d'un fichier (octets), sans le charger. Sert a calculer
// l'avancement d'un televersement avant d'en lire le contenu.
func Size(rel string) (int64, error) {
	p, err := chemin(rel)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// ModTime renvoie la date de derniere modification d'un fichier.
func ModTime(rel string) (time.Time, error) {
	p, err := chemin(rel)
	if err != nil {
		return time.Time{}, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// Has indique si un fichier existe dans le dossier de donnees.
func Has(rel string) bool {
	p, err := chemin(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Remove supprime un fichier du dossier de donnees (ignore s'il n'existe pas).
func Remove(rel string) error {
	p, err := chemin(rel)
	if err != nil {
		return err
	}
	err = reessayer(func() error { return os.Remove(p) })
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// RemoveDir supprime recursivement un sous-dossier (ignore s'il n'existe pas).
// Sert a purger tout ce qui concerne une boite (travail/boites/<mac> et
// cache/boites/<mac>) en une fois.
func RemoveDir(rel string) error {
	p, err := chemin(rel)
	if err != nil {
		return err
	}
	return reessayer(func() error { return os.RemoveAll(p) })
}

// Renommer deplace un fichier ou un dossier (chemins relatifs au store). Refuse
// d'ecraser une cible existante.
func Renommer(ancien, nouveau string) error {
	pa, err := chemin(ancien)
	if err != nil {
		return err
	}
	pn, err := chemin(nouveau)
	if err != nil {
		return err
	}
	if _, err := os.Stat(pn); err == nil {
		return fmt.Errorf("%s existe deja", nouveau)
	}
	return reessayer(func() error { return os.Rename(pa, pn) })
}

// Sous Windows, Go ouvre les fichiers sans FILE_SHARE_DELETE : un fichier tenu
// ouvert (lecture audio en cours, antivirus, indexeur) ne peut etre ni remplace,
// ni supprime, ni deplace. Ces verrous sont en general brefs : on reessaie.
var (
	surWindows    = runtime.GOOS == "windows"
	pauseReessai  = 100 * time.Millisecond
	essaisMaximum = 10
)

// reessayer execute op, et sous Windows la relance tant qu'elle echoue sur un
// fichier verrouille (partage, verrou, acces refuse), au plus essaisMaximum fois.
func reessayer(op func() error) error {
	err := op()
	for i := 1; surWindows && i < essaisMaximum && fichierVerrouille(err); i++ {
		time.Sleep(pauseReessai)
		err = op()
	}
	return err
}

// fichierVerrouille : erreur Windows due a un fichier ouvert ailleurs
// (ERROR_ACCESS_DENIED 5, ERROR_SHARING_VIOLATION 32, ERROR_LOCK_VIOLATION 33).
func fichierVerrouille(err error) bool {
	var e syscall.Errno
	if !errors.As(err, &e) {
		return false
	}
	return e == 5 || e == 32 || e == 33
}

// ListerDir renvoie les noms (base) des FICHIERS d'un sous-dossier, tries par
// ordre lexicographique. Les sous-dossiers sont ignores. Renvoie une liste vide
// si le dossier n'existe pas.
func ListerDir(rel string) []string {
	p, err := chemin(rel)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// SousDossiers renvoie les noms des SOUS-DOSSIERS d'un sous-dossier (ex. la
// liste des MAC sous travail/boites). Vide si le dossier n'existe pas.
func SousDossiers(rel string) []string {
	p, err := chemin(rel)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// GarderRecentsDir ne conserve que les n fichiers les plus recents d'un
// sous-dossier et supprime les plus anciens. Repose sur un nom horodate triable
// lexicographiquement (format 20060102-150405).
func GarderRecentsDir(rel string, n int) {
	noms := ListerDir(rel)
	if len(noms) <= n {
		return
	}
	// ListerDir trie deja par ordre croissant (chronologique) : on retire tout
	// sauf les n derniers.
	for _, nom := range noms[:len(noms)-n] {
		Remove(rel + "/" + nom)
	}
}
