package app

import (
	"embed"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/louis49/merlinus/internal/library"
	"github.com/louis49/merlinus/internal/store"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// handlerAssets route les requetes non servies par les assets embarques :
//
//	/boiteimg/<mac>/<uuid> : illustration d'arbre en cache d'affichage
//	/catimg?u=<url>        : image du catalogue, cache disque (cf. catalogue.go)
//	/biblioaudio/<uuid>    : audio de la bibliotheque (lecteur de "Mes histoires")
//
// Tout le reste tombe en 404 (l'app est une page unique).
func handlerAssets() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/boiteimg/", servirImageBoite)
	mux.HandleFunc("/catimg", imageCatalogue)
	mux.HandleFunc("/biblioaudio/", servirAudioBibliotheque)
	return mux
}

// reUUIDMedia : nom de media de la bibliotheque accepte (pas de chemin).
var reUUIDMedia = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// servirAudioBibliotheque sert l'audio local d'un contenu (MP3 ou AAC) avec
// prise en charge des requetes Range (http.ServeContent) : le lecteur peut
// se positionner dans le fichier sans tout charger.
func servirAudioBibliotheque(w http.ResponseWriter, r *http.Request) {
	uuid := strings.TrimPrefix(r.URL.Path, "/biblioaudio/")
	ext := library.ExtensionAudio(uuid)
	if !reUUIDMedia.MatchString(uuid) || ext == "" {
		http.NotFound(w, r)
		return
	}
	dir, err := store.Dir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(dir, library.CheminAudio(uuid, ext)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ext == "mp3" {
		w.Header().Set("Content-Type", "audio/mpeg")
	} else {
		w.Header().Set("Content-Type", "audio/aac")
	}
	http.ServeContent(w, r, uuid+"."+ext, st.ModTime(), f)
}

// servirImageBoite sert l'illustration d'arbre en cache d'affichage
// (cache/boites/<mac>/affichage/<uuid>.jpg), avec repli sur la bibliotheque pour
// un contenu ajoute pas encore pousse.
func servirImageBoite(w http.ResponseWriter, r *http.Request) {
	sain := func(s string) bool {
		return s != "" && !strings.ContainsAny(s, "/\\") && !strings.Contains(s, "..")
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/boiteimg/"), "/", 2)
	if len(parts) != 2 || !sain(parts[0]) || !sain(parts[1]) {
		http.NotFound(w, r)
		return
	}
	data, err := store.Load(cheminAffichage(parts[0], parts[1]))
	if err != nil || len(data) == 0 {
		// Repli : contenu ajoute pas encore pousse -> image en bibliotheque.
		data, err = store.Load(library.CheminIllustration(parts[1]))
	}
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=3600") // re-rendus de l'arbre : sert l'image du cache sans re-fetch (anti-scintillement)
	w.Write(data)
}

// Run cree l'application et lance la fenetre Wails. `assets` est le systeme de
// fichiers embarque (frontend/dist), fourni par le package main (racine) car la
// directive //go:embed doit y resider.
func Run(assets embed.FS, icone []byte) error {
	if j := ouvrirJournal(); j != nil {
		defer j.Close()
	}
	// Windows : profil WebView2 (cache, localStorage) dans le dossier de donnees
	// (%LOCALAPPDATA%\merlinus\webview) plutot que %APPDATA%\Merlinus.exe
	// (profil itinerant). Vide si le dossier est introuvable : defaut de Wails.
	profilWebview := ""
	if d, err := store.Dir(); err == nil {
		profilWebview = filepath.Join(d, "webview")
	}
	a := NewApp()
	return wails.Run(&options.App{
		Title:            "Merlinus",
		Width:            1280,
		Height:           800, // taille de repli ; l'app s'ouvre maximisee
		MinWidth:         900, // en dessous (~870 px), le bloc de la boite en haut a droite deborde
		MinHeight:        600,
		WindowStartState: options.Maximised,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: handlerAssets(),
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		// Glisser-deposer de fichiers : Wails renvoie les chemins absolus des
		// fichiers laches sur un element marque `--wails-drop-target: drop`.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		OnStartup:   a.startup,
		Bind:        []interface{}{a},
		Windows: &windows.Options{
			WebviewUserDataPath: profilWebview,
			// Options non nulles : Wails applique alors leurs valeurs zero, qui
			// couperaient le zoom (Ctrl + molette) actif par defaut. On le garde.
			IsZoomControlEnabled: true,
		},
		Linux: &linux.Options{
			Icon:        icone,      // fenetre et barre des taches
			ProgramName: "merlinus", // WM_CLASS : rattache la fenetre au lanceur merlinus.desktop
			// Sans options Linux, Wails force le rendu logiciel (issue wails #2977) ;
			// des options non nulles le repasseraient "a la demande". On garde le
			// comportement eprouve : a reevaluer seulement sur mesure reelle sous Linux.
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	})
}
