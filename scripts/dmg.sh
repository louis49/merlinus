#!/usr/bin/env bash
#
# Fabrique l'image disque macOS de Merlinus : une fenetre avec Merlinus.app et
# un raccourci vers Applications, a glisser l'un sur l'autre. Outil integre
# (hdiutil), sans dependance : utilisable en CI comme en local.
#
# Prerequis : build/bin/Merlinus.app deja construit (ex. wails build -platform
# darwin/universal).
#
# Usage : scripts/dmg.sh <fichier.dmg>

set -euo pipefail

cd "$(dirname "$0")/.."

sortie="${1:?usage : scripts/dmg.sh <fichier.dmg>}"
app="build/bin/Merlinus.app"
[[ -d "$app" ]] || { echo "absent : $app (lancer wails build avant)" >&2; exit 1; }

scene="$(mktemp -d)"
trap 'rm -rf "$scene"' EXIT
# ditto preserve les liens symboliques et les attributs du bundle.
ditto "$app" "$scene/Merlinus.app"
ln -s /Applications "$scene/Applications"

rm -f "$sortie"
hdiutil create -volname "Merlinus" -srcfolder "$scene" -fs HFS+ -format UDZO -ov "$sortie" >/dev/null
echo "image disque : $sortie ($(du -h "$sortie" | cut -f1))"
