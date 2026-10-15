package app

// Sequences reseau contre la fausse boite (fausseboite_test.go) : identite,
// sauvegarde complete, publication et restauration. Le store est redirige vers
// un dossier temporaire (main_test.go) ; chaque test utilise sa propre cle de
// boite.

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/louis49/merlinus/internal/device"
	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/playlist"
	"github.com/louis49/merlinus/internal/store"
)

// contenu : octets distincts par nom, de taille donnee.
func contenu(nom string, taille int) []byte {
	return bytes.Repeat([]byte(nom[:1]), taille)
}

// boiteVierge : purge l'etat local de la boite mac (tests repetes avec
// -count, qui partagent le dossier temporaire du TestMain) et le renvoie.
func boiteVierge(t *testing.T, mac string) string {
	t.Helper()
	for _, d := range []string{dirTravailBoite(mac), dirCacheBoite(mac)} {
		if err := store.RemoveDir(d); err != nil {
			t.Fatal(err)
		}
	}
	return mac
}

// indice : position de op dans le journal, -1 si absente.
func indice(journal []string, op string) int { return slices.Index(journal, op) }

// --- Identite ---

func TestVerifierIdentite(t *testing.T) {
	a := NewApp()
	a.modifierRegistre(func(l []BoiteMeta) []BoiteMeta {
		return append(l, BoiteMeta{MAC: "02:aa:aa:aa:aa:01", Nom: "Chambre"})
	})
	cas := []struct {
		nom     string
		sansMAC bool
		vise    string
		cle     string
	}{
		{"meme boite", false, macTestTexte, ""},
		{"meme boite, casse differente", false, strings.ToUpper(macTestTexte), ""},
		{"autre boite connue", false, "02:aa:aa:aa:aa:01", "err_autre_boite"},
		{"autre boite inconnue", false, "02:bb:bb:bb:bb:01", "err_autre_boite_anonyme"},
		{"MAC illisible", true, macTestTexte, "err_identite_illisible"},
	}
	for _, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			b := nouvelleFausseBoite(t)
			b.sansMAC = tc.sansMAC
			err := a.verifierIdentite(b.connecter(), tc.vise)
			if tc.cle == "" && err != nil {
				t.Fatalf("refus inattendu : %v", err)
			}
			if tc.cle != "" && cleErreur(err) != tc.cle {
				t.Fatalf("erreur %q attendue, obtenu %v", tc.cle, err)
			}
		})
	}
}

// --- Sauvegarde complete ---

// boiteASauvegarder : playlist, medias de H1, un fichier systeme et deux noms
// douteux (a ignorer).
func boiteASauvegarder(t *testing.T) (*fausseBoite, []*playlist.Node) {
	b := nouvelleFausseBoite(t)
	arbre := arbreReel()
	b.poserPlaylist(arbre)
	b.fichiers[pH1+".mp3"] = contenu("m", 3000)
	b.fichiers[pH1+".jpg"] = contenu("j", 500)
	b.fichiers["fav.bin"] = contenu("f", 10)
	b.fichiers[".cache"] = contenu("x", 10)
	b.fichiers["CON.txt"] = contenu("x", 10)
	return b, arbre
}

func inventaireDe(t *testing.T, c *device.Client) []device.FichierInfo {
	t.Helper()
	inv, err := c.ListerFichiers(nil)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestSauvegarderBoiteComplete(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5a:01")
	b, arbre := boiteASauvegarder(t)
	c := b.connecter()
	if err := NewApp().sauvegarderBoite(c, arbre, inventaireDe(t, c), []byte("clair"), mac, 1<<30); err != nil {
		t.Fatal(err)
	}
	for _, nom := range []string{"pl_enc.bin", pH1 + ".mp3", pH1 + ".jpg", "fav.bin"} {
		d, err := store.Load(dirSauvegarde(mac) + "/" + nom)
		if err != nil || !bytes.Equal(d, b.fichiers[nom]) {
			t.Errorf("%s mal sauvegarde (%v)", nom, err)
		}
	}
	for _, nom := range []string{".cache", "CON.txt"} {
		if slices.Contains(b.operations("get "), "get "+nom) {
			t.Errorf("nom douteux %s rapatrie", nom)
		}
	}
	if !store.Has(cheminAffichage(mac, pH1)) {
		t.Error("illustration non copiee dans le cache d'affichage")
	}
	if !store.Has(ficManifeste(mac)) {
		t.Error("manifeste absent apres une sauvegarde complete")
	}
}

func TestSauvegarderBoiteReprise(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5a:02")
	b, arbre := boiteASauvegarder(t)
	// Deja rapatrie a la bonne taille : pas de second transfert.
	if err := store.Save(dirSauvegarde(mac)+"/"+pH1+".mp3", b.fichiers[pH1+".mp3"]); err != nil {
		t.Fatal(err)
	}
	// Present mais tronque : retransfere.
	if err := store.Save(dirSauvegarde(mac)+"/"+pH1+".jpg", []byte("tronque")); err != nil {
		t.Fatal(err)
	}
	c := b.connecter()
	if err := NewApp().sauvegarderBoite(c, arbre, inventaireDe(t, c), nil, mac, 0); err != nil {
		t.Fatal(err)
	}
	gets := b.operations("get ")
	if slices.Contains(gets, "get "+pH1+".mp3") {
		t.Error("fichier deja sauvegarde retransfere")
	}
	if !slices.Contains(gets, "get "+pH1+".jpg") {
		t.Error("fichier tronque non retransfere")
	}
}

func TestSauvegarderBoiteEchecs(t *testing.T) {
	cas := []struct {
		nom      string
		panne    func(b *fausseBoite)
		capacite uint32
		attendu  string
	}{
		{"inventaire plus grand que la carte", func(*fausseBoite) {}, 1000, "capacite"},
		{"taille recue differente", func(b *fausseBoite) { b.tailleFausse[pH1+".mp3"] = 4000 }, 1 << 30, "annonces"},
		{"coupure pendant un transfert", func(b *fausseBoite) { b.coupeGet[pH1+".jpg"] = true }, 1 << 30, ""},
	}
	for i, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			mac := boiteVierge(t, "02:00:00:00:5b:0"+string(rune('1'+i)))
			b, arbre := boiteASauvegarder(t)
			tc.panne(b)
			c := b.connecter()
			err := NewApp().sauvegarderBoite(c, arbre, inventaireDe(t, c), nil, mac, tc.capacite)
			if err == nil || !strings.Contains(err.Error(), tc.attendu) {
				t.Fatalf("echec contenant %q attendu, obtenu %v", tc.attendu, err)
			}
			if store.Has(ficManifeste(mac)) {
				t.Error("manifeste ecrit pour une sauvegarde incomplete")
			}
		})
	}
}

// --- Publication ---

// preparerPublication : la boite porte arbreReel (H1, H2 dans Cat) et ses
// medias, plus un media orphelin ; le travail retire H2 et ajoute H3, dont
// l'audio et l'illustration sont en bibliotheque.
func preparerPublication(t *testing.T, mac string) *fausseBoite {
	t.Helper()
	b := nouvelleFausseBoite(t)
	b.poserPlaylist(arbreReel())
	for _, u := range []string{pH1, pH2} {
		b.fichiers[u+".mp3"] = contenu("m"+u, 3000)
		b.fichiers[u+".jpg"] = contenu("j"+u, 500)
	}
	b.fichiers[pCat+".jpg"] = contenu("c", 500)
	b.fichiers["a0000000-0000-4000-8000-0000000000ff.mp3"] = contenu("o", 100) // orphelin
	b.fichiers["fav.bin"] = contenu("f", 10)

	travail := `[{"uuid":"` + pCat + `","title":"Cat","child":[
		{"uuid":"` + pH1 + `","title":"H1"},
		{"uuid":"` + pH2 + `","title":"H2","_rem":true},
		{"uuid":"` + pH3 + `","title":"H3","_new":true}]}]`
	for rel, d := range map[string][]byte{
		ficTravail(mac):                 []byte(travail),
		library.CheminAudio(pH3, "mp3"): contenu("h", 4000),
		library.CheminIllustration(pH3): contenu("i", 600),
	} {
		if err := store.Save(rel, d); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestPublierEditions(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5c:01")
	b := preparerPublication(t, mac)
	b.fichiers["pl.bin"] = contenu("p", 96) // variante perimee
	b.libre = 123456789
	a := NewApp()
	a.majMeta(BoiteMeta{MAC: mac, StockageTotal: 1, StockageLibre: 1})
	if err := a.publierEditions(b.connecter(), mac); err != nil {
		t.Fatal(err)
	}
	// Stockage releve en fin de publication (jauge de l'IHM).
	for _, m := range a.lireRegistre() {
		if memeMAC(m.MAC, mac) && (m.StockageLibre != 123456789 || m.StockageTotal != 4<<30-1) {
			t.Errorf("stockage non releve : %+v", m)
		}
	}

	// Ordre : medias de H3, puis l'arbre, puis adoption, puis menage.
	j := b.operations("")
	ordre := []string{"upload " + pH3 + ".mp3", "upload " + pH3 + ".jpg", "upload pl_enc.bin",
		"get check_playlist.cmd", "suppr pl.bin", "get clean_sd.cmd"}
	prec := -1
	for _, op := range ordre {
		i := indice(j, op)
		if i <= prec {
			t.Fatalf("%q absente ou hors ordre dans %v", op, j)
		}
		prec = i
	}

	// Arbre adopte : H1 et H3, plus H2 ; favori unique conserve.
	f := playlist.Feuilles(b.arbreAdopte())
	if !f[pH1] || !f[pH3] || f[pH2] {
		t.Errorf("feuilles adoptees = %v", f)
	}
	if playlist.CompterFavoris(b.arbreAdopte()) != 1 {
		t.Error("categorie favoris perdue")
	}
	// Menage simule : H2 et l'orphelin effaces, le reste intact.
	for _, nom := range []string{pH2 + ".mp3", pH2 + ".jpg", "a0000000-0000-4000-8000-0000000000ff.mp3"} {
		if _, ok := b.fichiers[nom]; ok {
			t.Errorf("%s non efface par le menage", nom)
		}
	}
	for _, nom := range []string{pH1 + ".mp3", pH3 + ".mp3", pH3 + ".jpg", "fav.bin"} {
		if _, ok := b.fichiers[nom]; !ok {
			t.Errorf("%s absent apres publication", nom)
		}
	}
	// Filets et etat local.
	if len(store.ListerDir(dirBackups(mac))) == 0 {
		t.Error("arbre non exporte avant publication")
	}
	if len(store.ListerDir(dirPl(mac))) == 0 {
		t.Error("playlist relue non conservee")
	}
	if !store.Has(ficArbre(mac)) || !store.Has(cheminAffichage(mac, pH3)) {
		t.Error("etat local non mis a jour")
	}
	if store.Has(ficTravail(mac)) {
		t.Error("travail non solde apres publication")
	}
}

// Une modification du travail PENDANT la synchro n'est pas perdue : le travail
// n'est solde que s'il est identique a celui lu au debut.
func TestPublierEditionsTravailModifiePendant(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5c:03")
	b := preparerPublication(t, mac)
	modifie := []byte(`[{"uuid":"` + pCat + `","title":"Cat modifie pendant"}]`)
	b.surGet = func(nom string) {
		if nom == "check_playlist.cmd" {
			if err := store.Save(ficTravail(mac), modifie); err != nil {
				t.Error(err)
			}
		}
	}
	if err := NewApp().publierEditions(b.connecter(), mac); err != nil {
		t.Fatal(err)
	}
	if d, err := store.Load(ficTravail(mac)); err != nil || !bytes.Equal(d, modifie) {
		t.Errorf("travail modifie pendant la synchro perdu (%v)", err)
	}
}

// Reprise : un media deja present et identique sur la boite n'est pas renvoye.
func TestPublierEditionsReprise(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5c:02")
	b := preparerPublication(t, mac)
	b.fichiers[pH3+".mp3"] = contenu("h", 4000)
	if err := NewApp().publierEditions(b.connecter(), mac); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(b.operations("upload "), "upload "+pH3+".mp3") {
		t.Error("media identique renvoye")
	}
	if !slices.Contains(b.operations("upload "), "upload "+pH3+".jpg") {
		t.Error("illustration manquante non envoyee")
	}
}

// Echecs : l'arbre en place n'est jamais remplace (ni adopte, ni menage) si une
// etape anterieure echoue.
func TestPublierEditionsEchecs(t *testing.T) {
	cas := []struct {
		nom      string
		panne    func(b *fausseBoite, mac string)
		cle      string
		adoption bool // check_playlist.cmd atteint
	}{
		{"envoi d'un media refuse", func(b *fausseBoite, _ string) { b.refusUpload[pH3+".mp3"] = true }, "err_envoi_echoue", false},
		{"envoi de l'arbre refuse", func(b *fausseBoite, _ string) { b.refusUpload["pl_enc.bin"] = true }, "err_envoi_echoue", false},
		{"espace insuffisant", func(b *fausseBoite, _ string) { b.libre = 1000 }, "err_espace", false},
		{"arbre refuse par la boite", func(b *fausseBoite, _ string) { b.statutAdopte = 1 }, "err_playlist_refusee", true},
		{"retrait non demande (anti-wipe)", func(_ *fausseBoite, mac string) {
			_ = store.Save(ficTravail(mac), []byte(`[{"uuid":"`+pCat+`","title":"Cat","child":[{"uuid":"`+pH1+`","title":"H1"}]}]`))
		}, "err_anti_wipe", false},
		{"audio absent de la bibliotheque", func(*fausseBoite, string) {
			_ = store.Remove(library.CheminAudio(pH3, "mp3"))
		}, "err_audio_introuvable", false},
	}
	for i, tc := range cas {
		t.Run(tc.nom, func(t *testing.T) {
			mac := boiteVierge(t, "02:00:00:00:5d:0"+string(rune('1'+i)))
			b := preparerPublication(t, mac)
			avant := b.fichiers["pl_enc.bin"]
			tc.panne(b, mac)
			err := NewApp().publierEditions(b.connecter(), mac)
			if cleErreur(err) != tc.cle {
				t.Fatalf("erreur %q attendue, obtenu %v", tc.cle, err)
			}
			if slices.Contains(b.operations("get "), "get clean_sd.cmd") {
				t.Error("menage lance apres un echec")
			}
			if got := slices.Contains(b.operations("get "), "get check_playlist.cmd"); got != tc.adoption {
				t.Errorf("adoption tentee = %v, attendu %v", got, tc.adoption)
			}
			if !tc.adoption && !bytes.Equal(b.fichiers["pl_enc.bin"], avant) {
				t.Error("arbre en place remplace malgre l'echec")
			}
		})
	}
}

// --- Restauration ---

// preparerRestauration : la sauvegarde porte arbreReel et tous ses medias ; la
// boite a depuis recu H3, perdu l'audio de H2, une version differente de
// l'audio de H1, et un pl.bin concurrent.
func preparerRestauration(t *testing.T, mac string) (*fausseBoite, map[string][]byte) {
	t.Helper()
	b := nouvelleFausseBoite(t)
	sauv := map[string][]byte{"pl_enc.bin": b.poserPlaylist(arbreReel())}
	for _, u := range []string{pCat, pH1, pH2, pFav} {
		sauv[u+".jpg"] = contenu("j"+u, 500)
	}
	sauv[pH1+".mp3"] = contenu("a", 3000)
	sauv[pH2+".mp3"] = contenu("b", 3000)
	sauv["fav.bin"] = contenu("v", 48) // favoris d'origine
	for nom, d := range sauv {
		if err := store.Save(dirSauvegarde(mac)+"/"+nom, d); err != nil {
			t.Fatal(err)
		}
	}

	arbreActuel := arbreReel()
	arbreActuel[0].Children = append(arbreActuel[0].Children, &playlist.Node{UUID: pH3, Title: "H3"})
	b.poserPlaylist(arbreActuel)
	for nom, d := range sauv {
		if nom != "pl_enc.bin" && nom != pH2+".mp3" {
			b.fichiers[nom] = d
		}
	}
	b.fichiers[pH1+".mp3"] = contenu("z", 2000)
	b.fichiers[pH3+".mp3"] = contenu("h", 4000)
	b.fichiers[pH3+".jpg"] = contenu("i", 600)
	b.fichiers["pl.bin"] = contenu("p", 96)
	b.fichiers["fav.bin"] = contenu("f", 10)
	return b, sauv
}

func TestRestaurerBoite(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5e:01")
	b, sauv := preparerRestauration(t, mac)
	if err := NewApp().restaurerBoite(b.connecter(), mac, fichiersSauvegarde(mac)); err != nil {
		t.Fatal(err)
	}
	j := b.operations("")
	// Suppressions avant envois (la place est liberee d'abord), playlist posee
	// avant le retrait de la variante concurrente, puis adoption.
	ordre := []string{"suppr " + pH3 + ".jpg", "suppr " + pH3 + ".mp3", "upload " + pH1 + ".mp3",
		"upload " + pH2 + ".mp3", "upload pl_enc.bin", "suppr pl.bin", "get check_playlist.cmd"}
	prec := -1
	for _, op := range ordre {
		i := indice(j, op)
		if i <= prec {
			t.Fatalf("%q absente ou hors ordre dans %v", op, j)
		}
		prec = i
	}
	if slices.Contains(j, "get clean_sd.cmd") {
		t.Error("la restauration ne doit pas lancer clean_sd.cmd")
	}
	// Carte identique a la sauvegarde, favoris d'origine compris (la boite
	// avait d'autres favoris).
	for nom, d := range sauv {
		if !bytes.Equal(b.fichiers[nom], d) {
			t.Errorf("%s different de la sauvegarde", nom)
		}
	}
	if len(b.fichiers) != len(sauv) {
		t.Errorf("carte apres restauration : %v", b.noms())
	}
	if indice(j, "upload fav.bin") < indice(j, "get check_playlist.cmd") {
		t.Error("favoris reposes avant l'adoption de l'arbre")
	}
	if indice(j, "upload clean_listening.cmd") < indice(j, "get check_playlist.cmd") {
		t.Errorf("historique d'ecoute non purge apres l'adoption : %v", j)
	}
	// Sauvegarde archivee.
	if store.Has(dirSauvegarde(mac) + "/pl_enc.bin") {
		t.Error("sauvegarde non archivee")
	}
	archive := false
	for _, d := range store.SousDossiers(dirTravailBoite(mac)) {
		archive = archive || strings.HasPrefix(d, prefixeRestauration)
	}
	if !archive {
		t.Error("archive de restauration absente")
	}
}

// Sauvegarde sans fav.bin (boite sans favoris a l'origine) : les favoris poses
// depuis sont supprimes.
func TestRestaurerBoiteSansFavorisDOrigine(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5e:03")
	b, _ := preparerRestauration(t, mac)
	if err := store.Remove(dirSauvegarde(mac) + "/fav.bin"); err != nil {
		t.Fatal(err)
	}
	if err := NewApp().restaurerBoite(b.connecter(), mac, fichiersSauvegarde(mac)); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.fichiers["fav.bin"]; ok {
		t.Error("favoris poses depuis la sauvegarde non supprimes")
	}
}

// Favoris : la restauration ne touche fav.bin que s'il differe de la sauvegarde,
// et un fav.bin vide (aucun favori) se restaure par suppression, jamais par un
// televersement de 0 octet.
func TestRestaurerBoiteFavoris(t *testing.T) {
	cas := []struct {
		nom          string
		sauv, boite  []byte // nil = absent
		upload, supp bool
		attendu      []byte // fav.bin sur la boite apres ; nil = absent
	}{
		{"aucun favori des deux cotes (vides)", []byte{}, []byte{}, false, false, []byte{}},
		{"aucun favori : vide sauvegarde, absent boite", []byte{}, nil, false, false, nil},
		{"aucun favori a l'origine, favoris poses depuis", []byte{}, contenu("f", 16), false, true, nil},
		{"memes favoris", contenu("v", 16), contenu("v", 16), false, false, contenu("v", 16)},
		{"favoris differents", contenu("v", 16), contenu("f", 32), true, false, contenu("v", 16)},
	}
	for i, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			mac := boiteVierge(t, "02:00:00:00:5e:1"+string(rune('0'+i)))
			b, _ := preparerRestauration(t, mac)
			if err := store.Save(dirSauvegarde(mac)+"/fav.bin", c.sauv); err != nil {
				t.Fatal(err)
			}
			delete(b.fichiers, "fav.bin")
			if c.boite != nil {
				b.fichiers["fav.bin"] = c.boite
			}
			if err := NewApp().restaurerBoite(b.connecter(), mac, fichiersSauvegarde(mac)); err != nil {
				t.Fatal(err)
			}
			j := b.operations("")
			if got := slices.Contains(j, "upload fav.bin"); got != c.upload {
				t.Errorf("upload fav.bin = %v, attendu %v", got, c.upload)
			}
			if got := slices.Contains(j, "suppr fav.bin"); got != c.supp {
				t.Errorf("suppression fav.bin = %v, attendu %v", got, c.supp)
			}
			d, ok := b.fichiers["fav.bin"]
			if (c.attendu == nil) != !ok || (ok && !bytes.Equal(d, c.attendu)) {
				t.Errorf("fav.bin apres = %q (present %v), attendu %q", d, ok, c.attendu)
			}
		})
	}
}

// Sauvegarde incomplete : refus avant toute ecriture sur la boite.
func TestRestaurerBoiteSauvegardeIncomplete(t *testing.T) {
	mac := boiteVierge(t, "02:00:00:00:5e:02")
	b, _ := preparerRestauration(t, mac)
	if err := store.Remove(dirSauvegarde(mac) + "/" + pH2 + ".mp3"); err != nil {
		t.Fatal(err)
	}
	err := NewApp().restaurerBoite(b.connecter(), mac, fichiersSauvegarde(mac))
	if cleErreur(err) != "err_resto_incomplete" {
		t.Fatalf("err_resto_incomplete attendu, obtenu %v", err)
	}
	if n := len(b.operations("suppr ")) + len(b.operations("upload ")); n != 0 {
		t.Errorf("%d ecritures sur la boite malgre le refus", n)
	}
}
