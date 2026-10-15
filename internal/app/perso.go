package app

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/store"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const ficPerso = "travail/perso.json"

// HistoirePerso : une histoire importee par l'utilisateur, independante de toute
// boite (perso.json n'est pas indexe par MAC). L'audio et l'illustration vivent
// dans la bibliotheque partagee : media-<uuid>.<ext> et media-<uuid>.jpg.
type HistoirePerso struct {
	UUID   string `json:"uuid"`
	Titre  string `json:"titre"`
	Ext    string `json:"ext"`           // "mp3" ou "aac"
	Taille int64  `json:"taille"`        // octets du media stocke
	Cover  bool   `json:"cover"`         // illustration presente
	Ajout  int64  `json:"ajout"`         // horodatage unix
	Duree  int    `json:"duree"`         // secondes, 0 si inconnue (calcule, non persiste)
	Img    string `json:"img,omitempty"` // data URL de l'illustration (calcule, non persiste)
	// Referencee : l'histoire est presente dans l'arbre de travail d'une boite
	// (calcule, non persiste). Quand vrai, la suppression est interdite.
	Referencee bool `json:"referencee"`
}

// persoReferencees collecte les uuids d'histoires presentes dans un arbre de
// travail de boite (travail-<mac>.json), pour interdire leur suppression.
func (a *App) persoReferencees() map[string]bool {
	ref := map[string]bool{}
	for _, mac := range store.SousDossiers("travail/boites") {
		data, err := store.Load(ficTravail(mac))
		if err != nil {
			continue
		}
		var arbre []wNode
		if json.Unmarshal(data, &arbre) != nil {
			continue
		}
		for _, w := range arbre {
			feuillesSous(w, ref)
		}
	}
	return ref
}

func (a *App) lirePerso() []HistoirePerso {
	var l []HistoirePerso
	if d, e := store.Load(ficPerso); e == nil {
		_ = json.Unmarshal(d, &l)
	}
	return l
}

func (a *App) ecrirePerso(l []HistoirePerso) {
	if d, e := json.Marshal(l); e == nil {
		_ = store.Save(ficPerso, d)
	}
}

// imgDataURL renvoie l'illustration d'un uuid en data URL, ou "" si absente.
func imgDataURL(uuid string) string {
	jpg := library.IllustrationJPEG(uuid)
	if len(jpg) == 0 {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
}

// ListerHistoiresPerso renvoie les histoires importees, illustration incluse.
func (a *App) ListerHistoiresPerso() []HistoirePerso {
	l := a.lirePerso()
	ref := a.persoReferencees()
	for i := range l {
		l[i].Img = imgDataURL(l[i].UUID)
		l[i].Duree = library.DureeAudio(l[i].UUID)
		l[i].Referencee = ref[l[i].UUID]
	}
	if l == nil {
		l = []HistoirePerso{}
	}
	return l
}

// ImporterHistoirePerso ouvre un selecteur de fichiers natif (selection
// MULTIPLE), importe chaque audio choisi et l'ajoute au registre. Renvoie la
// liste des histoires importees (vide si annulation). Une erreur n'est remontee
// que si aucun fichier n'a pu etre importe.
func (a *App) ImporterHistoirePerso() ([]HistoirePerso, error) {
	chemins, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Choisir un ou plusieurs fichiers audio",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Audio (mp3, m4a, aac)", Pattern: "*.mp3;*.m4a;*.aac;*.mp4"},
		},
	})
	if err != nil {
		return nil, err
	}
	out := []HistoirePerso{}
	var dernErr error
	for _, c := range chemins {
		h, e := a.importerChemin(c)
		if e != nil {
			dernErr = e
			continue
		}
		out = append(out, h)
	}
	if len(out) == 0 && dernErr != nil {
		return nil, dernErr
	}
	return out, nil
}

// importerChemin importe un fichier audio local (chemin absolu) dans la
// bibliotheque et le registre perso. Dedup par uuid (meme contenu) : met a jour
// l'entree existante en conservant sa date. Partage par le dialogue et le drop.
func (a *App) importerChemin(chemin string) (HistoirePerso, error) {
	imp, err := library.ImporterAudioLocal(chemin)
	if err != nil {
		return HistoirePerso{}, err
	}
	a.muPerso.Lock()
	l := a.lirePerso()
	h := HistoirePerso{UUID: imp.UUID, Titre: imp.Titre, Ext: imp.Ext, Taille: imp.Taille, Cover: imp.CoverPresente, Ajout: time.Now().Unix()}
	remplace := false
	for i := range l {
		if l[i].UUID == imp.UUID {
			h.Ajout = l[i].Ajout
			l[i] = h
			remplace = true
			break
		}
	}
	if !remplace {
		l = append(l, h)
	}
	a.ecrirePerso(l)
	a.muPerso.Unlock()
	h.Img = imgDataURL(h.UUID)
	return h, nil
}

// extAudioPerso indique si le chemin a une extension audio supportee.
func extAudioPerso(chemin string) bool {
	switch strings.ToLower(filepath.Ext(chemin)) {
	case ".mp3", ".m4a", ".aac", ".mp4":
		return true
	}
	return false
}

// onFileDrop : fichiers laches sur la zone "Mes histoires" (Wails renvoie les
// chemins absolus). Importe les fichiers audio, ignore le reste, et notifie le
// frontend (rafraichissement de la liste). Non bloquant pour l'IHM.
func (a *App) onFileDrop(x, y int, chemins []string) {
	n := 0
	var dernErr string
	for _, c := range chemins {
		if !extAudioPerso(c) {
			continue
		}
		if _, err := a.importerChemin(c); err != nil {
			dernErr = err.Error()
			continue
		}
		n++
	}
	wailsruntime.EventsEmit(a.ctx, "perso:depose", map[string]any{"importes": n, "erreur": dernErr})
}

// RenommerHistoirePerso change le titre d'une histoire perso. Titre vide ignore.
func (a *App) RenommerHistoirePerso(uuid, titre string) {
	titre = strings.TrimSpace(titre)
	if uuidMediaValide(uuid) != nil || titre == "" {
		return
	}
	a.muPerso.Lock()
	defer a.muPerso.Unlock()
	l := a.lirePerso()
	for i := range l {
		if l[i].UUID == uuid {
			l[i].Titre = titre
			a.ecrirePerso(l)
			return
		}
	}
}

// SupprimerHistoirePerso retire une histoire du registre et de la bibliotheque.
func (a *App) SupprimerHistoirePerso(uuid string) error {
	if err := uuidMediaValide(uuid); err != nil {
		return err
	}
	// Garde-fou : une histoire encore dans l'arbre d'une boite ne peut pas etre
	// supprimee (sinon media manquant a la publication). L'IHM grise deja le
	// bouton ; ceci couvre les autres appels.
	if a.persoReferencees()[uuid] {
		return errIHM("err_perso_referencee")
	}
	a.muPerso.Lock()
	defer a.muPerso.Unlock()
	// Medias d'abord : si un fichier resiste (verrou Windows durable), l'histoire
	// reste au registre et l'utilisateur peut reessayer, sans fichier fantome.
	if err := library.SupprimerMedia(uuid); err != nil {
		return errDetail("err_perso_suppression", err)
	}
	l := a.lirePerso()
	out := make([]HistoirePerso, 0, len(l))
	for _, h := range l {
		if h.UUID != uuid {
			out = append(out, h)
		}
	}
	a.ecrirePerso(out)
	return nil
}

// DefinirIllustrationPerso remplace l'illustration d'une histoire par l'image
// fournie en data URL (issue du picker ou d'un import).
func (a *App) DefinirIllustrationPerso(uuid, dataURL string) error {
	if err := uuidMediaValide(uuid); err != nil {
		return err
	}
	raw, err := library.DecoderDataURL(dataURL)
	if err != nil {
		return err
	}
	if err := library.DefinirIllustration(uuid, raw); err != nil {
		return err
	}
	a.muPerso.Lock()
	defer a.muPerso.Unlock()
	l := a.lirePerso()
	for i := range l {
		if l[i].UUID == uuid {
			l[i].Cover = true
			a.ecrirePerso(l)
			break
		}
	}
	return nil
}
