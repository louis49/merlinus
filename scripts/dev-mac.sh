#!/usr/bin/env bash
#
# Build + lancement local de Merlinus pour le dev sur macOS.
#
# 1. Tue les instances precedentes de l'app.
# 2. Compile en mode debug (devtools + inspecteur actives).
# 3. Lance le binaire en avant-plan : les logs s'affichent dans le terminal,
#    Ctrl+C arrete l'app.
#
# Pour une boucle a rechargement a chaud (frontend + Go), preferer `wails dev`.
# Ce script sert quand on veut tester le vrai bundle .app compile.
#
# Usage :
#   scripts/dev-mac.sh                 # build debug + lancement
#   scripts/dev-mac.sh -- <flags wails># passe des flags a `wails build`

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ "$(uname)" != "Darwin" ]]; then
  echo "Ce script est prevu pour macOS. Sur les autres OS, utiliser 'wails build'." >&2
  exit 1
fi

# CLI Wails installe via 'go install' (typiquement ~/go/bin).
export PATH="$PATH:$(go env GOPATH)/bin"
if ! command -v wails >/dev/null 2>&1; then
  echo "wails introuvable. Installer : go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0" >&2
  exit 1
fi

APP="build/bin/Merlinus.app"
BIN="$APP/Contents/MacOS/Merlinus"

# Flags supplementaires eventuels apres '--'.
extra=()
if [[ "${1:-}" == "--" ]]; then
  shift
  extra=("$@")
fi

echo "== Arret des instances precedentes =="
# Cible le binaire de notre bundle, pas un vague 'merlinus'.
pkill -f "Merlinus\.app/Contents/MacOS/Merlinus" 2>/dev/null && echo "  instance(s) arretee(s)" || echo "  aucune instance"

echo "== Build (debug) =="
wails build -debug ${extra[@]+"${extra[@]}"}

if [[ ! -x "$BIN" ]]; then
  echo "Binaire introuvable apres build : $BIN" >&2
  exit 1
fi

echo "== Lancement ($BIN) =="
echo "   (Ctrl+C pour arreter ; logs ci-dessous)"
exec "$BIN"
