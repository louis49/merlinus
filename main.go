package main

import (
	"embed"
	"log"

	"github.com/louis49/merlinus/internal/app"
)

// assets embarque le frontend compile. La directive //go:embed doit resider a la
// racine du module (chemin relatif a ce fichier) ; tout le reste de la logique
// vit dans internal/app.
//
//go:embed all:frontend/dist
var assets embed.FS

// icone : icone de fenetre (Linux ; macOS et Windows la tirent du bundle).
//
//go:embed build/appicon.png
var icone []byte

// wailsJSON : configuration Wails, dont info.productVersion (ecrit par le
// workflow de release depuis le tag) donne la version de l'application.
//
//go:embed wails.json
var wailsJSON []byte

func main() {
	if err := app.Run(assets, icone, wailsJSON); err != nil {
		log.Printf("demarrage impossible : %v", err) // aussi dans merlinus.log (voir app.ouvrirJournal)
		println("Error:", err.Error())
	}
}
