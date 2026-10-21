package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/itunes"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/radiofrance"
	"github.com/louis49/merlinus/internal/store"
	"github.com/louis49/merlinus/internal/webradio"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Persistance CLE PAR MAC : chaque boite a ses propres fichiers. Le registre
// liste les boites connues.
// Arborescence du dossier de donnees, deux racines :
//
//	cache/   PURGEABLE (re-derivable : re-telechargement web ou relecture boite)
//	travail/ A CONSERVER (source de verite, donnees utilisateur)
//
// Chaque boite a son dossier (le MAC est porte par le dossier, plus par le nom).
const ficRegistre = "travail/boites.json"

// macKey normalise une MAC en identifiant de fichier (hex, sans separateurs).
// Ne garde que [0-9a-z] : la cle sert de nom de dossier, elle ne doit jamais
// porter de separateur ni de "..". Jamais vide : "" donnerait le dossier PARENT
// de toutes les boites (un RemoveDir les purgerait toutes).
func macKey(mac string) string {
	k := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, strings.ToLower(mac))
	if k == "" {
		return "_"
	}
	return k
}

// memeMAC compare deux identifiants de boite sous leur forme normalisee
// (macKey) : seule comparaison a utiliser (casse, separateurs).
func memeMAC(a, b string) bool { return macKey(a) == macKey(b) }

// uuidMediaValide refuse un uuid qui ne peut pas etre un nom de media de la
// bibliotheque (chemin, separateur...). Les bindings le recoivent du front.
func uuidMediaValide(uuid string) error {
	if !reUUIDMedia.MatchString(uuid) {
		return errIHM("err_identifiant_invalide", "detail", uuid)
	}
	return nil
}

func dirTravailBoite(mac string) string { return "travail/boites/" + macKey(mac) }
func dirCacheBoite(mac string) string   { return "cache/boites/" + macKey(mac) }

func ficArbre(mac string) string     { return dirCacheBoite(mac) + "/arbre.json" }
func ficRapport(mac string) string   { return dirCacheBoite(mac) + "/rapport.json" }
func ficTailles(mac string) string   { return dirCacheBoite(mac) + "/tailles.json" }
func ficFavoris(mac string) string   { return dirCacheBoite(mac) + "/favoris.json" }
func dirAffichage(mac string) string { return dirCacheBoite(mac) + "/affichage" }
func dirPl(mac string) string        { return dirCacheBoite(mac) + "/pl" }

// cheminAffichage : illustration d'arbre en cache d'affichage ({uuid}.jpg).
func cheminAffichage(mac, uuid string) string { return dirAffichage(mac) + "/" + uuid + ".jpg" }

// Evenements pousses vers le frontend pendant l'accueil.
const (
	evtAttente    = "boite:attente"    // en attente de la boite en AP
	evtConnectee  = "boite:connectee"  // boite jointe, lecture en cours
	evtProgres    = "boite:progres"    // message d'avancement (string)
	evtImages     = "boite:images"     // avancement du téléchargement des illustrations
	evtInventaire = "boite:inventaire" // avancement de l'inventaire des fichiers (op12), payload EtatImages
	evtPret       = "boite:pret"       // termine, payload = ComptePret
	evtErreur     = "boite:erreur"     // erreur (string), la veille continue

	// Seul sync:progres est consomme par l'IHM (barre d'avancement) ; la fin et
	// l'erreur de publication passent par la valeur de retour de la fonction Go
	// (Promise cote front), pas par un evenement.
	evtSyncProgres = "sync:progres" // avancement chiffre (payload ProgresSync)
)

// EtatImages : avancement du téléchargement des illustrations.
type EtatImages struct {
	Fait  int `json:"fait"`
	Total int `json:"total"`
}

// App porte l'etat applicatif et le contexte Wails (pour emettre des events).
type App struct {
	ctx context.Context

	it *itunes.Client
	rf *radiofrance.Client

	mu          sync.Mutex
	enVeille    bool
	arretVeille chan struct{} // ferme pour demander l'arret de la goroutine de veille

	// sauvegardeInitiale : vrai quand la prochaine lecture de boite (association)
	// doit rapatrier TOUT le contenu de la SD (sauvegarde complete, cf.
	// sauvegarde.go) avant d'inscrire la boite. Pose par l'ecran d'accueil, remis
	// a faux par la relecture d'une boite deja connue. Lu/ecrit sous mu.
	sauvegardeInitiale bool

	// connMu serialise TOUT acces reseau a la boite. La connexion TCP
	// (192.168.4.1:50000) est mono-usage : deux echanges concurrents la
	// cassent. Les operations exclusives (publication, restauration) le
	// prennent et refusent si occupe ; les sondes (EtatBoite, veille) tentent
	// un TryLock et sautent leur tour sans jamais bloquer.
	connMu sync.Mutex

	// Verrous dedies aux read-modify-write des fichiers JSON du store (ecrivains
	// concurrents : veille, publication, IHM). Un verrou par fichier logique.
	muRegistre sync.Mutex // registre des boites (ficRegistre)
	muSuivis   sync.Mutex // suivis-<mac>.json
	muPerso    sync.Mutex // perso.json

	sonde joignabilite // delai de detection vu par EtatBoite (sous connMu)
}

// NewApp cree l'application.
func NewApp() *App {
	return &App{
		it:    itunes.New(),
		rf:    radiofrance.New(),
		sonde: joignabilite{origine: "sonde"},
	}
}

// startup memorise le contexte Wails, requis pour EventsEmit et l'appel des
// methodes liees.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Menage local (bibliotheque orpheline, cache d'images plafonne) : avant que
	// l'IHM ne puisse lancer un telechargement.
	a.menageDemarrage()
	// Fichiers laches sur la zone "Mes histoires" : import direct par chemin.
	wailsruntime.OnFileDrop(ctx, a.onFileDrop)
}

// ComptePret est le payload de l'evenement de fin d'accueil.
type ComptePret struct {
	Rapport       playlist.Report  `json:"rapport"`
	Firmware      string           `json:"firmware,omitempty"`
	Batterie      *device.Batterie `json:"batterie,omitempty"`
	StockageTotal uint32           `json:"stockage_total,omitempty"`
	StockageLibre uint32           `json:"stockage_libre,omitempty"`
	SSID          string           `json:"ssid,omitempty"`
	MAC           string           `json:"mac,omitempty"`
}

// BoiteMeta : identite persistee d'une boite connue (registre).
type BoiteMeta struct {
	MAC           string `json:"mac"`
	SSID          string `json:"ssid"`
	Nom           string `json:"nom"`
	Firmware      string `json:"firmware,omitempty"`
	StockageTotal uint32 `json:"stockage_total,omitempty"`
	StockageLibre uint32 `json:"stockage_libre,omitempty"`
	DerniereSync  int64  `json:"derniere_sync,omitempty"` // Unix, mis a jour a chaque publication reussie
	// WifiMaison : SSID du Wi-Fi domestique (sta_wifi.json), "" si aucun. A ne
	// pas confondre avec SSID (point d'acces de la boite, wifi.cfg / op17).
	WifiMaison string `json:"wifi_maison,omitempty"`
}

// Boite : boite connue enrichie de son dernier compte rendu (pour l'IHM).
type Boite struct {
	BoiteMeta
	Rapport *playlist.Report `json:"rapport,omitempty"`
}

func (a *App) lireRegistre() []BoiteMeta {
	data, err := store.Load(ficRegistre)
	if err != nil {
		return nil
	}
	var l []BoiteMeta
	_ = json.Unmarshal(data, &l)
	return l
}

// modifierRegistre applique fn au registre sous verrou, puis l'ecrit. Un
// registre present mais indecodable n'est JAMAIS reecrit (on y perdrait les
// autres boites) : erreur journalisee, rien n'est modifie.
func (a *App) modifierRegistre(fn func(l []BoiteMeta) []BoiteMeta) {
	a.muRegistre.Lock()
	defer a.muRegistre.Unlock()
	data, err := store.Load(ficRegistre)
	if err != nil {
		data = nil // absent (premiere utilisation) : registre vide
	}
	sortie, err := modifierListe(data, fn)
	if err != nil {
		log.Printf("registre des boites non modifie : %v", err)
		return
	}
	_ = store.Save(ficRegistre, sortie)
}

// modifierListe : coeur pur de modifierRegistre (decodage, fn, encodage).
func modifierListe(data []byte, fn func(l []BoiteMeta) []BoiteMeta) ([]byte, error) {
	var l []BoiteMeta
	if len(data) > 0 {
		if err := json.Unmarshal(data, &l); err != nil {
			return nil, fmt.Errorf("registre illisible : %w", err)
		}
	}
	return json.Marshal(fn(l))
}

// majBoite applique maj a l'entree de la boite mac, si elle existe.
func (a *App) majBoite(mac string, maj func(m *BoiteMeta)) {
	a.modifierRegistre(func(l []BoiteMeta) []BoiteMeta {
		for i := range l {
			if memeMAC(l[i].MAC, mac) {
				maj(&l[i])
			}
		}
		return l
	})
}

// majMeta insere ou met a jour une boite dans le registre, en conservant le nom
// choisi par l'utilisateur.
func (a *App) majMeta(m BoiteMeta) {
	a.modifierRegistre(func(l []BoiteMeta) []BoiteMeta {
		for i := range l {
			if memeMAC(l[i].MAC, m.MAC) {
				m.Nom = l[i].Nom                   // le nom choisi par l'utilisateur est conserve
				m.DerniereSync = l[i].DerniereSync // la date de derniere synchro n'est pas ecrasee par une relecture
				l[i] = m
				return l
			}
		}
		return append(l, m)
	})
}

// marquerSync enregistre l'instant de la derniere publication reussie d'une boite.
func (a *App) marquerSync(mac string) {
	a.majBoite(mac, func(m *BoiteMeta) { m.DerniereSync = time.Now().Unix() })
}

// Stockage : capacite et espace libre de la carte, tels que la boite les annonce.
type Stockage struct {
	Total uint32 `json:"total"`
	Libre uint32 `json:"libre"`
}

// noterStockage relit capacite (op4) et espace libre (op3) et les inscrit au
// registre : la jauge de l'IHM en derive. Appele apres toute operation qui
// change le contenu de la carte (publication) et a chaque connexion
// (LireStockage), sinon la jauge resterait figee au releve de l'association.
// Lecture impossible : registre inchange, nil.
func (a *App) noterStockage(c *device.Client, mac string) *Stockage {
	total, err := c.SDSize()
	if err != nil {
		return nil
	}
	libre, err := c.SizeAvailable()
	if err != nil {
		return nil
	}
	a.majBoite(mac, func(m *BoiteMeta) { m.StockageTotal, m.StockageLibre = total, libre })
	return &Stockage{Total: total, Libre: libre}
}

// LireStockage releve le stockage de la boite au passage deconnecte -> connecte
// (suit aussi les ecritures de l'app officielle). nil si la boite est occupee,
// injoignable ou si ce n'est pas la boite attendue : l'IHM reessaiera.
func (a *App) LireStockage(mac string) *Stockage {
	var st *Stockage
	_ = a.sonderBoite(mac, func(c *device.Client) error {
		st = a.noterStockage(c, mac)
		return nil
	})
	return st
}

// EstConfigure indique qu'au moins une boite est connue (accueil deja realise).
func (a *App) EstConfigure() bool { return len(a.lireRegistre()) > 0 }

// macConnue indique si une MAC (normalisee) est deja au registre.
func (a *App) macConnue(cle string) bool {
	if cle == "" {
		return false
	}
	for _, m := range a.lireRegistre() {
		if memeMAC(m.MAC, cle) {
			return true
		}
	}
	return false
}

// Boites renvoie les boites connues, chacune avec son dernier compte rendu,
// pour alimenter le selecteur multi-boite.
func (a *App) Boites() []Boite {
	out := []Boite{}
	for _, m := range a.lireRegistre() {
		b := Boite{BoiteMeta: m}
		if data, err := store.Load(ficRapport(m.MAC)); err == nil {
			var r playlist.Report
			if json.Unmarshal(data, &r) == nil {
				b.Rapport = &r
			}
		}
		out = append(out, b)
	}
	return out
}

// NommerBoite fixe le nom convivial d'une boite (prenom de l'enfant).
func (a *App) NommerBoite(mac, nom string) {
	a.majBoite(mac, func(m *BoiteMeta) { m.Nom = nom })
}

// SupprimerBoite retire la boite (RetirerBoite : registre, travail, cache) et
// efface en plus sa sauvegarde. Les archives de restauration sont gardees (au
// cas ou) ; la bibliotheque est globale : intacte.
func (a *App) SupprimerBoite(mac string) {
	a.RetirerBoite(mac)
	store.RemoveDir(dirSauvegarde(mac))
}

// ResultatMedia : issue d'un telechargement de media (audio ou illustration).
type ResultatMedia struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"` // message explicite si !OK (ex. m4a non convertible)
}

// TelechargerEpisode recupere l'audio (mp3, ou m4a remuxe en aac) et la vignette
// 128 d'un episode dans la bibliotheque locale. Appele au clic "ajouter".
func (a *App) TelechargerEpisode(uuid, audioURL, imageURL string) ResultatMedia {
	if err := uuidMediaValide(uuid); err != nil {
		return ResultatMedia{Message: err.Error()}
	}
	if _, err := library.PreparerEpisode(uuid, audioURL, imageURL); err != nil {
		return ResultatMedia{Message: errDetail("err_telechargement", err).Error()}
	}
	return ResultatMedia{OK: true}
}

// TelechargerImageSerie recupere l'illustration d'une serie (si absente).
func (a *App) TelechargerImageSerie(uuid, imageURL string) ResultatMedia {
	if err := uuidMediaValide(uuid); err != nil {
		return ResultatMedia{Message: err.Error()}
	}
	if err := library.PreparerImage(uuid, imageURL); err != nil {
		return ResultatMedia{Message: errDetail("err_telechargement", err).Error()}
	}
	return ResultatMedia{OK: true}
}

// EtatsUUID renvoie, pour une boite, les uuid par etat : deja sur la boite,
// en attente d'ajout (_new), en attente de suppression (_rem). Le catalogue en
// derive les indicateurs (histoire/serie). Les cascades sont prises en compte.
type EtatsUUID struct {
	SurBoite     []string `json:"surBoite"`
	AttenteAjout []string `json:"attenteAjout"`
	AttenteSuppr []string `json:"attenteSuppr"`
	Favoris      []string `json:"favoris"` // UUID favoris lus de fav.bin (lecture seule)
}

func (a *App) EtatsBoite(mac string) EtatsUUID {
	e := EtatsUUID{SurBoite: []string{}, AttenteAjout: []string{}, AttenteSuppr: []string{}, Favoris: []string{}}
	if data, err := store.Load(ficFavoris(mac)); err == nil {
		_ = json.Unmarshal(data, &e.Favoris)
		if e.Favoris == nil {
			e.Favoris = []string{}
		}
	}
	if data, err := store.Load(ficArbre(mac)); err == nil {
		var arbre []*playlist.Node
		if json.Unmarshal(data, &arbre) == nil {
			playlist.Parcourir(arbre, func(n, _ *playlist.Node) {
				if n.UUID != "" {
					e.SurBoite = append(e.SurBoite, n.UUID)
				}
			})
		}
	}
	if data, err := store.Load(ficTravail(mac)); err == nil {
		var travail []wNode
		if json.Unmarshal(data, &travail) == nil {
			var walk func(ns []wNode, sousNew, sousRem bool)
			walk = func(ns []wNode, sousNew, sousRem bool) {
				for _, w := range ns {
					n, r := sousNew || w.New, sousRem || w.Rem
					if w.UUID != "" {
						switch {
						case r:
							e.AttenteSuppr = append(e.AttenteSuppr, w.UUID)
						case n:
							e.AttenteAjout = append(e.AttenteAjout, w.UUID)
						}
					}
					if w.Child != nil {
						walk(*w.Child, n, r)
					}
				}
			}
			walk(travail, false, false)
		}
	}
	return e
}

// EtatConnexion decrit si une boite est joignable en mode AP, et si oui son
// identite (MAC) et l'etat live de sa batterie. Sert a l'indicateur de
// connexion et au niveau de charge dans l'IHM.
type EtatConnexion struct {
	Joignable bool             `json:"joignable"`
	MAC       string           `json:"mac,omitempty"`
	Batterie  *device.Batterie `json:"batterie,omitempty"`
	Occupee   bool             `json:"occupee,omitempty"` // boite la, mais tenue par une autre operation
}

// EtatBoite sonde l'adresse AP par defaut : ouvre une connexion courte, ping,
// puis lit MAC et batterie. Lecture seule, sans effet de bord. Renvoie
// Joignable=false des que la boite n'est pas la (cas nominal hors mode AP).
func (a *App) EtatBoite() EtatConnexion {
	// Connexion mono-usage : si une operation exclusive (publication, veille)
	// tient la boite, on ne sonde pas (une 2e socket vers :50000 casserait
	// l'echange). On rapporte "occupee" (et NON joignable) : l'IHM ne doit pas
	// marquer la boite active "connectee" ni deverrouiller la synchro sur cette
	// base.
	if !a.connMu.TryLock() {
		return EtatConnexion{Occupee: true}
	}
	defer a.connMu.Unlock()
	c, err := device.Dial(device.DefaultAddr(), 1200*time.Millisecond)
	if err != nil {
		a.sonde.echec(err)
		return EtatConnexion{Joignable: false}
	}
	defer c.Close()
	c.SetTimeout(2 * time.Second)
	if err := c.Ping(); err != nil {
		a.sonde.echec(fmt.Errorf("ping : %w", err))
		return EtatConnexion{Joignable: false}
	}
	a.sonde.succes()
	e := EtatConnexion{Joignable: true}
	if m, err := c.MAC(); err == nil {
		e.MAC = m
	}
	if b, err := c.Battery(); err == nil {
		e.Batterie = &b
	}
	return e
}

// LireHoraires relit les plages configurees sur la boite. Appelee par l'IHM au
// passage deconnecte -> connecte (pas a chaque sonde). Renvoie nil sans erreur
// si la boite est occupee, injoignable, ou si ce n'est pas la boite attendue :
// l'IHM reessaiera au prochain passage.
func (a *App) LireHoraires(mac string) (*device.Horaires, error) {
	var res *device.Horaires
	_ = a.sonderBoite(mac, func(c *device.Client) error {
		h, err := c.LireHoraires()
		if err == nil {
			res = &h
		}
		return err
	})
	return res, nil
}

// EjecterBoite termine proprement la session avec la boite (op9 EndSync) : elle
// quitte le mode point d'acces et le Mac retrouve son Wi-Fi habituel. Aucune
// donnee modifiee. Refuse si une autre operation tient la connexion (l'IHM
// reessaie) ou si la boite jointe n'est pas celle attendue.
func (a *App) EjecterBoite(mac string) error {
	return a.sonderBoite(mac, func(c *device.Client) error { return c.EndSync() })
}

// LireFavoris lit fav.bin sur la boite (favoris poses par l'enfant, LECTURE
// SEULE : seule la restauration les remet a l'etat d'origine, cf.
// restaurerFavoris), le decode et persiste la liste
// d'UUID dans favoris.json. Calquee sur LireHoraires : l'IHM l'appelle au passage
// deconnecte -> connecte (pas a chaque sonde). Renvoie nil sans erreur si la
// boite est occupee, injoignable, ou si ce n'est pas la boite attendue. Un
// fav.bin VIDE (0 octet) donne une liste vide (favoris effaces par l'enfant) ; un
// fav.bin absent/illisible laisse la liste persistee inchangee (pas d'ecrasement
// sur erreur transitoire).
func (a *App) LireFavoris(mac string) ([]string, error) {
	var fav []string
	_ = a.sonderBoite(mac, func(c *device.Client) error {
		bin, err := c.GetFileBytes("fav.bin")
		if err != nil {
			return err // absent/illisible : on garde la liste precedente
		}
		fav = playlist.FavorisDepuisBin(bin)
		if d, e := json.Marshal(fav); e == nil {
			_ = store.Save(ficFavoris(mac), d)
		}
		return nil
	})
	return fav, nil
}

// enregistrerTailles persiste la taille effective (octets) de chaque fichier de
// la boite (index par nom, ex. "<uuid>.mp3"). Journalise un controle : somme
// des tailles vs espace occupe rapporte par la boite (total - libre).
func (a *App) enregistrerTailles(mac string, fichiers []device.FichierInfo, total, libre uint32) {
	m := make(map[string]uint32, len(fichiers))
	var somme uint64
	for _, f := range fichiers {
		m[f.Nom] = f.Taille
		somme += uint64(f.Taille)
	}
	if d, err := json.Marshal(m); err == nil {
		_ = store.Save(ficTailles(mac), d)
	}
	occupe := int64(total) - int64(libre)
	log.Printf("inventaire boite %s : %d fichiers, somme=%d o, occupe(boite)=%d o, ecart=%d o",
		mac, len(fichiers), somme, occupe, occupe-int64(somme))
}

// taillesParUUID charge l'inventaire persiste et cumule par uuid (audio +
// vignette du meme contenu). Cle = uuid (nom de fichier sans extension).
func taillesParUUID(mac string) map[string]int64 {
	out := map[string]int64{}
	data, err := store.Load(ficTailles(mac))
	if err != nil {
		return out
	}
	var m map[string]uint32
	if json.Unmarshal(data, &m) != nil {
		return out
	}
	for nom, taille := range m {
		uuid, ok := library.UUIDDepuisNom(nom)
		if !ok {
			continue // pas de "<uuid>.<ext>"
		}
		out[uuid] += int64(taille)
	}
	return out
}

// fusionInventaire met a jour l'inventaire des tailles (nom de fichier -> octets)
// apres une publication, SANS re-scan de la boite :
//   - retire les medias (.mp3/.aac/.jpg) dont l'uuid n'est plus reference dans
//     l'arbre publie (ils ont ete supprimes par clean_sd.cmd) ;
//   - (re)pose la taille des fichiers televerses (medias uniquement) ;
//   - preserve tout le reste (fichiers systeme : fav.bin, pl_enc.bin, etc.).
func fusionInventaire(inv map[string]uint32, refUUID map[string]bool, televerses map[string]int64) map[string]uint32 {
	if inv == nil {
		inv = map[string]uint32{}
	}
	estMedia := func(nom string) bool {
		return strings.HasSuffix(nom, ".mp3") || strings.HasSuffix(nom, ".aac") || strings.HasSuffix(nom, ".jpg")
	}
	for nom := range inv {
		if !estMedia(nom) {
			continue
		}
		if u, ok := library.UUIDDepuisNom(nom); ok && !refUUID[u] {
			delete(inv, nom)
		}
	}
	for nom, t := range televerses {
		if estMedia(nom) {
			inv[nom] = uint32(t)
		}
	}
	return inv
}

// TaillesArbre renvoie la taille (octets) de chaque contenu indexee par uuid,
// pour l'affichage dans l'arbre : les contenus DEJA sur la boite prennent leur
// taille reelle d'inventaire ; les contenus AJOUTES localement (_new, pas encore
// sur la boite) prennent leur taille en bibliotheque (audio + vignette). Ainsi
// chaque noeud de l'arbre de travail a une taille, qu'il soit publie ou en attente.
func (a *App) TaillesArbre(mac string) map[string]int {
	out := map[string]int{}
	for u, t := range taillesParUUID(mac) { // 1. inventaire reel de la boite
		out[u] = int(t)
	}
	// 2. Repli bibliotheque : pour tout media telecharge localement et absent de
	// l'inventaire (ex. ajoute apres le dernier inventaire, relu seulement a
	// l'accueil), on affiche la taille locale (identique a celle poussee).
	// Couvre entierement les contenus _new (ils ont un media-<uuid>.* en
	// bibliotheque) : pas besoin de parcourir l'arbre de travail en plus.
	for u, t := range library.TaillesLocales() {
		if _, deja := out[u]; !deja {
			out[u] = int(t)
		}
	}
	return out
}

// tailleMediaLib renvoie la taille en octets des fichiers bibliotheque d'un
// media (audio + vignette), 0 si absents.
func tailleMediaLib(uuid string) int64 {
	var s int64
	if ext := library.ExtensionAudio(uuid); ext != "" {
		if n, e := store.Size(library.CheminAudio(uuid, ext)); e == nil {
			s += n
		}
	}
	if n, e := store.Size(library.CheminIllustration(uuid)); e == nil {
		s += n
	}
	return s
}

// EstimationStockage : differentiel de la prochaine synchro, en Mo. Ajouts (_new)
// mesures sur les fichiers reels en bibliotheque (audio + vignette) ; retraits
// (_rem, cascade) credites depuis l'inventaire des tailles reelles de la boite.
type EstimationStockage struct {
	AjoutMo   int `json:"ajoutMo"`
	RetraitMo int `json:"retraitMo"`
}

// EstimationStockage calcule le differentiel a partir de l'arbre de travail. Sans
// arbre de travail (aucune edition), renvoie 0/0. Calculable hors ligne.
func (a *App) EstimationStockage(mac string) EstimationStockage {
	data, err := store.Load(ficTravail(mac))
	if err != nil || len(data) == 0 {
		return EstimationStockage{}
	}
	var travail []wNode
	if json.Unmarshal(data, &travail) != nil {
		return EstimationStockage{}
	}
	tailles := taillesParUUID(mac) // uuid -> octets sur la boite (pour les retraits)
	var ajout, retrait int64
	vus := map[string]bool{}
	var walk func(ns []wNode, sousNew, sousRem bool)
	walk = func(ns []wNode, sousNew, sousRem bool) {
		for _, n := range ns {
			estNew := sousNew || n.New
			estRem := sousRem || n.Rem
			if n.Child == nil { // feuille (histoire)
				if n.UUID == "" || vus[n.UUID] {
					continue
				}
				vus[n.UUID] = true
				if estRem {
					retrait += tailles[n.UUID] // taille reelle sur la boite
				} else if estNew {
					ajout += tailleMediaLib(n.UUID) // taille reelle en bibliotheque
				}
				continue
			}
			// conteneur ajoute (serie, categorie, radio) : son illustration a pousser,
			// celle de l'arbre (_img, re-encodee comme a la publication) sinon celle
			// de la bibliotheque.
			if n.New && !estRem && n.UUID != "" && !vus[n.UUID] {
				vus[n.UUID] = true
				if n.Img != "" {
					if jpg, e := library.JPEG128DepuisDataURL(n.Img); e == nil {
						ajout += int64(len(jpg))
					}
				} else if s, e := store.Size(library.CheminIllustration(n.UUID)); e == nil {
					ajout += s
				}
			}
			walk(*n.Child, estNew, estRem)
		}
	}
	walk(travail, false, false)
	const Mo = 1024 * 1024
	return EstimationStockage{AjoutMo: int(ajout / Mo), RetraitMo: int(retrait / Mo)}
}

// ---- Accueil : series suivies dont le catalogue a de nouveaux episodes ----

func ficSuivis(mac string) string { return dirTravailBoite(mac) + "/suivis.json" }

// SuiviSerie : identite catalogue d'une serie suivie (persistee, survit aux
// synchros contrairement a l'arbre de travail).
type SuiviSerie struct {
	Src   string `json:"src"`
	SrcId string `json:"srcId"`
	Img   string `json:"img"`
	Title string `json:"title"`
}

// EnregistrerSuivi memorise (par MAC) l'identite catalogue d'une serie suivie.
// Appele par le front a chaque ajout d'une histoire/serie.
func (a *App) EnregistrerSuivi(mac, uuid, src, srcId, img, title string) {
	if uuid == "" || src == "" || srcId == "" {
		return
	}
	a.muSuivis.Lock()
	defer a.muSuivis.Unlock()
	m := map[string]SuiviSerie{}
	if d, e := store.Load(ficSuivis(mac)); e == nil {
		_ = json.Unmarshal(d, &m)
	}
	cur := m[uuid]
	if cur.Img == "" {
		cur.Img = img
	}
	m[uuid] = SuiviSerie{Src: src, SrcId: srcId, Img: firstNonEmpty(img, cur.Img), Title: title}
	if d, e := json.Marshal(m); e == nil {
		_ = store.Save(ficSuivis(mac), d)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type cacheSerie struct {
	Ts    int64    `json:"ts"`
	Uuids []string `json:"uuids"`
}

// uuidsCatalogue renvoie les uuids des episodes d'une serie du catalogue, avec un
// cache disque de 24 h (les acces web sont couteux, surtout RF).
func (a *App) uuidsCatalogue(src, id string) ([]string, error) {
	f := "cache/series/" + itunes.UUIDDeterministe("cache:"+src+":"+id) + ".json"
	if d, e := store.Load(f); e == nil {
		var c cacheSerie
		if json.Unmarshal(d, &c) == nil && time.Now().Unix()-c.Ts < 24*3600 {
			return c.Uuids, nil
		}
	}
	var feed itunes.Feed
	var err error
	if src == "radiofrance" {
		feed, err = a.rf.Feed(id)
	} else {
		feed, err = a.it.Feed(id)
	}
	if err != nil {
		return nil, err
	}
	uuids := make([]string, 0, len(feed.Episodes))
	for _, e := range feed.Episodes {
		if e.UUID != "" {
			uuids = append(uuids, e.UUID)
		}
	}
	if d, e := json.Marshal(cacheSerie{Ts: time.Now().Unix(), Uuids: uuids}); e == nil {
		_ = store.Save(f, d)
	}
	return uuids, nil
}

// seriesSuivies : les series de l'annuaire suivis.json encore dans l'arbre,
// selon la regle du catalogue (serieSuivie cote front) : sur la boite (meme
// marquee a retirer, elle y est encore) ou en attente d'ajout. L'annuaire ne
// fait que grandir (identite catalogue gardee si la serie revient) : il ne
// definit pas, a lui seul, ce qui est suivi.
func seriesSuivies(annuaire map[string]SuiviSerie, e EtatsUUID) map[string]SuiviSerie {
	dansArbre := map[string]bool{}
	for _, u := range e.SurBoite {
		dansArbre[u] = true
	}
	for _, u := range e.AttenteAjout {
		dansArbre[u] = true
	}
	out := map[string]SuiviSerie{}
	for u, s := range annuaire {
		if dansArbre[u] {
			out[u] = s
		}
	}
	return out
}

// SerieNouveaute : une serie suivie ayant N nouveaux episodes cote catalogue.
type SerieNouveaute struct {
	UUID   string `json:"uuid"`
	Title  string `json:"title"`
	Src    string `json:"src"`
	SrcId  string `json:"srcId"`
	Img    string `json:"img"`
	NbNouv int    `json:"nbNouv"`
}

// NouveautesAccueil : pour chaque serie suivie, compte les episodes du catalogue
// absents de la boite (et non deja en attente d'ajout). Acces web caches 24 h,
// series interrogees en parallele borne.
func (a *App) NouveautesAccueil(mac string) []SerieNouveaute {
	out := []SerieNouveaute{}
	suivis := map[string]SuiviSerie{}
	if d, e := store.Load(ficSuivis(mac)); e == nil {
		_ = json.Unmarshal(d, &suivis)
	}
	suivis = seriesSuivies(suivis, a.EtatsBoite(mac))
	if len(suivis) == 0 {
		return out
	}

	// UUIDs deja presents (boite) ou deja en attente (travail _new) : pas "nouveaux".
	presents := map[string]bool{}
	if d, e := store.Load(ficArbre(mac)); e == nil {
		var arbre []*playlist.Node
		if json.Unmarshal(d, &arbre) == nil {
			for u := range playlist.Feuilles(arbre) {
				presents[u] = true
			}
		}
	}
	if d, e := store.Load(ficTravail(mac)); e == nil {
		var trav []wNode
		if json.Unmarshal(d, &trav) == nil {
			var walk func([]wNode, bool)
			walk = func(ns []wNode, sn bool) {
				for _, n := range ns {
					nw := sn || n.New
					if n.Child == nil {
						if nw {
							presents[n.UUID] = true
						}
					} else {
						walk(*n.Child, nw)
					}
				}
			}
			walk(trav, false)
		}
	}

	type res struct {
		nv SerieNouveaute
		ok bool
	}
	ch := make(chan res, len(suivis))
	sem := make(chan struct{}, 4) // parallelisme borne (les Feed RF sont lents)
	n := 0
	for uuid, s := range suivis {
		n++
		go func(uuid string, s SuiviSerie) {
			sem <- struct{}{}
			defer func() { <-sem }()
			uuids, err := a.uuidsCatalogue(s.Src, s.SrcId)
			if err != nil {
				ch <- res{ok: false}
				return
			}
			nb := 0
			for _, u := range uuids {
				if !presents[u] {
					nb++
				}
			}
			ch <- res{nv: SerieNouveaute{UUID: uuid, Title: s.Title, Src: s.Src, SrcId: s.SrcId, Img: s.Img, NbNouv: nb}, ok: nb > 0}
		}(uuid, s)
	}
	for i := 0; i < n; i++ {
		if r := <-ch; r.ok {
			out = append(out, r.nv)
		}
	}
	return out
}

// lirePlaylistBoite recupere et decode la playlist de la boite (voir
// choisirPlaylist pour l'ordre). Renvoie les enregistrements decodes et les
// octets DECODES (clair), utiles pour la taille du compte rendu et la
// sauvegarde. Le pl_enc.bin chiffre d'origine est capture a part par la
// sauvegarde complete (inventaire SD).
func (a *App) lirePlaylistBoite(c *device.Client) ([]playlist.Item, []byte, error) {
	existe := func(nom string) (bool, error) {
		p, _, err := c.IsFileExist(nom, nil)
		return p, err
	}
	dechiffrer := func(enc []byte) ([]byte, error) {
		mac, err := c.MACBytes()
		if err != nil {
			return nil, fmt.Errorf("lecture du MAC (cle de dechiffrement) impossible : %w", err)
		}
		return device.DechiffrerPlaylist(mac, enc)
	}
	return choisirPlaylist(c.GetFileBytes, existe, dechiffrer)
}

// choisirPlaylist lit la playlist dans l'ordre du FIRMWARE (PROTOCOLE-2.1.23.md
// section 7) :
//   - pl_enc.bin (playlist CHIFFRE, pose par l'app officielle et par Merlinus :
//     AES-256-GCM, cle = SHA-256 du MAC op30, clair au format DecodeV2) ;
//   - pl.bin (enregistrements 96 o en clair), seulement si pl_enc.bin est ABSENT.
//
// Un pl.bin perime peut survivre a cote du pl_enc.bin publie : le lire en
// premier ferait republier un arbre perime, puis clean_sd.cmd effacerait les
// medias ajoutes depuis. Pour la meme raison, un pl_enc.bin present mais
// illisible (coupure reseau) est une erreur, pas un repli sur pl.bin.
func choisirPlaylist(lire func(string) ([]byte, error), existe func(string) (bool, error), dechiffrer func([]byte) ([]byte, error)) ([]playlist.Item, []byte, error) {
	enc, err := lire("pl_enc.bin")
	if err != nil {
		present, xerr := existe("pl_enc.bin")
		if xerr != nil {
			return nil, nil, fmt.Errorf("pl_enc.bin illisible (%v) et presence non verifiable : %w", err, xerr)
		}
		if present {
			return nil, nil, fmt.Errorf("pl_enc.bin present mais illisible : %w", err)
		}
		bin, berr := lire("pl.bin")
		if berr != nil {
			return nil, nil, fmt.Errorf("ni pl_enc.bin ni pl.bin lisibles : %w", berr)
		}
		items, derr := playlist.DecodeV2(bin)
		if derr != nil {
			return nil, nil, fmt.Errorf("decodage pl.bin : %w", derr)
		}
		return items, bin, nil
	}
	clair, derr := dechiffrer(enc)
	if derr != nil {
		return nil, nil, fmt.Errorf("dechiffrement pl_enc.bin : %w", derr)
	}
	items, derr := playlist.DecodeV2(clair)
	if derr != nil {
		return nil, nil, fmt.Errorf("decodage pl_enc.bin dechiffre : %w", derr)
	}
	return items, clair, nil
}

// PublierEditions relit l'arbre reel (base anti-wipe + champs authentiques),
// applique par-dessus les editions de l'arbre de travail (renommages, ordre,
// deplacements de series, categories creees), televerse medias et illustrations
// nouveaux, puis publie l'arbre complet : pl_enc.bin, adoption par
// check_playlist.cmd, menage par clean_sd.cmd (docs/PROTOCOLE-2.1.23.md
// section 5). En l'absence d'editions, elle republie l'arbre a l'identique.
func (a *App) PublierEditions(mac string) error {
	debut := time.Now()
	log.Printf("publication %s : debut", mac)
	err := a.connecterPourEcrire(mac, func(c *device.Client) error { return a.publierEditions(c, mac) })
	if err != nil {
		log.Printf("publication %s : echec apres %s : %v", mac, time.Since(debut).Round(time.Second), err)
	} else {
		log.Printf("publication %s : terminee en %s", mac, time.Since(debut).Round(time.Second))
	}
	return err
}

func (a *App) publierEditions(c *device.Client, mac string) error {

	// Relire l'arbre reel (anti-wipe + champs authentiques : favorite/webradio/add_time).
	a.emitProgres("prog_lecture_boite", 0)
	items, bin, err := a.lirePlaylistBoite(c)
	if err != nil {
		return errDetail("err_lecture_playlist", err)
	}
	if e := store.Save(dirPl(mac)+"/"+time.Now().Format("20060102-150405")+".bin", bin); e != nil {
		log.Printf("sauvegarde pl.bin (forensique, non bloquante) echouee : %v", e)
	}
	store.GarderRecentsDir(dirPl(mac), 5)
	reel, err := playlist.BuildTree(items)
	if err != nil {
		return errDetail("err_reconstruction_arbre", err)
	}
	log.Printf("publication %s : playlist de la boite relue (%d enregistrements)", mac, len(items))

	// Arbre a publier : editions de l'arbre de travail (LOCAL-FIRST, source de
	// verite ; absentes -> republication a l'identique) et garde-fous.
	travailJSON, _ := store.Load(ficTravail(mac))
	prep, err := preparerArbre(reel, travailJSON)
	if err != nil {
		return err
	}
	publie, images, nouveaux := prep.publie, prep.images, prep.nouveaux
	jsonArbre, err := playlist.SerialiserBoite(publie)
	if err != nil {
		return errDetail("err_serialisation", err)
	}
	// Export de l'arbre courant AVANT publication (invariant protocole) : si on ne
	// peut pas l'ecrire, on refuse de publier (pas de filet = pas d'ecriture destructive).
	if e := store.Save(dirBackups(mac)+"/"+time.Now().Format("20060102-150405")+".json", jsonArbre); e != nil {
		return errDetail("err_export_arbre", e)
	}
	store.GarderRecentsDir(dirBackups(mac), 5)

	// Playlist binaire chiffree (norme firmware 2.1.23) : on construit le pl.bin
	// (enregistrements 96 o) depuis l'arbre, en reutilisant le kind BRUT connu de
	// la boite par uuid (elle distingue des sous-types audio par des bits que
	// l'arbre ne porte pas ; cf. SerialiserArbreV2), puis on chiffre (cle =
	// SHA-256 du MAC). Pousse via UploadFile("pl_enc.bin") + check_playlist.cmd
	// (op13), PAS op6 (chemin JSON herite qui refuse un .bin : "Invalid extension").
	clairPl := playlist.SerialiserV2(playlist.SerialiserArbreV2(publie, playlist.ConnusDepuis(items)))
	macBytes, err := c.MACBytes()
	if err != nil {
		return errDetail("err_mac_chiffrement", err)
	}
	plEnc, err := device.ChiffrerPlaylist(macBytes, clairPl, nil)
	if err != nil {
		return errDetail("err_chiffrement", err)
	}

	// Liste des envois, pour une barre de progression par octets (pas de jargon).
	// Ordre : illustrations de categories, medias des contenus ajoutes (audio +
	// vignette, ou illustration de serie seule), puis l'arbre en dernier. On
	// calcule d'abord les tailles (sans tout charger) et on charge a la volee.
	const nomPlaylist = "pl_enc.bin"
	type envoi struct {
		nom      string // nom du fichier sur la boite
		data     []byte // donnees si deja en memoire (illustrations, arbre) ; sinon nil
		source   string // nom bibliotheque a charger a la volee (medias)
		taille   int64
		cacheImg string // uuid a mettre en cache d'affichage apres envoi (illustrations)
		histoire bool   // audio d'une histoire (pour le libelle)
	}
	var envois []envoi

	for _, im := range images {
		envois = append(envois, envoi{nom: im.uuid + ".jpg", data: im.data, taille: int64(len(im.data)), cacheImg: im.uuid})
	}
	couverts := map[string]bool{}
	for _, im := range images {
		couverts[im.uuid] = true
	}
	// Titre par uuid, pour des messages d'erreur nommant le noeud concerne.
	titres := playlist.IndexTitres(publie)
	nomNoeud := func(u string) string {
		if t := titres[u]; t != "" {
			return t
		}
		return u // sans titre connu : l'identifiant
	}
	// Une histoire (feuille hors webradio) exige son audio : sans lui, elle
	// tomberait dans la branche "illustration seule" des series et stations, et
	// partirait muette sur la boite.
	histoires := playlist.Histoires(publie)
	for _, u := range nouveaux {
		if couverts[u] {
			continue
		}
		if histoires[u] && library.ExtensionAudio(u) == "" {
			return errIHM("err_audio_introuvable", "nom", nomNoeud(u))
		}
		if ext := library.ExtensionAudio(u); ext != "" {
			ta, e := store.Size(library.CheminAudio(u, ext))
			if e != nil {
				return errIHM("err_audio_introuvable", "nom", nomNoeud(u))
			}
			tj, e := store.Size(library.CheminIllustration(u))
			if e != nil {
				return errIHM("err_illustration_introuvable", "nom", nomNoeud(u))
			}
			envois = append(envois,
				envoi{nom: u + "." + ext, source: library.CheminAudio(u, ext), taille: ta, histoire: true},
				envoi{nom: u + ".jpg", source: library.CheminIllustration(u), taille: tj, cacheImg: u})
		} else if tj, e := store.Size(library.CheminIllustration(u)); e == nil {
			envois = append(envois, envoi{nom: u + ".jpg", source: library.CheminIllustration(u), taille: tj, cacheImg: u})
		} else {
			return errIHM("err_contenu_incomplet", "nom", nomNoeud(u))
		}
	}
	envois = append(envois, envoi{nom: nomPlaylist, data: plEnc, taille: int64(len(plEnc))})

	var total int64
	for _, e := range envois {
		total += e.taille
	}
	if total <= 0 {
		total = 1
	}

	// Controle d'espace AVANT tout envoi (cf. espace.go) : les retraits ne
	// liberent leur place qu'a l'adoption de l'arbre, apres les envois.
	connus := inventaireBoite(mac)
	fe := make([]fichierEnvoi, 0, len(envois))
	for _, e := range envois {
		fe = append(fe, fichierEnvoi{Nom: e.nom, Taille: e.taille})
	}
	if err := a.controlerEspace(c, octetsNets(fe, connus)); err != nil {
		return err
	}
	log.Printf("publication %s : %d fichier(s) a envoyer (%s), arbre de %d noeud(s)", mac, len(envois), moLisible(total), len(playlist.UUIDs(publie)))

	// Avancement : pourcentage sur les octets (fichiers deja presents credites),
	// temps restant estime sur le debit REELLEMENT transfere (comme la sauvegarde
	// initiale). envoye = octets credites ; transfere = octets vraiment envoyes.
	debut := time.Now()
	var envoye, transfere int64
	nbEnvois := len(envois)
	progres := func(libelle string, faits int, enCours int64) {
		fait := envoye + enCours
		eta := int64(-1)
		if ec := time.Since(debut).Seconds(); ec > 1 && transfere+enCours > 0 {
			eta = int64(float64(total-fait) / (float64(transfere+enCours) / ec))
		}
		a.emitProgresSync(ProgresSync{
			Libelle: libelle, Pct: float64(fait) / float64(total) * 100, EtaSecondes: eta,
			Faits: faits, Total: nbEnvois, OctetsFaits: fait, OctetsTotal: total,
		})
	}

	// Reprise apres coupure : un fichier deja present et IDENTIQUE (sha256) sur la
	// boite n'est pas renvoye. L'arbre (pl_enc.bin) part toujours, en dernier : il
	// n'est adopte qu'a la fin, donc les medias deja televerses avant une coupure
	// sont simplement reutilises. Delai large : la boite hache le fichier.
	c.SetTimeout(60 * time.Second)
	verifReprise := true // passe a faux a la 1re erreur d'op31 : on n'insiste pas
	// Inventaire tenu a jour fichier par fichier (envoye OU retrouve par la
	// reprise) : apres une coupure, le controle d'espace de la reprise suivante
	// ne recompte pas ce qui est deja sur la boite.
	noterSurBoite := func(nom string, taille int) {
		connus[nom] = uint32(taille)
		if d, err := json.Marshal(connus); err == nil {
			_ = store.Save(ficTailles(mac), d)
		}
	}
	// Journal d'avancement par paliers de 10 % (octets) : une synchro peut durer
	// des heures, le journal doit dire ou elle en est.
	dernierPalier := int64(0)
	dejaPresents := 0
	journaliser := func(faits int) {
		p := envoye * 10 / total // dizaines de % atteintes
		if p <= dernierPalier {
			return
		}
		dernierPalier = p
		debit := float64(transfere) / time.Since(debut).Seconds() / 1024
		log.Printf("publication %s : %d %%, %d/%d fichier(s), %s envoyes, %d deja sur la boite, %.0f Ko/s", mac, p*10, faits, nbEnvois, moLisible(envoye), dejaPresents, debit)
	}
	for i, e := range envois {
		libelle := "prog_envoi_images"
		if e.histoire {
			libelle = "prog_envoi_histoires"
		} else if e.nom == nomPlaylist {
			libelle = "prog_finalisation"
		}
		data := e.data
		if data == nil {
			d, err := store.Load(e.source)
			if err != nil {
				return errIHM("err_fichier_introuvable")
			}
			data = d
		}
		if e.nom != nomPlaylist && verifReprise {
			sum := sha256.Sum256(data)
			present, identique, err := c.IsFileExist(e.nom, sum[:])
			if err != nil {
				verifReprise = false
				log.Printf("reprise : IsFileExist indisponible (%v), envoi integral", err)
			} else if present && identique {
				envoye += int64(len(data))
				noterSurBoite(e.nom, len(data))
				progres("prog_deja_sur_boite", i+1, 0)
				dejaPresents++
				journaliser(i + 1)
				if e.cacheImg != "" {
					_ = store.Save(cheminAffichage(mac, e.cacheImg), data)
				}
				continue
			}
		}
		if err := televerserBride(c, e.nom, data, func(envoyeFichier int64) {
			progres(libelle, i, envoyeFichier)
		}); err != nil {
			log.Printf("publication %s : envoi de %s (%s) echoue : %v", mac, e.nom, moLisible(int64(len(data))), err)
			return errIHM("err_envoi_echoue")
		}
		envoye += int64(len(data))
		transfere += int64(len(data))
		progres(libelle, i+1, 0)
		journaliser(i + 1)
		if e.nom != nomPlaylist {
			noterSurBoite(e.nom, len(data))
		}
		if e.cacheImg != "" {
			_ = store.Save(cheminAffichage(mac, e.cacheImg), data)
		}
	}

	a.emitProgres("prog_finalisation", 100)
	// Adoption de la playlist binaire : check_playlist.cmd (op13) declenche
	// load_playlist cote boite (lecture + dechiffrement + chargement du pl_enc.bin
	// qu'on vient de televerser). Reponse [status:1][count:u16 LE] ; status 0 = OK.
	c.SetTimeout(120 * time.Second)
	st, err := c.AdopterPlaylist()
	if err != nil {
		return errIHM("err_validation_playlist")
	}
	if st != 0 {
		return errIHM("err_playlist_refusee", "statut", st)
	}
	log.Printf("publication %s : arbre adopte par la boite", mac)
	// Comme l'app officielle (uploadPlaylist) : une seule variante de playlist sur
	// la carte. Un pl.bin perime n'a plus d'effet (pl_enc.bin est lu en priorite,
	// par le firmware comme par choisirPlaylist) : simple menage, non bloquant.
	c.SetTimeout(10 * time.Second)
	if present, _, err := c.IsFileExist("pl.bin", nil); err == nil && present {
		if err := c.SupprimerFichierVerifie("pl.bin"); err != nil {
			log.Printf("publication : suppression de pl.bin perime non faite (%v)", err)
		}
	}
	// Menage, comme l'app officielle : check_playlist.cmd adopte l'arbre SANS
	// rien supprimer ; seul clean_sd.cmd efface les medias que l'arbre adopte ne
	// reference plus (retraits, et restes des synchros precedentes). Sur pour cet
	// arbre : il vient d'etre adopte et les garde-fous ci-dessus l'ont valide
	// complet. Echec non bloquant : la publication est faite, la place sera
	// liberee a la prochaine.
	a.emitProgres("prog_liberation", 100)
	c.SetTimeout(120 * time.Second) // la boite parcourt toute la carte
	if err := c.NettoyerCarte(); err != nil {
		log.Printf("publication : menage (clean_sd.cmd) non fait (%v)", err)
	} else {
		log.Printf("publication %s : menage de la carte fait", mac)
	}
	c.SetTimeout(10 * time.Second)
	a.noterStockage(c, mac) // apres le menage, avant EndSync (la boite quitte l'AP)
	_ = c.EndSync()

	// Succes : l'arbre local reflete la boite ; on solde les editions.
	if d, e := json.MarshalIndent(publie, "", "  "); e == nil {
		_ = store.Save(ficArbre(mac), d)
	}

	// Mettre a jour l'inventaire des tailles pour refleter la boite apres push,
	// sans re-scan op11/op12 : on retire les medias supprimes par le menage
	// (uuid plus reference dans l'arbre publie) et on (re)pose la taille des
	// fichiers televerses. Les fichiers systeme (sans uuid d'arbre) sont preserves.
	refUUID := playlist.UUIDs(publie)
	televerses := map[string]int64{}
	for _, e := range envois {
		televerses[e.nom] = e.taille
	}
	inv := map[string]uint32{}
	if data, e := store.Load(ficTailles(mac)); e == nil {
		_ = json.Unmarshal(data, &inv)
	}
	if data, e := json.Marshal(fusionInventaire(inv, refUUID, televerses)); e == nil {
		_ = store.Save(ficTailles(mac), data)
	}

	// Editions soldees : le travail est supprime, SAUF s'il a change depuis sa
	// lecture en debut de publication (modification faite pendant la synchro) :
	// le supprimer la perdrait sans un mot. Conserve, il repartira a la
	// prochaine publication (les medias deja envoyes sont reconnus par op31).
	if actuel, _ := store.Load(ficTravail(mac)); bytes.Equal(actuel, travailJSON) {
		_ = store.Remove(ficTravail(mac))
	} else {
		log.Printf("publication %s : travail modifie pendant la synchronisation, conserve pour la prochaine", mac)
	}
	a.marquerSync(mac) // date de derniere synchro (pour l'accueil)
	return nil
}

// DefinirWifi configure le Wi-Fi STATION de la boite (le reseau domestique
// qu'elle rejoint pour Internet). Voir docs/WIFI.md. La boite doit etre en mode
// point d'acces. Le mot de passe n'est ni journalise ni renvoye a l'IHM.
func (a *App) DefinirWifi(mac, ssid, motDePasse string) error {
	return a.connecterPourEcrire(mac, func(c *device.Client) error { return a.definirWifi(c, mac, ssid, motDePasse) })
}

func (a *App) definirWifi(c *device.Client, mac, ssid, motDePasse string) error {

	a.emitProgres("prog_wifi_envoi", 50)
	if err := c.DefinirWifi(ssid, motDePasse); err != nil {
		return errDetail("err_wifi", err)
	}
	a.majWifiMaison(mac, ssid) // la boite a desormais un Wi-Fi domestique (panneau Radio)
	a.emitProgres("prog_wifi_ok", 100)
	return nil
}

// majWifiMaison enregistre au registre le SSID du Wi-Fi domestique de la boite
// (jamais le mot de passe). Sa presence signale que les radios sont jouables.
func (a *App) majWifiMaison(mac, ssid string) {
	a.majBoite(mac, func(m *BoiteMeta) { m.WifiMaison = ssid })
}

// DefinirHoraires pose les plages horaires d'utilisation sur la boite (voir
// docs/HORAIRES.md). La boite doit etre en mode point d'acces.
func (a *App) DefinirHoraires(mac string, h device.Horaires) error {
	return a.connecterPourEcrire(mac, func(c *device.Client) error { return a.definirHoraires(c, mac, h) })
}

func (a *App) definirHoraires(c *device.Client, mac string, h device.Horaires) error {

	a.emitProgres("prog_horaires_envoi", 50)
	if err := c.DefinirHoraires(h); err != nil {
		return errDetail("err_horaires", err)
	}
	a.emitProgres("prog_horaires_ok", 100)
	return nil
}

// RadioDispo : une radio du catalogue, prete a inserer dans l'arbre de travail.
// Logo : son illustration 128 est en bibliotheque (<uuid>.jpg), comme celle d'une
// serie ou d'une histoire : affichee par /boiteimg (repli bibliotheque), poussee a
// la publication et comptee dans l'estimation. Sans logo, l'IHM en genere un.
type RadioDispo struct {
	UUID  string `json:"uuid"`
	Titre string `json:"titre"`
	Logo  bool   `json:"logo"`
}

// RadiosResult : le catalogue + l'identite du conteneur "Radio" d'origine
// (UUID + logo en bibliotheque), pour reutiliser l'icone existante au lieu d'en
// recreer une.
type RadiosResult struct {
	ConteneurUUID string       `json:"conteneurUuid"`
	ConteneurLogo bool         `json:"conteneurLogo"`
	Stations      []RadioDispo `json:"stations"`
}

// RadiosDisponibles recupere le catalogue des webradios pour cette boite
// (en ligne, hors mode AP) et telecharge les logos. Renvoie aussi l'identite du
// conteneur "Radio" existant (lu dans l'arbre reel persiste `arbre-<mac>.json`,
// disponible hors ligne) pour conserver son UUID et son icone. L'ajout a l'arbre
// et le push se font ensuite cote IHM. `mac` est le MAC SoftAP (op30).
func (a *App) RadiosDisponibles(mac string) (RadiosResult, error) {
	radios, err := webradio.Catalogue(mac)
	if err != nil {
		return RadiosResult{}, err
	}
	res := RadiosResult{Stations: make([]RadioDispo, 0, len(radios))}
	for _, r := range radios {
		d := RadioDispo{UUID: r.UUID, Titre: r.Titre}
		if r.ImageURL != "" && uuidMediaValide(r.UUID) == nil {
			if jpg, e := library.ImageJPEG128(r.ImageURL); e == nil {
				d.Logo = store.Save(library.CheminIllustration(r.UUID), jpg) == nil
			}
		}
		res.Stations = append(res.Stations, d)
	}
	// Conteneur "Radio" d'origine : UUID + logo depuis l'arbre reel persiste.
	if data, e := store.Load(ficArbre(mac)); e == nil {
		var arbre []*playlist.Node
		if json.Unmarshal(data, &arbre) == nil {
			if u := uuidConteneurWebradio(arbre); u != "" {
				res.ConteneurUUID = u
				if logo, e := store.Load(cheminAffichage(mac, u)); e == nil && len(logo) > 0 {
					res.ConteneurLogo = store.Save(library.CheminIllustration(u), logo) == nil
				}
			}
		}
	}
	return res, nil
}

// uuidConteneurWebradio renvoie l'UUID du premier noeud portant le drapeau
// webradio (le conteneur "Radio"), ou "" s'il n'y en a pas.
func uuidConteneurWebradio(arbre []*playlist.Node) string {
	for _, n := range arbre {
		if n.Webradio != 0 {
			return n.UUID
		}
		if u := uuidConteneurWebradio(n.Children); u != "" {
			return u
		}
	}
	return ""
}

// nomBoite renvoie le nom d'une boite du registre (par MAC), ou un libelle
// generique si absente.
func (a *App) nomBoite(mac string) string {
	for _, m := range a.lireRegistre() {
		if memeMAC(m.MAC, mac) && m.Nom != "" {
			return m.Nom
		}
	}
	return ""
}

// verifierIdentite refuse d'ecrire si la boite jointe n'est pas celle visee. La
// connexion cible "ce qui repond en 192.168.4.1" : avec plusieurs boites, on
// pourrait ecrire la mauvaise et corrompre l'etat local de la boite ciblee. Un
// MAC illisible est traite comme un echec (op30 est fiable sur le firmware
// supporte : ne pas verifier, c'est prendre le risque).
func (a *App) verifierIdentite(c *device.Client, mac string) error {
	macLue, err := c.MAC()
	if err != nil {
		return errIHM("err_identite_illisible")
	}
	if !memeMAC(macLue, mac) {
		if nom := a.nomBoite(mac); nom != "" {
			return errIHM("err_autre_boite", "nom", nom)
		}
		return errIHM("err_autre_boite_anonyme")
	}
	return nil
}

// ArbreBoite relit l'arbre d'une boite (par MAC) pour l'IHM, SANS les
// categories systeme (gerees par la boite, non editables), issu de la lecture de
// pl_enc.bin (ou pl.bin). Sert de source a la vue "Contenu par categorie".
func (a *App) ArbreBoite(mac string) ([]*playlist.Node, error) {
	data, err := store.Load(ficArbre(mac))
	if err != nil {
		return nil, err
	}
	var arbre []*playlist.Node
	if err := json.Unmarshal(data, &arbre); err != nil {
		return nil, err
	}
	out := make([]*playlist.Node, 0, len(arbre))
	for _, n := range arbre {
		if n != nil && !playlist.EstCategorieSysteme(n.Title) {
			out = append(out, n)
		}
	}
	return out, nil
}

// ficTravail nomme le fichier de l'arbre de travail (editions en cours) d'une
// boite. Distinct de l'arbre reel (ficArbre) : il porte les ajouts, retraits,
// renommages et illustrations non encore publies. Le modele deux phases impose
// de preparer l'arbre hors-ligne, donc de survivre aux redemarrages.
func ficTravail(mac string) string { return dirTravailBoite(mac) + "/travail.json" }
func dirBackups(mac string) string { return dirTravailBoite(mac) + "/backups" }

// ArbreTravail renvoie l'arbre de travail persiste (JSON brut, avec les
// drapeaux d'edition du front prefixes par _), ou "" si aucun. On ne le decode
// pas cote Go pour ne pas perdre ces champs specifiques a l'IHM.
func (a *App) ArbreTravail(mac string) string {
	data, err := store.Load(ficTravail(mac))
	if err != nil {
		return ""
	}
	return string(data)
}

// EnregistrerArbreTravail persiste l'arbre de travail d'une boite (JSON brut
// fourni par l'IHM). Appele a chaque edition pour ne rien perdre.
func (a *App) EnregistrerArbreTravail(mac, arbreJSON string) error {
	return store.Save(ficTravail(mac), []byte(arbreJSON))
}

// Les illustrations d'arbre sont mises en cache d'affichage par boite sous
// cache/boites/<mac>/affichage/<uuid>.jpg (cf. cheminAffichage), servies par le
// handler d'assets (voir run.go) a l'URL boiteimg/<mac>/<uuid>.

// cacherImagesArbre telecharge l'illustration {uuid}.jpg de CHAQUE noeud de
// l'arbre (categories, series, histoires) et la met en cache. Best-effort et
// sequentiel (connexion unique a la boite). Emet l'avancement (evtImages) pour
// que l'IHM affiche une barre de progression.
func (a *App) cacherImagesArbre(c *device.Client, arbre []*playlist.Node, mac string) {
	total := 0
	var compter func(n *playlist.Node)
	compter = func(n *playlist.Node) {
		if n == nil || n.UUID == "" {
			return
		}
		total++
		for _, ch := range n.Children {
			compter(ch)
		}
	}
	for _, cat := range arbre {
		if cat != nil && !playlist.EstCategorieSysteme(cat.Title) {
			compter(cat)
		}
	}

	fait := 0
	a.emit(evtImages, EtatImages{Fait: 0, Total: total})
	var visite func(n *playlist.Node)
	visite = func(n *playlist.Node) {
		if n == nil || n.UUID == "" {
			return
		}
		if !store.Has(cheminAffichage(mac, n.UUID)) { // deja en cache -> pas de re-telechargement
			if data, err := c.GetFileBytes(n.UUID + ".jpg"); err == nil && len(data) > 0 {
				_ = store.Save(cheminAffichage(mac, n.UUID), data)
			}
		}
		fait++
		a.emit(evtImages, EtatImages{Fait: fait, Total: total})
		for _, ch := range n.Children {
			visite(ch)
		}
	}
	for _, cat := range arbre {
		if cat != nil && !playlist.EstCategorieSysteme(cat.Title) {
			visite(cat)
		}
	}
}

// DemarrerVeille lance la surveillance de la boite en mode AP. Des qu'elle est
// jointe, l'arbre est lu, reconstruit et persiste (par MAC), puis evtPret est
// emis. Re-armable : apres une lecture reussie, un nouvel appel relance une
// veille (ajout d'une autre boite via le "+").
func (a *App) DemarrerVeille() {
	a.mu.Lock()
	if a.enVeille {
		a.mu.Unlock()
		return
	}
	a.enVeille = true
	a.arretVeille = make(chan struct{})
	stop := a.arretVeille
	a.mu.Unlock()

	go a.veille(stop)
}

// ArreterVeille demande l'arret de la goroutine de veille (fermeture du modal
// d'ajout, destruction du composant de decouverte). Idempotent : sans effet si
// la veille est deja arretee.
func (a *App) ArreterVeille() {
	a.mu.Lock()
	if a.enVeille && a.arretVeille != nil {
		close(a.arretVeille)
		a.arretVeille = nil
		a.enVeille = false
	}
	a.mu.Unlock()
}

// attendreOuArret dort d, ou rend true si l'arret est demande avant la fin.
func attendreOuArret(stop <-chan struct{}, d time.Duration) bool {
	select {
	case <-stop:
		return true
	case <-time.After(d):
		return false
	}
}

func (a *App) emit(nom string, data ...interface{}) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, nom, data...)
}

// ProgresSync : avancement de la publication pour la barre de l'IHM. Libelle en
// langage courant (pas de jargon protocole), Pct de 0 a 100.
type ProgresSync struct {
	Libelle     string  `json:"libelle"`
	Pct         float64 `json:"pct"`
	EtaSecondes int64   `json:"eta_secondes"` // temps restant estime, -1 si inconnu
	Faits       int     `json:"faits"`        // fichiers traites (envoyes ou deja presents)
	Total       int     `json:"total"`        // fichiers a traiter (0 : pas de detail)
	OctetsFaits int64   `json:"octets_faits"`
	OctetsTotal int64   `json:"octets_total"`
}

func (a *App) emitProgres(libelle string, pct float64) {
	a.emitProgresSync(ProgresSync{Libelle: libelle, Pct: pct, EtaSecondes: -1})
}

func (a *App) emitProgresSync(p ProgresSync) {
	if p.Pct < 0 {
		p.Pct = 0
	} else if p.Pct > 100 {
		p.Pct = 100
	}
	a.emit(evtSyncProgres, p)
}

// veille interroge la boite jusqu'a reussir a lire et reconstruire l'arbre.
func (a *App) veille(stop <-chan struct{}) {
	a.emit(evtAttente)
	j := joignabilite{origine: "veille"}
	for {
		select {
		case <-stop:
			return
		default:
		}
		// Boite occupee par une operation exclusive (publication...) : on
		// n'ouvre pas de 2e socket, on attend le tour suivant.
		if !a.connMu.TryLock() {
			if attendreOuArret(stop, 2*time.Second) {
				return
			}
			continue
		}
		c, err := device.Dial(device.DefaultAddr(), 1500*time.Millisecond)
		if err != nil {
			a.connMu.Unlock()
			j.echec(err)
			// Port ferme : boite absente ou machine pas sur son AP.
			a.emit(evtAttente)
			if attendreOuArret(stop, 2*time.Second) {
				return
			}
			continue
		}
		j.succes()
		pret := a.lireBoite(c)
		c.Close()
		a.connMu.Unlock()
		if pret {
			a.mu.Lock()
			// Ne liberer l'etat que si on possede toujours ce canal (une
			// veille relancee entre-temps ne doit pas etre ecrasee).
			if a.arretVeille == stop {
				a.enVeille = false // re-armable pour l'ajout d'une autre boite
				a.arretVeille = nil
			}
			a.mu.Unlock()
			return
		}
		if attendreOuArret(stop, 2*time.Second) {
			return
		}
	}
}

// lireBoite realise l'echange complet avec une boite jointe. Renvoie true si
// l'arbre a ete lu et persiste (accueil termine).
func (a *App) lireBoite(c *device.Client) bool {
	c.SetTimeout(5 * time.Second)
	debut := time.Now()
	if err := c.Ping(); err != nil {
		log.Printf("veille : ping sans reponse apres %s (%v)", time.Since(debut).Round(100*time.Millisecond), err)
		a.emit(evtAttente)
		return false
	}

	// Firmware en toute premiere lecture (op5) : avant la 2.1.23, la playlist
	// n'est pas lisible par Merlinus. Le verdict n'est applique qu'apres la MAC,
	// et seulement a une boite nouvelle (une boite deja connue est ignoree).
	firmware, errFirmware := controlerFirmware(c)

	// MAC ensuite : si la boite est DEJA connue, on n'emet rien et on ne lit rien.
	// La veille continue simplement d'attendre une NOUVELLE boite (l'utilisateur
	// reste sur l'ecran d'attente, sans message ni bouton).
	// Sans MAC, pas d'association : la MAC est la cle du registre, la cle de
	// dechiffrement de pl_enc.bin et le controle d'identite avant publication.
	pret := ComptePret{}
	m, err := c.MAC()
	if err != nil || m == "" {
		a.emit(evtConnectee)
		a.emit(evtErreur, errIHM("err_assoc_mac").Error())
		return false
	}
	pret.MAC = m
	if a.macConnue(pret.MAC) {
		return false // deja connue : on continue d'attendre
	}

	a.emit(evtConnectee)
	if errFirmware != nil {
		log.Printf("association %s refusee : %v", pret.MAC, errFirmware)
		a.emit(evtErreur, errFirmware.Error())
		return false
	}
	pret.Firmware = firmware.String()
	log.Printf("association %s : firmware %s accepte", pret.MAC, firmware)

	// Infos de la boite : enrichissent le compte rendu, sans bloquer si l'une
	// est indisponible (chaque lecture est independante et tolerante a l'echec).
	if b, err := c.Battery(); err == nil {
		pret.Batterie = &b
	}
	if total, err := c.SDSize(); err == nil {
		pret.StockageTotal = total
		if libre, err := c.SizeAvailable(); err == nil {
			pret.StockageLibre = libre
		}
	}
	if w, err := c.WifiConfig(); err == nil {
		pret.SSID = w.SSID // uniquement le SSID, jamais le mot de passe
	}
	// Wi-Fi domestique (sta_wifi.json) : seul son SSID est retenu (panneau Radio).
	wifiMaison := ""
	if sta, err := c.GetFileBytes("sta_wifi.json"); err == nil {
		wifiMaison = device.SSIDWifiMaison(sta)
	}

	cle := pret.MAC // cle de la boite au registre

	a.emit(evtProgres, "Lecture des données...")
	items, bin, err := a.lirePlaylistBoite(c)
	if err != nil {
		a.emit(evtErreur, errDetail("err_lecture_playlist", err).Error())
		return false
	}
	arbre, err := playlist.BuildTree(items)
	if err != nil {
		a.emit(evtErreur, errDetail("err_reconstruction_arbre", err).Error())
		return false
	}
	rapport := playlist.Summarize(items, len(bin))

	// Inventaire des tailles reelles des fichiers (op 11 + op 12) : sert a afficher
	// la taille de chaque histoire dans l'arbre et a crediter les retraits dans
	// l'estimation de stockage. Tolerant : un echec ne bloque pas l'accueil.
	a.emit(evtInventaire, EtatImages{Fait: 0, Total: 0})
	fichiers, err := c.ListerFichiers(func(fait, total int) {
		if total > 0 && (fait == total || fait%16 == 0) {
			a.emit(evtInventaire, EtatImages{Fait: fait, Total: total})
		}
	})
	if err == nil {
		a.enregistrerTailles(cle, fichiers, pret.StockageTotal, pret.StockageLibre)
	} else {
		log.Printf("inventaire des fichiers indisponible (%s) : %v", cle, err)
	}

	// Sauvegarde complete (opt-in) OU simple cache d'affichage des illustrations.
	// La sauvegarde rapatrie TOUT l'inventaire (audios + illustrations) et
	// recopie les .jpg dans le cache d'affichage : elle remplace alors
	// cacherImagesArbre. Complete ou rien : une erreur empeche l'inscription.
	a.mu.Lock()
	avecSauvegarde := a.sauvegardeInitiale
	a.mu.Unlock()
	if avecSauvegarde {
		if err != nil { // inventaire indispensable (totaux, pourcentage, reprise)
			a.emit(evtErreur, errDetail("err_assoc_inventaire", err).Error())
			return false
		}
		if e := a.sauvegarderBoite(c, arbre, fichiers, bin, cle, pret.StockageTotal); e != nil {
			a.emit(evtErreur, errDetail("err_assoc_sauvegarde", e).Error())
			return false
		}
	} else {
		// Pas de sauvegarde demandee : on efface un eventuel reste de sauvegarde
		// interrompue (sans manifeste), qui ferait croire a une sauvegarde.
		if !store.Has(ficManifeste(cle)) {
			store.RemoveDir(dirSauvegarde(cle))
		}
		a.emit(evtProgres, "Recuperation des illustrations...")
		a.cacherImagesArbre(c, arbre, cle)
	}

	// Favoris (fav.bin, lecture seule) : lus des l'association, pour que l'IHM
	// les affiche sans attendre un passage "connecte".
	if fb, err := c.GetFileBytes("fav.bin"); err == nil {
		if d, e := json.Marshal(playlist.FavorisDepuisBin(fb)); e == nil {
			_ = store.Save(ficFavoris(cle), d)
		}
	}

	// COMMIT a la toute fin : la boite n'est inscrite au registre (donc "configuree")
	// qu'une fois la decouverte COMPLETE (arbre + inventaire + illustrations). Une
	// decouverte interrompue avant ce point ne laisse aucune entree -> l'app
	// reaffiche l'ecran Welcome au redemarrage.
	if err := a.persister(cle, arbre, rapport); err != nil {
		a.emit(evtErreur, errDetail("err_assoc_enregistrement", err).Error())
		return false
	}
	a.majMeta(BoiteMeta{
		MAC: cle, SSID: pret.SSID, Firmware: pret.Firmware,
		StockageTotal: pret.StockageTotal, StockageLibre: pret.StockageLibre,
		WifiMaison: wifiMaison,
	})
	store.Remove(ficTravail(cle)) // relecture = etat frais : on repart des editions a zero
	// Dernieres ecoutes : relevees des l'association (l'accueil les montre tout
	// de suite). Apres persister : les titres viennent de l'arbre en cache.
	// Echec non bloquant (le releve se refera a la prochaine connexion).
	if err := releverEcoutes(c, cle); err != nil {
		log.Printf("association %s : releve des ecoutes impossible (%v)", cle, err)
	}
	// Pref de sauvegarde consommee seulement a l'inscription reussie : en cas
	// d'echec on la laisse armee pour que la reconnexion reprenne la sauvegarde.
	a.mu.Lock()
	a.sauvegardeInitiale = false
	a.mu.Unlock()

	pret.Rapport = rapport
	a.emit(evtPret, pret)
	log.Printf("accueil termine : %d categories, %d dossiers, %d histoires",
		rapport.Categories, rapport.Dossiers, rapport.Histoires)
	return true
}

func (a *App) persister(mac string, arbre []*playlist.Node, rapport playlist.Report) error {
	arbreJSON, err := json.MarshalIndent(arbre, "", "  ")
	if err != nil {
		return err
	}
	if err := store.Save(ficArbre(mac), arbreJSON); err != nil {
		return err
	}
	rapportJSON, err := json.Marshal(rapport)
	if err != nil {
		return err
	}
	return store.Save(ficRapport(mac), rapportJSON)
}

// ---- Catalogue (necessite Internet ; boite PAS en mode AP) ----

// Internet indique si l'acces au web est disponible (message du catalogue quand
// la machine est sur l'AP de la boite, donc coupee d'Internet). Tests EN PARALLELE sur des
// IP publiques (pas de DNS, qui pendrait hors-ligne) : en ligne des que l'une
// repond ; hors-ligne apres ~2s si aucune. Un seul host bloque/filtre ne suffit
// donc pas a declarer hors-ligne.
func (a *App) Internet() bool {
	// Hotes reels du catalogue, par NOM : la resolution DNS fait partie du test.
	// Une IP brute peut sembler joignable sans que le catalogue le soit (sur l'AP
	// de la boite, pas de DNS) ; c'est la joignabilite du catalogue qui compte.
	hosts := []string{"itunes.apple.com:443", "www.radiofrance.fr:443"}
	res := make(chan bool, len(hosts))
	for _, h := range hosts {
		go func(h string) {
			c, err := net.DialTimeout("tcp", h, 3*time.Second)
			if err == nil {
				_ = c.Close()
				res <- true
				return
			}
			res <- false
		}(h)
	}
	for range hosts {
		if <-res {
			return true
		}
	}
	return false
}

// CatalogueInitial renvoie les series a afficher par defaut pour une source :
// le classement enfants pour iTunes, le listing jeunesse pour Radio France.
func (a *App) CatalogueInitial(source string) ([]itunes.Podcast, error) {
	if source == "radiofrance" {
		return a.rf.List()
	}
	list, err := a.it.Chart()
	if err != nil {
		return nil, err
	}
	// Le classement iTunes peut renvoyer jusqu'a 200 series ; on borne l'affichage
	// initial pour limiter le nombre de vignettes chargees (memoire webview).
	if len(list) > 60 {
		list = list[:60]
	}
	return list, nil
}

// RechercheCatalogue cherche des series dans une source.
// enrichirRechercheParEpisode active l'enrichissement de la recherche iTunes par
// match sur les titres d'EPISODES (en plus du match sur le nom de serie) : on
// remonte alors les collections contenant une histoire au titre correspondant.
// Booleen d'activation en dur (RF exclu tant que l'API officielle n'est pas la,
// cf. DT1 dans docs/BUGS.md).
const enrichirRechercheParEpisode = true

func (a *App) RechercheCatalogue(source, terme string) ([]itunes.Podcast, error) {
	if source == "radiofrance" {
		return a.rf.Search(terme)
	}
	series, err := a.it.Search(terme)
	if err != nil {
		return nil, err
	}
	if !enrichirRechercheParEpisode {
		return series, nil
	}
	// Enrichissement : collections dont un EPISODE correspond au terme, ajoutees
	// apres les correspondances de serie et dedupliquees par identifiant.
	deja := map[string]bool{}
	for _, p := range series {
		deja[p.ID] = true
	}
	parEpisode, err := a.it.RechercheParEpisode(terme)
	if err != nil {
		return series, nil // enrichissement best-effort : on n'echoue pas la recherche
	}
	for _, p := range parEpisode {
		if !deja[p.ID] {
			deja[p.ID] = true
			series = append(series, p)
		}
	}
	return series, nil
}

// EpisodesSerie renvoie la serie complete (description, image, episodes).
func (a *App) EpisodesSerie(source, id string) (itunes.Feed, error) {
	if source == "radiofrance" {
		return a.rf.Feed(id)
	}
	return a.it.Feed(id)
}
