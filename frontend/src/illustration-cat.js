// Generateur d'illustration de categorie, dans le style du constructeur :
// fond plat colore + icone Lucide en trait blanc + nom de la categorie cuit
// dans l'image. Rendu Canvas -> JPEG (data URL), reutilisant le canal _img.
//
// Parametres cales sur les illustrations reelles de la boite (mesures sur les
// originaux 128px, rendu ici a 256 pour la nettete en interface) :
//  - icone centree, boite 124px a partir de y=52, trait 0.6 (unites viewBox 24)
//  - nom centre, police 600 26px, base a y=202
// Le nom se retrecit si trop large (categories a nom long).

import { ICONES } from './icones.js';

// Palette de fonds : les 6 couleurs reelles du constructeur + quelques teintes
// complementaires (chaudes -> froides -> sombres).
export const COULEURS = [
  '#FFAD0F', '#FB9143', '#FE374E', '#EC6EAD', '#8E7CF0', '#00B5E2',
  '#7AE2BF', '#5BB661', '#003B5B', '#2D3E50', '#6D4C41',
];
export const COULEUR_DEFAUT = '#00B5E2';
export const ICONE_DEFAUT = 'book-open';

function svgURL(nomIcone) {
  const inner = ICONES[nomIcone] || '';
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"'
    + ' stroke="#fff" stroke-width="0.6" stroke-linecap="round" stroke-linejoin="round">' + inner + '</svg>';
  return 'data:image/svg+xml;charset=utf8,' + encodeURIComponent(svg);
}

function chargerIcone(nomIcone) {
  return new Promise((res) => {
    const im = new Image();
    im.onload = () => res(im);
    im.onerror = () => res(null);
    im.src = svgURL(nomIcone);
  });
}

let policePrete = false;
async function attendrePolice() {
  if (policePrete) return;
  try {
    if (document.fonts) { await document.fonts.load('600 26px Quicksand'); await document.fonts.ready; }
  } catch (_) { /* police indisponible : repli sans-serif */ }
  policePrete = true;
}

// genererIllustration renvoie une data URL JPEG (256x256) prete a devenir _img.
export async function genererIllustration({ nom, icone, couleur }) {
  await attendrePolice();
  const S = 256;
  const cv = document.createElement('canvas');
  cv.width = S; cv.height = S;
  const c = cv.getContext('2d');

  c.fillStyle = couleur || COULEUR_DEFAUT;
  c.fillRect(0, 0, S, S);

  const im = await chargerIcone(icone || ICONE_DEFAUT);
  if (im) { const z = 124; c.drawImage(im, (S - z) / 2, 52, z, z); }

  c.fillStyle = '#fff';
  c.textAlign = 'center';
  c.textBaseline = 'alphabetic';
  const texte = String(nom || '');
  let taille = 26;
  const maxW = S - 28;
  c.font = '600 ' + taille + 'px Quicksand, sans-serif';
  while (taille > 12 && c.measureText(texte).width > maxW) {
    taille -= 1; c.font = '600 ' + taille + 'px Quicksand, sans-serif';
  }
  c.fillText(texte, S / 2, 202);

  return cv.toDataURL('image/jpeg', 0.9);
}
