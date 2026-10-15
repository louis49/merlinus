// i18n centralisé : dictionnaires fr / en + une langue de DEBUG où t(cle) renvoie
// la clé elle-même. En mode debug, tout texte affiché en clair (lisible) est un
// texte NON internationalisé : repérage visuel immédiat.
//
// Sélection de la langue (par priorité) :
//   1. localStorage 'merlinus.lang' = 'fr' | 'en' | 'debug'
//   2. paramètre d'URL ?lang=...
//   3. langue du système (fr -> fr, sinon en)
//
// Bascule pratique en console (build -debug) :
//   merlinusSetLang('debug')  merlinusSetLang('en')  merlinusSetLang('')  // reset

import fr from './locales/fr.json';
import en from './locales/en.json';

const DICTS = { fr, en };

function resoudreLangue() {
  let l = null;
  try { l = localStorage.getItem('merlinus.lang'); } catch (_) { /* indispo */ }
  if (!l) {
    try { l = new URLSearchParams(location.search).get('lang'); } catch (_) { /* ignore */ }
  }
  if (l === 'debug' || l === 'fr' || l === 'en') return l;
  return (navigator.language || 'fr').toLowerCase().startsWith('fr') ? 'fr' : 'en';
}

export const lang = resoudreLangue();

// t(cle, vars) : texte localisé, interpolation simple de {placeholders}.
// En mode debug, renvoie la clé brute (les vars sont ignorées).
export function t(cle, vars) {
  if (lang === 'debug') return cle;
  const dict = DICTS[lang] || fr;
  let s = dict[cle] ?? cle;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.split('{' + k + '}').join(v);
    }
  }
  return s;
}

// plur(n, cleUn, cleN, vars) : choisit le singulier/pluriel selon n, avec la
// regle de la langue (CLDR) : en francais 0 et 1 sont au singulier, en anglais
// seulement 1 ("0 stories").
export function plur(n, cleUn, cleN, vars) {
  const a = Math.abs(n);
  const singulier = lang === 'en' ? a === 1 : a < 2;
  return t(singulier ? cleUn : cleN, Object.assign({ n }, vars || {}));
}

// Bascule de langue depuis la console (utile en développement).
try {
  window.merlinusSetLang = function (l) {
    try { l ? localStorage.setItem('merlinus.lang', l) : localStorage.removeItem('merlinus.lang'); } catch (_) { /* ignore */ }
    location.reload();
  };
} catch (_) { /* pas de window */ }
