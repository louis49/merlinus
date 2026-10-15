package library

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/louis49/merlinus/internal/itunes"
	"github.com/louis49/merlinus/internal/store"
)

// ImportPerso decrit un audio local importe dans la bibliotheque.
type ImportPerso struct {
	UUID          string
	Ext           string // extension du media stocke : "mp3" ou "aac"
	Titre         string // depuis les tags, sinon nom de fichier
	CoverPresente bool   // une illustration a ete extraite du fichier
	Taille        int64  // octets du media stocke (apres remux eventuel)
}

// ImporterAudioLocal copie un fichier audio local dans la bibliotheque
// (media-<uuid>.<ext>), en le ramenant a un format lisible par la boite (mp3 tel
// quel, m4a remuxe en aac). Il extrait au mieux titre + cover embarquee. L'uuid
// est deterministe (sha du contenu original) : reimporter le meme fichier ne
// cree pas de doublon. La cover, si presente, est stockee en media-<uuid>.jpg.
func ImporterAudioLocal(chemin string) (ImportPerso, error) {
	data, err := os.ReadFile(chemin)
	if err != nil {
		return ImportPerso{}, err
	}
	if len(data) < 1024 {
		return ImportPerso{}, fmt.Errorf("fichier audio vide ou tronque (%d octets)", len(data))
	}
	somme := sha256.Sum256(data)
	uuid := itunes.UUIDDeterministe("perso:" + fmt.Sprintf("%x", somme))

	// sniff par extension du chemin (ct vide) : mp3 passe-plat, m4a -> aac.
	ext, media, err := normaliserAudio(chemin, "", data)
	if err != nil {
		return ImportPerso{}, err
	}
	if err := store.Save(CheminAudio(uuid, ext), media); err != nil {
		return ImportPerso{}, err
	}

	titre, cover := lireTags(strings.ToLower(filepath.Ext(chemin)), data)
	if strings.TrimSpace(titre) == "" {
		base := filepath.Base(chemin)
		titre = strings.TrimSuffix(base, filepath.Ext(base))
	}
	coverPresente := false
	if len(cover) > 0 {
		if jpg, e := JPEG128FromBytes(cover); e == nil {
			if store.Save(CheminIllustration(uuid), jpg) == nil {
				coverPresente = true
			}
		}
	}
	taille, _ := store.Size(CheminAudio(uuid, ext))
	return ImportPerso{UUID: uuid, Ext: ext, Titre: strings.TrimSpace(titre), CoverPresente: coverPresente, Taille: taille}, nil
}

// SupprimerMedia retire de la bibliotheque l'audio (mp3/aac) et l'illustration
// d'un uuid. Sans effet si les fichiers sont absents.
func SupprimerMedia(uuid string) error {
	return errors.Join(
		store.Remove(CheminAudio(uuid, "mp3")),
		store.Remove(CheminAudio(uuid, "aac")),
		store.Remove(CheminIllustration(uuid)),
	)
}

// DefinirIllustration stocke une illustration (deja en octets image) pour un
// uuid, redimensionnee au format boite. Remplace une cover extraite.
func DefinirIllustration(uuid string, image []byte) error {
	jpg, err := JPEG128FromBytes(image)
	if err != nil {
		return err
	}
	return store.Save(CheminIllustration(uuid), jpg)
}

// IllustrationJPEG renvoie l'illustration stockee d'un uuid (media-<uuid>.jpg),
// ou nil si absente.
func IllustrationJPEG(uuid string) []byte {
	d, err := store.Load(CheminIllustration(uuid))
	if err != nil {
		return nil
	}
	return d
}
