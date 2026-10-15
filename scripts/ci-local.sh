#!/usr/bin/env bash
#
# Rejoue la CI en local avec act (https://github.com/nektos/act).
#
# Limite structurelle : act ne sait executer que les runners Linux (il utilise
# des conteneurs Docker). Les jobs macOS et Windows de la matrice ne sont PAS
# rejouables localement ; ils ne tournent qu'en CI GitHub. Ce script ne lance
# donc que la branche Linux de la matrice.
#
# Prerequis : Docker demarre, et act installe (brew install act).
# Les reglages par defaut (image, architecture amd64) sont dans .actrc.
#
# Usage :
#   scripts/ci-local.sh                # rejoue le job Linux de ci.yml
#   scripts/ci-local.sh --list         # liste les jobs sans les executer
#   scripts/ci-local.sh -- <args act>  # passe des arguments a act

set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v act >/dev/null 2>&1; then
  echo "act absent. Installer avec : brew install act" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Docker ne repond pas. Demarre Docker puis relance." >&2
  exit 1
fi

# Tout ce qui suit un '--' litteral est transmis tel quel a act.
extra=()
if [[ "${1:-}" == "--" ]]; then
  shift
  extra=("$@")
elif [[ $# -gt 0 ]]; then
  extra=("$@")
fi

# upload-artifact@v4 exige un serveur d'artefacts local sous act.
ART_DIR="${TMPDIR:-/tmp}/act-artifacts"
mkdir -p "$ART_DIR"

echo "== CI locale (job Linux uniquement) =="
echo "   artefacts -> $ART_DIR"
# ${extra[@]+...} : developpement sur du bash 3.2 (macOS) sans declencher set -u
# quand le tableau est vide.
exec act push -j build --matrix name:linux \
  --artifact-server-path "$ART_DIR" \
  ${extra[@]+"${extra[@]}"}
