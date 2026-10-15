package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

// Restauration d'une boite dans l'etat de sa sauvegarde (a l'association), avant
// de la retirer de Merlinus. Deroulement :
//
//  1. Garde-fous : bonne boite (MAC) et sauvegarde COMPLETE (chaque noeud hors
//     racine a son illustration, chaque histoire son audio), sinon refus.
//  2. Inventaire de la carte (op11/op12).
//  3. Suppression (op15) des medias absents de la sauvegarde (nos ajouts),
//     verifiee par op31 : la place est liberee AVANT tout envoi.
//  4. Envoi des medias de la sauvegarde manquants ou de taille differente.
//  5. Pose de la playlist sauvegardee TELLE QUELLE (pl_enc.bin chiffre pour
//     cette boite, ou pl.bin clair), suppression verifiee de l'autre variante,
//     puis adoption par check_playlist.cmd.
//  6. Favoris (fav.bin) remis a l'etat de la sauvegarde : repose tel quel, ou
//     supprime si la boite n'en avait pas (les favoris designent des histoires
//     de l'arbre, d'ou ce passage apres l'adoption). Historique d'ecoute purge
//     (clean_listening.cmd, comme l'app officielle) : il cite des contenus
//     retires, et la sauvegarde ne le contient pas toujours (la boite ne
//     l'ecrit dans listening.bin qu'a certains moments).
//  7. Verification : nouvel inventaire compare a la sauvegarde, et absence de
//     la variante concurrente.
//
// On ne touche ni au Wi-Fi ni aux autres fichiers systeme : seuls les medias
// (<uuid>.mp3|aac|jpg), la playlist concurrente et fav.bin peuvent etre
// supprimes.
// Coupure entre 3 et 5 : l'arbre en place reference des medias effaces ;
// relancer la restauration repare (elle repart de l'inventaire reel).

// reMedia : nom d'un media de contenu sur la carte (uuid + extension).
var reMedia = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\.(mp3|aac|jpg)$`)

// fichiersSauvegarde : fichiers de la BOITE presents dans la sauvegarde (nom ->
// taille). Exclut ce que Merlinus a ecrit lui-meme (arbre.json, manifeste.json,
// et pl.bin quand il n'est que la copie dechiffree de pl_enc.bin).
func fichiersSauvegarde(mac string) map[string]int64 {
	dir := dirSauvegarde(mac)
	chiffree := store.Has(dir + "/pl_enc.bin")
	out := map[string]int64{}
	for _, n := range store.ListerDir(dir) {
		if n == "arbre.json" || n == "manifeste.json" || (chiffree && n == "pl.bin") {
			continue
		}
		if s, err := store.Size(dir + "/" + n); err == nil {
			out[n] = s
		}
	}
	return out
}

// itemsSauvegarde decode l'arbre sauvegarde (pl.bin clair, ou pl_enc.bin
// dechiffre avec le MAC) et renvoie aussi le fichier de playlist a reposer.
func itemsSauvegarde(mac string, macBytes []byte) (items []playlist.Item, nomPl string, pl []byte, err error) {
	dir := dirSauvegarde(mac)
	if enc, e := store.Load(dir + "/pl_enc.bin"); e == nil {
		clair, e := device.DechiffrerPlaylist(macBytes, enc)
		if e != nil {
			return nil, "", nil, fmt.Errorf("la playlist sauvegardee ne se dechiffre pas avec cette boite : %w", e)
		}
		items, e = playlist.DecodeV2(clair)
		return items, "pl_enc.bin", enc, e
	}
	bin, e := store.Load(dir + "/pl.bin")
	if e != nil {
		return nil, "", nil, fmt.Errorf("aucune playlist dans la sauvegarde")
	}
	items, e = playlist.DecodeV2(bin)
	return items, "pl.bin", bin, e
}

// manquantsSauvegarde liste les fichiers attendus absents de la sauvegarde :
// l'illustration de chaque noeud hors racine, l'audio de chaque histoire.
func manquantsSauvegarde(items []playlist.Item, presents map[string]int64) []string {
	vus := map[string]bool{}
	var out []string
	exige := func(nom string) {
		if vus[nom] {
			return
		}
		vus[nom] = true
		if presents[nom] <= 0 {
			out = append(out, nom)
		}
	}
	for _, it := range items {
		if it.FileName == "" || it.FileName == playlist.UUIDRacine || it.KindRaw&0xf == 0x1 {
			continue // racine : ni image ni audio
		}
		exige(it.FileName + ".jpg")
		// Audio : .mp3, ou .aac (m4a remuxe par la bibliotheque).
		if it.KindRaw&0xf == 0x4 && presents[it.FileName+".aac"] <= 0 {
			exige(it.FileName + ".mp3")
		}
	}
	sort.Strings(out)
	return out
}

// autrePlaylist : la variante de playlist concurrente de nomPl. Une seule doit
// rester sur la carte : le firmware lit pl_enc.bin en priorite, un pl_enc.bin
// laisse a cote d'un pl.bin restaure masquerait l'arbre restaure.
func autrePlaylist(nomPl string) string {
	if nomPl == "pl.bin" {
		return "pl_enc.bin"
	}
	return "pl.bin"
}

// planRestauration compare la carte (inventaire) a la sauvegarde : medias a
// supprimer (absents de la sauvegarde) et medias a envoyer (absents de la carte
// ou de taille differente). Les playlists sont traitees a part (etape 5).
func planRestauration(inventaire []device.FichierInfo, sauv map[string]int64) (aSupprimer, aEnvoyer []string) {
	surCarte := map[string]int64{}
	for _, f := range inventaire {
		surCarte[f.Nom] = int64(f.Taille)
		if reMedia.MatchString(f.Nom) {
			if _, ok := sauv[f.Nom]; !ok {
				aSupprimer = append(aSupprimer, f.Nom)
			}
		}
	}
	for nom, taille := range sauv {
		if !reMedia.MatchString(nom) {
			continue // systeme (fav.bin, wifi...) et playlist : traites a part ou laisses
		}
		if t, ok := surCarte[nom]; !ok || t != taille {
			aEnvoyer = append(aEnvoyer, nom)
		}
	}
	sort.Strings(aSupprimer)
	sort.Strings(aEnvoyer)
	return aSupprimer, aEnvoyer
}

// RestaurerBoite remet la boite connectee dans l'etat de sa sauvegarde (voir en
// tete). Ne retire PAS la boite de Merlinus : l'IHM appelle RetirerBoite ensuite.
func (a *App) RestaurerBoite(mac string) error {
	sauv := fichiersSauvegarde(mac)
	if len(sauv) == 0 {
		return errIHM("err_resto_sans_sauvegarde")
	}
	debut := time.Now()
	log.Printf("restauration %s : debut", mac)
	err := a.connecterPourEcrire(mac, func(c *device.Client) error { return a.restaurerBoite(c, mac, sauv) })
	if err != nil {
		log.Printf("restauration %s : echec apres %s : %v", mac, time.Since(debut).Round(time.Second), err)
	} else {
		log.Printf("restauration %s : terminee en %s", mac, time.Since(debut).Round(time.Second))
	}
	return err
}

// restaurerBoite deroule la restauration sur la boite connectee et identifiee.
func (a *App) restaurerBoite(c *device.Client, mac string, sauv map[string]int64) error {
	c.SetTimeout(10 * time.Second)
	macBytes, err := c.MACBytes()
	if err != nil {
		return errDetail("err_resto_mac", err)
	}

	// 1. Sauvegarde complete ?
	items, nomPl, pl, err := itemsSauvegarde(mac, macBytes)
	if err != nil {
		return errDetail("err_resto_sauvegarde_illisible", err)
	}
	if m := manquantsSauvegarde(items, sauv); len(m) > 0 {
		return errIHM("err_resto_incomplete", "n", len(m), "ex", m[0])
	}
	// Une sauvegarde d'avant le manifeste, validee ici, en recoit un : elle sera
	// reconnue comme complete (et conservee) si la boite est rassociee.
	if !store.Has(ficManifeste(mac)) {
		e := a.EtatSauvegarde(mac)
		if d, err := json.Marshal(manifesteSauvegarde{Date: e.Date, Fichiers: e.Fichiers, Octets: e.Octets}); err == nil {
			_ = store.Save(ficManifeste(mac), d)
		}
	}

	// 2. Inventaire de la carte.
	inventaire, err := c.ListerFichiers(func(fait, total int) {
		if total > 0 {
			a.emitProgres("prog_lecture_boite", 15*float64(fait)/float64(total))
		}
	})
	if err != nil {
		return errIHM("err_resto_inventaire")
	}
	aSupprimer, aEnvoyer := planRestauration(inventaire, sauv)
	log.Printf("restauration %s : %d a supprimer, %d a envoyer", mac, len(aSupprimer), len(aEnvoyer))

	// 3. Suppressions, verifiees une a une (op15 repond 00 meme sans effet).
	for i, nom := range aSupprimer {
		a.emitProgres("prog_resto_suppression", 15+15*float64(i)/float64(len(aSupprimer)))
		if err := c.SupprimerFichierVerifie(nom); err != nil {
			return errIHM("err_resto_suppression", "nom", nom, "detail", err.Error())
		}
	}

	if len(aSupprimer) > 0 {
		log.Printf("restauration %s : %d fichier(s) supprime(s)", mac, len(aSupprimer))
	}

	// Espace : les suppressions sont faites, les envois doivent tenir.
	var besoin int64
	for _, nom := range aEnvoyer {
		besoin += sauv[nom]
	}
	if err := a.controlerEspace(c, besoin); err != nil {
		return err
	}

	// 4. Envois des medias de la sauvegarde.
	c.SetTimeout(60 * time.Second)
	debut := time.Now()
	var faits int64
	for i, nom := range aEnvoyer {
		data, err := store.Load(dirSauvegarde(mac) + "/" + nom)
		if err != nil {
			return errIHM("err_resto_fichier_illisible", "nom", nom)
		}
		base := faits
		progres := func(envoye int64) {
			fait := base + envoye
			eta := int64(-1)
			if ec := time.Since(debut).Seconds(); ec > 1 && fait > 0 {
				eta = int64(float64(besoin-fait) / (float64(fait) / ec))
			}
			pct := 30.0
			if besoin > 0 {
				pct = 30 + 60*float64(fait)/float64(besoin)
			}
			a.emitProgresSync(ProgresSync{Libelle: "prog_resto_histoires", Pct: pct, EtaSecondes: eta,
				Faits: i, Total: len(aEnvoyer), OctetsFaits: fait, OctetsTotal: besoin})
		}
		if err := televerserBride(c, nom, data, progres); err != nil {
			log.Printf("restauration %s : envoi de %s echoue : %v", mac, nom, err)
			return errIHM("err_resto_envoi")
		}
		faits += int64(len(data))
		progres(0)
	}

	if len(aEnvoyer) > 0 {
		log.Printf("restauration %s : %d fichier(s) renvoye(s) (%s)", mac, len(aEnvoyer), moLisible(besoin))
	}

	// 5. Playlist sauvegardee posee telle quelle, puis adoption.
	a.emitProgres("prog_finalisation", 92)
	if err := c.UploadFile(nomPl, pl, nil); err != nil {
		return errIHM("err_resto_envoi_playlist")
	}
	// Variante concurrente supprimee APRES la pose de la playlist sauvegardee : la
	// carte n'est jamais sans playlist, meme si la restauration est coupee ici.
	autre := autrePlaylist(nomPl)
	if present, _, err := c.IsFileExist(autre, nil); err != nil {
		return errIHM("err_resto_lecture")
	} else if present {
		if err := c.SupprimerFichierVerifie(autre); err != nil {
			return errIHM("err_resto_suppression", "nom", autre, "detail", err.Error())
		}
	}
	c.SetTimeout(120 * time.Second)
	if st, err := c.AdopterPlaylist(); err != nil || st != 0 {
		return errIHM("err_resto_validation")
	}
	log.Printf("restauration %s : playlist d'origine adoptee", mac)

	// 6. Favoris d'origine.
	c.SetTimeout(10 * time.Second)
	if err := restaurerFavoris(c, mac, sauv); err != nil {
		return errDetail("err_resto_favoris", err)
	}
	if err := c.PurgerEcoutes(); err != nil {
		return errDetail("err_resto_ecoutes", err)
	}
	log.Printf("restauration %s : favoris d'origine remis, historique d'ecoute efface", mac)

	// 7. Verification sur un nouvel inventaire.
	c.SetTimeout(10 * time.Second)
	apres, err := c.ListerFichiers(func(fait, total int) {
		if total > 0 {
			a.emitProgres("prog_verification", 94+6*float64(fait)/float64(total))
		}
	})
	if err != nil {
		return errIHM("err_resto_verif_lecture")
	}
	restants, manquants := planRestauration(apres, sauv)
	for _, f := range apres {
		if f.Nom == autre {
			restants = append(restants, autre)
		}
	}
	if len(restants) > 0 || len(manquants) > 0 {
		ex := append(restants, manquants...)[0]
		return errIHM("err_resto_verif", "trop", len(restants), "manquants", len(manquants), "ex", ex)
	}
	_ = c.EndSync()
	// Le dossier a servi : il devient l'archive datee de cette restauration
	// (conservee). Une reassociation repartira d'un dossier de sauvegarde neuf,
	// sans melange avec ces fichiers.
	if err := store.Renommer(dirSauvegarde(mac), dirRestauration(mac, time.Now())); err != nil {
		// Signale (et non seulement journalise) : sans archivage, une
		// reassociation repartirait sur un dossier de sauvegarde melange.
		return errDetail("err_resto_archivage", err)
	}
	a.emitProgres("prog_boite_restauree", 100)
	return nil
}

// ficFavorisBoite : favoris de la boite (fav.bin, poses par l'enfant).
const ficFavorisBoite = "fav.bin"

// restaurerFavoris remet fav.bin dans l'etat de la sauvegarde. Seule ecriture
// des favoris par Merlinus (ailleurs ils sont en lecture seule, cf.
// LireFavoris) :
//   - absent de la sauvegarde : supprime (verifie) s'il est sur la boite ;
//   - deja identique sur la boite (aucun favori des deux cotes compris) : rien ;
//   - vide dans la sauvegarde (aucun favori a l'origine) : supprime, car la
//     boite n'acquitte pas un televersement de 0 octet (constate en reel) ;
//   - sinon : repose, puis relu a l'identique.
func restaurerFavoris(c *device.Client, mac string, sauv map[string]int64) error {
	if _, ok := sauv[ficFavorisBoite]; !ok {
		present, _, err := c.IsFileExist(ficFavorisBoite, nil)
		if err != nil || !present {
			return err
		}
		return c.SupprimerFichierVerifie(ficFavorisBoite)
	}
	data, err := store.Load(dirSauvegarde(mac) + "/" + ficFavorisBoite)
	if err != nil {
		return err
	}
	actuel, err := c.GetFileBytes(ficFavorisBoite)
	var refus *device.ErrFichierRefuse
	if errors.As(err, &refus) {
		actuel, err = nil, nil // absent de la boite
	}
	if err != nil {
		return err
	}
	if bytes.Equal(actuel, data) {
		return nil
	}
	if len(data) == 0 {
		return c.SupprimerFichierVerifie(ficFavorisBoite)
	}
	if err := c.UploadFile(ficFavorisBoite, data, nil); err != nil {
		return err
	}
	relu, err := c.GetFileBytes(ficFavorisBoite)
	if err != nil {
		return err
	}
	if !bytes.Equal(relu, data) {
		return fmt.Errorf("fav.bin relu different de la sauvegarde (%d o au lieu de %d)", len(relu), len(data))
	}
	return nil
}

// prefixeRestauration : dossiers d'archive des restaurations, conserves au
// retrait comme a la suppression de la boite.
const prefixeRestauration = "restauration-"

func dirRestauration(mac string, t time.Time) string {
	return dirTravailBoite(mac) + "/" + prefixeRestauration + t.Format("20060102-150405")
}

// conserveAuRetrait : sous-dossiers de travail/boites/<mac> gardes quand la
// boite quitte Merlinus (sauvegarde en cours, archives de restauration).
func conserveAuRetrait(nom string) bool {
	return nom == "sauvegarde" || strings.HasPrefix(nom, prefixeRestauration)
}

// RetirerBoite retire une boite de Merlinus (registre, cache, travail) en
// CONSERVANT sauvegarde et archives de restauration (conserveAuRetrait).
func (a *App) RetirerBoite(mac string) {
	a.modifierRegistre(func(l []BoiteMeta) []BoiteMeta {
		out := l[:0]
		for _, m := range l {
			if !memeMAC(m.MAC, mac) {
				out = append(out, m)
			}
		}
		return out
	})
	purgerBoite(mac)
}

// purgerBoite efface cache et travail d'une boite SAUF les dossiers conserves
// (conserveAuRetrait).
func purgerBoite(mac string) {
	store.RemoveDir(dirCacheBoite(mac))
	for _, n := range store.ListerDir(dirTravailBoite(mac)) {
		store.Remove(dirTravailBoite(mac) + "/" + n)
	}
	for _, d := range store.SousDossiers(dirTravailBoite(mac)) {
		if !conserveAuRetrait(d) {
			store.RemoveDir(dirTravailBoite(mac) + "/" + d)
		}
	}
}
