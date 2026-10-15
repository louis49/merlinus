package app

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

// evtSauvegarde : avancement de la sauvegarde complete de la boite (payload
// ProgresSauvegarde). Distinct de sync:progres (publication).
const evtSauvegarde = "sauvegarde:progres"

// dirSauvegarde : dossier (a CONSERVER) de l'image complete d'une boite,
// rapatriee de la SD par op13. Contient tous les fichiers de l'inventaire + le
// manifeste (pl.bin, arbre.json). Purge avec la boite (RemoveDir).
func dirSauvegarde(mac string) string { return dirTravailBoite(mac) + "/sauvegarde" }

// ProgresSauvegarde : etat d'avancement envoye au front pendant la sauvegarde.
type ProgresSauvegarde struct {
	Phase       string  `json:"phase"`        // libelle d'etape (ex. "Sauvegarde de la boite")
	Titre       string  `json:"titre"`        // titre du contenu en cours (si connu)
	Fichier     string  `json:"fichier"`      // nom de fichier en cours
	Faits       int     `json:"faits"`        // fichiers termines
	Total       int     `json:"total"`        // fichiers a sauvegarder
	OctetsFaits int64   `json:"octets_faits"` // octets transferes
	OctetsTotal int64   `json:"octets_total"` // octets a transferer (connu d'avance)
	Pct         float64 `json:"pct"`          // avancement en pourcentage (sur les octets)
	EtaSecondes int64   `json:"eta_secondes"` // estimation du temps restant, -1 si inconnue
}

// DefinirSauvegardeInitiale indique si la PROCHAINE association de boite doit
// rapatrier tout le contenu (sauvegarde complete) avant inscription. Pose par
// l'ecran d'accueil selon la case "Sauvegarder tout le contenu".
func (a *App) DefinirSauvegardeInitiale(oui bool) {
	a.mu.Lock()
	a.sauvegardeInitiale = oui
	a.mu.Unlock()
}

// uuidDepuisNom : uuid d'un nom de fichier de la boite (le nom entier s'il n'a
// pas d'extension).
func uuidDepuisNom(nom string) string {
	u, _ := library.UUIDDepuisNom(nom)
	return u
}

// sauvegarderBoite rapatrie TOUT l'inventaire de la SD (op13) dans
// dirSauvegarde, plus le manifeste (pl.bin, arbre.json). Les illustrations
// (.jpg) sont aussi recopiees dans le cache d'affichage. Complete ou rien : une
// erreur de transfert fait echouer la sauvegarde (la boite ne sera pas inscrite,
// cf. lireBoite). Reprise : les fichiers deja presents a la bonne taille sont
// sautes. Emet l'avancement (pourcentage sur les octets + ETA) a chaque fichier.
//
// La boite n'est pas authentifiee (un faux point d'acces MERLIN_... peut
// repondre) : les noms douteux sont ignores (nomFichierBoiteSain), le total est
// plafonne a la capacite annoncee (capaciteSD, 0 si inconnue) et chaque fichier
// recu doit avoir la taille annoncee par l'inventaire.
func (a *App) sauvegarderBoite(c *device.Client, arbre []*playlist.Node, fichiers []device.FichierInfo, bin []byte, mac string, capaciteSD uint32) error {
	// Delai genereux : lireBoite a fixe 5 s (bon pour les petites commandes), mais
	// apres plusieurs gros transferts la boite peut mettre plus de 5 s a renvoyer
	// l'EN-TETE GetFile du fichier suivant (le corps, lui, a deja 60 s dans
	// device.GetFile). Sans ca, un i/o timeout sur l'en-tete avorte la sauvegarde.
	c.SetTimeout(30 * time.Second)
	titres := playlist.IndexTitres(arbre)

	// Manifeste : pl.bin brut + arbre decode. Ecrit d'abord (petit) ; sans quoi une
	// sauvegarde reprise ne contiendrait pas forcement l'arbre si l'inventaire est
	// vide.
	if len(bin) > 0 {
		if err := store.Save(dirSauvegarde(mac)+"/pl.bin", bin); err != nil {
			return err
		}
	}
	if arbreJSON, err := json.MarshalIndent(arbre, "", "  "); err == nil {
		_ = store.Save(dirSauvegarde(mac)+"/arbre.json", arbreJSON)
	}

	fichiers = fichiersSains(fichiers)

	// Totaux connus d'avance (tailles de l'inventaire) : permet le pourcentage et
	// l'ETA. On compte TOUT l'inventaire ; les fichiers deja presents comptent
	// comme faits (octets credites sans transfert).
	var octetsTotal int64
	for _, f := range fichiers {
		octetsTotal += int64(f.Taille)
	}
	if plafond := plafondSauvegarde(capaciteSD); octetsTotal > plafond {
		return fmt.Errorf("inventaire de %d octets superieur a la capacite de la carte (%d octets)", octetsTotal, plafond)
	}
	total := len(fichiers)

	emettre := func(faits int, octetsFaits int64, titre, fichier string, debut time.Time) {
		pct := 0.0
		if octetsTotal > 0 {
			pct = float64(octetsFaits) / float64(octetsTotal) * 100
		}
		eta := int64(-1)
		if ecoule := time.Since(debut).Seconds(); ecoule > 0.5 && octetsFaits > 0 {
			debit := float64(octetsFaits) / ecoule // octets/s observes
			if debit > 0 {
				eta = int64(float64(octetsTotal-octetsFaits) / debit)
			}
		}
		a.emit(evtSauvegarde, ProgresSauvegarde{
			Phase: "Sauvegarde de la boite", Titre: titre, Fichier: fichier,
			Faits: faits, Total: total, OctetsFaits: octetsFaits, OctetsTotal: octetsTotal,
			Pct: pct, EtaSecondes: eta,
		})
	}

	debut := time.Now()
	var octetsFaits int64
	emettre(0, 0, "", "", debut)
	for i, f := range fichiers {
		dst := dirSauvegarde(mac) + "/" + f.Nom
		titre := titres[uuidDepuisNom(f.Nom)]
		// Reprise : deja present a la bonne taille -> credite sans retransfert. Une
		// illustration est quand meme (re)copiee dans le cache d'affichage, qui a
		// pu etre purge (reassociation, sauvegarde reprise d'une restauration) :
		// sans elle, l'arbre de la vue Boite s'affiche sans images.
		if sz, err := store.Size(dst); err == nil && sz == int64(f.Taille) {
			if strings.HasSuffix(strings.ToLower(f.Nom), ".jpg") && !store.Has(cheminAffichage(mac, uuidDepuisNom(f.Nom))) {
				if data, err := store.Load(dst); err == nil {
					_ = store.Save(cheminAffichage(mac, uuidDepuisNom(f.Nom)), data)
				}
			}
			octetsFaits += sz
			emettre(i+1, octetsFaits, titre, f.Nom, debut)
			continue
		}
		data, err := c.GetFileBytes(f.Nom)
		if err != nil {
			return err // complete ou rien : on abandonne, la boite ne sera pas inscrite
		}
		if len(data) != int(f.Taille) {
			return fmt.Errorf("%s : %d octets recus, %d annonces par l'inventaire", f.Nom, len(data), f.Taille)
		}
		if err := store.Save(dst, data); err != nil {
			return err
		}
		// Double-copie des illustrations dans le cache d'affichage.
		if strings.HasSuffix(strings.ToLower(f.Nom), ".jpg") {
			_ = store.Save(cheminAffichage(mac, uuidDepuisNom(f.Nom)), data)
		}
		octetsFaits += int64(len(data))
		emettre(i+1, octetsFaits, titre, f.Nom, debut)
	}
	// Manifeste : ecrit SEULEMENT si tout est rapatrie. Sa presence atteste une
	// sauvegarde complete (cf. EtatSauvegarde).
	if d, err := json.Marshal(manifesteSauvegarde{Date: time.Now().Unix(), Fichiers: total, Octets: octetsTotal}); err == nil {
		if err := store.Save(ficManifeste(mac), d); err != nil {
			return err
		}
	}
	return nil
}

// nomFichierBoiteSain : nom de fichier de la carte utilisable tel quel comme nom
// de fichier local (sauvegarde) sur tous les systemes : 1 a 64 caracteres parmi
// [A-Za-z0-9._-], sans point initial (donc ni "." ni ".."), et pas un nom
// reserve de Windows (CON, NUL, COM1..., meme suivi d'une extension). Les
// fichiers de la boite (medias <uuid>.ext, pl_enc.bin, fav.bin, wifi.cfg,
// sta_wifi.json...) le sont tous.
func nomFichierBoiteSain(nom string) bool {
	if nom == "" || len(nom) > 64 || nom[0] == '.' {
		return false
	}
	for i := 0; i < len(nom); i++ {
		b := nom[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-') {
			return false
		}
	}
	base := strings.ToUpper(nom)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

// fichiersSains ecarte (en le journalisant) tout fichier de l'inventaire dont le
// nom n'est pas sain. La restauration n'utilise que les medias et la playlist,
// toujours bien nommes : ignorer un nom douteux ne fait rien perdre d'utile.
func fichiersSains(fichiers []device.FichierInfo) []device.FichierInfo {
	out := make([]device.FichierInfo, 0, len(fichiers))
	for _, f := range fichiers {
		if nomFichierBoiteSain(f.Nom) {
			out = append(out, f)
		} else {
			log.Printf("sauvegarde : fichier au nom douteux ignore (%q)", f.Nom)
		}
	}
	return out
}

// plafondSauvegarde : total maximal rapatriable, la capacite annoncee de la
// carte (op4) ; a defaut, le maximum que ce champ u32 peut annoncer (4 Gio).
func plafondSauvegarde(capaciteSD uint32) int64 {
	if capaciteSD == 0 {
		return 1 << 32
	}
	return int64(capaciteSD)
}

// manifesteSauvegarde : temoin d'une sauvegarde complete.
type manifesteSauvegarde struct {
	Date     int64 `json:"date"`     // fin de la sauvegarde (unix)
	Fichiers int   `json:"fichiers"` // fichiers de la carte SD rapatries
	Octets   int64 `json:"octets"`
}

func ficManifeste(mac string) string { return dirSauvegarde(mac) + "/manifeste.json" }

// EtatSauvegarde : ce que l'IHM affiche dans "Retirer la boite de Merlinus".
type EtatSauvegarde struct {
	Disponible bool  `json:"disponible"`
	Date       int64 `json:"date"` // 0 si inconnue
	Fichiers   int   `json:"fichiers"`
	Octets     int64 `json:"octets"`
}

// EtatSauvegarde indique si une sauvegarde complete de la boite existe. Le
// manifeste fait foi ; a defaut (sauvegardes anterieures au manifeste), un
// dossier contenant l'arbre et la playlist est reconnu, date = celle de l'arbre.
// Une sauvegarde interrompue n'a ni manifeste ni inscription de la boite, et
// son reste est efface si la boite est ensuite ajoutee sans sauvegarde.
func (a *App) EtatSauvegarde(mac string) EtatSauvegarde {
	if d, err := store.Load(ficManifeste(mac)); err == nil {
		var m manifesteSauvegarde
		if json.Unmarshal(d, &m) == nil {
			return EtatSauvegarde{Disponible: true, Date: m.Date, Fichiers: m.Fichiers, Octets: m.Octets}
		}
	}
	dir := dirSauvegarde(mac)
	if !store.Has(dir+"/arbre.json") || !(store.Has(dir+"/pl_enc.bin") || store.Has(dir+"/pl.bin")) {
		return EtatSauvegarde{}
	}
	e := EtatSauvegarde{Disponible: true}
	for _, taille := range fichiersSauvegarde(mac) { // fichiers de la boite seulement
		e.Fichiers++
		e.Octets += taille
	}
	if t, err := store.ModTime(dir + "/arbre.json"); err == nil {
		e.Date = t.Unix()
	}
	return e
}
