// Composants d'interface réutilisables.
import { ICONES } from './icones.js';
import { t } from './i18n.js';
import { fmtOctets } from './format.js';

// esc : echappe une chaine pour un contexte TEXTUEL HTML (via textContent, qui
// neutralise < > &). N'echappe PAS le guillemet : en contexte d'attribut,
// utiliser escAttr (ou affecter .value cote DOM).
export function esc(s) { const d = document.createElement('div'); d.textContent = String(s == null ? '' : s); return d.innerHTML; }
// escAttr : echappe le guillemet double pour une valeur d'attribut entre "".
export function escAttr(s) { return String(s == null ? '' : s).replace(/"/g, '&quot;'); }

// Palette des degrades (vignettes sans image, boites) : paires [debut, fin].
export const PAL = [['#E890A9', '#8CA0E6'], ['#63B9A2', '#8CA0E6'], ['#EFA478', '#E890A9'], ['#8CA0E6', '#63B9A2'], ['#E9C795', '#EFA478'], ['#8CA0E6', '#E890A9']];

// icone : rend un pictogramme Lucide (ICONES = contenu SVG interne) dans un <svg>
// stroke=currentColor, donc il prend la couleur du contexte.
export function icone(nom, cls) {
  const p = ICONES[nom]; if (!p) return '';
  return '<svg class="ic' + (cls ? ' ' + cls : '') + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + p + '</svg>';
}

// imgCat : route une image DISTANTE du catalogue (iTunes/RF/flux RSS) par le
// cache disque local (handler /catimg), qui la telecharge au premier acces puis
// la sert depuis le disque. Liste blanche : seules les illustrations locales
// (boiteimg/...) et les data URL d'image en base64 sont renvoyees telles quelles ;
// toute autre forme (venue d'un flux RSS, donc externe) donne une chaine vide.
export function imgCat(url, hd = true) {
  if (!url) return '';
  if (/^https?:\/\//i.test(url)) return 'catimg?u=' + encodeURIComponent(hd ? imgHD(url) : url);
  if (/^boiteimg\/[A-Za-z0-9%._-]+\/[A-Za-z0-9%._-]+$/.test(url)) return url;
  if (/^data:image\/[a-z0-9.+-]+;base64,[A-Za-z0-9+/=]+$/i.test(url)) return url;
  return '';
}

// Taille demandee aux CDN qui la parametrent dans l'URL : iTunes (mzstatic,
// ".../170x170bb.png") et Radio France (pikapi, ".../400x400?webp=true").
// 800 = maximum accepte par pikapi (1200 refuse) ; couvre les vignettes de
// l'accueil (jusqu'a ~440 px affiches, ~880 px reels sur ecran Retina). Le
// catalogue iTunes initial ne fournit que du 170 px, d'ou la pixelisation.
const TAILLE_HD = 800;
export function imgHD(url) {
  if (!url) return url;
  // iTunes : carre "bb" (boite englobante, ratio conserve par le CDN).
  let u = url.replace(/\/\d+x\d+bb(?:-\d+)?\.(?:png|jpe?g|webp)(\?[^/]*)?$/i, '/' + TAILLE_HD + 'x' + TAILLE_HD + 'bb.jpg');
  // Radio France : formats CARRES seulement (les 16:9 des episodes gardent leur cadrage).
  u = u.replace(/(\/pikapi\/images\/[^/?#]+\/)(\d+)x(\d+)(?=[?#]|$)/, (m, debut, w, h) => (w === h ? debut + TAILLE_HD + 'x' + TAILLE_HD : m));
  return u;
}

// barreStockage : barre de remplissage SANS valeur chiffrée, qui montre en plus
// le différentiel de la prochaine synchro par des blocs colorés dans la barre :
//   - bloc vert  (sb-add) = espace des contenus à AJOUTER (après le remplissage)
//   - bloc rouge (sb-rem) = espace des contenus à RETIRER (fin du remplissage)
// Le reste (sb-stay) est le contenu qui reste en place.
//
// opts : { usedMo, totalMo, addMo?, remMo?, classe? }
export function barreStockage(opts) {
  const total = opts.totalMo || 1;
  const usedMo = opts.usedMo || 0;
  const addMo = opts.addMo || 0;
  const remMo = opts.remMo || 0;
  const clamp = (v) => Math.max(0, Math.min(100, v));
  const stay = clamp(((usedMo - remMo) / total) * 100);
  const rem = clamp((remMo / total) * 100);
  const add = clamp((addMo / total) * 100);
  return '<div class="storagebar ' + (opts.classe || '') + '" role="img" aria-label="Espace de stockage">'
    + '<span class="sb-stay" style="left:0;width:' + stay + '%"></span>'
    + (rem ? '<span class="sb-rem" style="left:' + stay + '%;width:' + rem + '%"></span>' : '')
    + (add ? '<span class="sb-add" style="left:' + (stay + rem) + '%;width:' + add + '%"></span>' : '')
    + '</div>';
}

// msgErreur : texte a afficher pour une erreur renvoyee par le Go. Une erreur
// destinee a l'utilisateur arrive sous la forme "ihm:{cle, params}" et est
// traduite (parametres *Octets formates en taille lisible) ; toute autre erreur
// (systeme, reseau) est affichee telle quelle.
export function msgErreur(e) {
  const s = typeof e === 'string' ? e : String((e && e.message) || e);
  const i = s.indexOf('ihm:{');
  if (i < 0) return s;
  try {
    const o = JSON.parse(s.slice(i + 4));
    const p = Object.assign({}, o.params || {});
    for (const k of Object.keys(p)) if (k.endsWith('Octets')) p[k] = fmtOctets(p[k]);
    return t(o.cle, p);
  } catch (_) { return s; }
}
