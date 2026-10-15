// Editeur d'illustration REUTILISABLE (categorie d'arbre, histoire perso...).
// Extrait de arbreboite.js pour etre partage sans divergence de rendu (CSS
// dediee, comme le composant de decouverte).
//
// Modale a deux onglets :
//   - "Generer" : une couleur + une icone -> illustration composee (genererIllustration).
//   - "Importer": une image JPEG/PNG locale qui remplace l'illustration generee.
//
// API : ouvrirEditeurIllustration({ nom, valeur, hote }) -> Promise
//   - nom   : texte de l'apercu genere (initiale + nom).
//   - valeur: { icone?, couleur? } etat initial de l'onglet Generer.
//   - hote  : element portant les variables de theme (defaut : document.body).
//     Doit etre (ou etre sous) le noeud qui definit les var(--...) du theme.
// Resout avec :
//   - { gen: { icone, couleur }, image } si validation d'une illustration generee ;
//   - { gen: null, image }               si import d'une image ;
//   - null                               si annulation / fermeture.
// `image` est toujours une data URL prete a l'affichage.

import './editeur-illustration.css';
import { genererIllustration, COULEURS, COULEUR_DEFAUT, ICONE_DEFAUT } from './illustration-cat.js';
import { ICONES, ICONE_LABELS, ICONE_GROUPES } from './icones.js';
import { t, plur } from './i18n.js';
import { esc, escAttr } from './composants.js';

export function ouvrirEditeurIllustration(opts = {}) {
  const nom = opts.nom || '';
  const hote = opts.hote || document.body;
  const val = opts.valeur || {};
  let icone = val.icone || ICONE_DEFAUT;
  let couleur = val.couleur || COULEUR_DEFAUT;
  let apercuJeton = 0;

  return new Promise((resolve) => {
    let clos = false;

    const fileInput = document.createElement('input');
    fileInput.type = 'file'; fileInput.accept = 'image/*'; fileInput.style.display = 'none';

    const ov = document.createElement('div');
    ov.className = 'illed-overlay';
    const colors = COULEURS.map((c) => '<button class="illed-sw" data-color="' + c + '" style="background:' + c + '" title="' + c + '" aria-label="' + c + '"></button>').join('');
    ov.innerHTML =
      '<div class="illed" role="dialog" aria-modal="true">'
      + '<div class="illed-head"><div class="illed-tabs">'
        + '<button data-tab="gen" class="on">' + t('illed_tab_gen') + '</button><button data-tab="imp">' + t('illed_tab_imp') + '</button></div>'
        + '<button class="illed-x" data-illed-close aria-label="' + escAttr(t('illed_fermer')) + '">✕</button></div>'
      + '<div class="illed-body">'
        + '<div data-pane="gen" class="illed-pane">'
          + '<div class="illed-top"><div class="illed-prev"><img id="illedPrev" alt=""></div>'
          + '<div class="illed-swatches">' + colors
            + '<label class="illed-sw illed-custom" title="' + escAttr(t('illed_couleur_perso')) + '"><input type="color" id="illedColor"><span>+</span></label>'
          + '</div></div>'
          + '<div class="illed-search"><input type="text" id="illedSearch" placeholder="' + escAttr(t('illed_search_ph')) + '" autocomplete="off" spellcheck="false"></div>'
          + '<div class="illed-icons" id="illedIcons"></div>'
        + '</div>'
        + '<div data-pane="imp" class="illed-pane" hidden>'
          + '<button class="illed-import" data-illed-import>' + t('illed_choisir') + '</button>'
          + '<p class="illed-hint">' + esc(t('illed_hint')) + '</p>'
        + '</div>'
      + '</div>'
      + '<div class="illed-foot">'
        + '<button class="illed-btn ghost" data-illed-close>' + t('ihm_arbre_annuler') + '</button>'
        + '<button class="illed-btn primary" data-illed-ok>' + t('ihm_arbre_valider') + '</button>'
      + '</div></div>';

    hote.appendChild(ov);
    hote.appendChild(fileInput);
    document.addEventListener('keydown', onKey);

    function nettoyer() { document.removeEventListener('keydown', onKey); ov.remove(); fileInput.remove(); }
    function finir(res) { if (clos) return; clos = true; nettoyer(); resolve(res); }
    function onKey(e) { if (e.key === 'Escape') finir(null); }

    function iconeApercu(n) {
      return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + (ICONES[n] || '') + '</svg>';
    }
    function boutonIcone(n) {
      return '<button class="illed-ico" data-icone="' + n + '" title="' + escAttr(ICONE_LABELS[n] || n) + '">' + iconeApercu(n) + '</button>';
    }
    function rendreIcones(q) {
      const cont = ov.querySelector('#illedIcons');
      q = (q || '').trim().toLowerCase();
      if (!q) {
        let html = '';
        for (const grp of Object.keys(ICONE_GROUPES)) {
          html += '<div class="illed-grp">' + esc(grp) + '</div><div class="illed-iconrow">'
            + ICONE_GROUPES[grp].map(boutonIcone).join('') + '</div>';
        }
        cont.innerHTML = html;
      } else {
        const noms = Object.keys(ICONES).filter((n) => n.includes(q) || (ICONE_LABELS[n] && ICONE_LABELS[n].toLowerCase().includes(q)));
        const cap = 150; const liste = noms.slice(0, cap);
        const nb = liste.length + (noms.length > cap ? ' / ' + noms.length : '');
        const info = noms.length ? plur(noms.length, 'illed_res_un', 'illed_res_n', { nb }) : t('illed_aucune');
        cont.innerHTML = '<div class="illed-grp">' + info + '</div><div class="illed-iconrow">' + liste.map(boutonIcone).join('') + '</div>';
      }
      majSelection();
    }
    function majSelection() {
      ov.querySelectorAll('[data-color]').forEach((b) => b.classList.toggle('on', b.getAttribute('data-color') === couleur));
      const custom = ov.querySelector('.illed-custom');
      const cin = ov.querySelector('#illedColor');
      const isPalette = COULEURS.indexOf(couleur) >= 0;
      if (custom) custom.classList.toggle('on', !isPalette);
      if (cin && /^#[0-9a-fA-F]{6}$/.test(couleur)) cin.value = couleur;
      ov.querySelectorAll('[data-icone]').forEach((b) => b.classList.toggle('on', b.getAttribute('data-icone') === icone));
    }
    async function rafraichirApercu() {
      const jeton = ++apercuJeton;
      const url = await genererIllustration({ nom, icone, couleur });
      if (jeton !== apercuJeton || clos) return; // une selection plus recente (ou fermeture) a pris la main
      const img = ov.querySelector('#illedPrev'); if (img) img.src = url;
    }

    // Onglets
    ov.querySelectorAll('[data-tab]').forEach((b) => b.addEventListener('click', () => {
      const tab = b.getAttribute('data-tab');
      ov.querySelectorAll('[data-tab]').forEach((x) => x.classList.toggle('on', x === b));
      ov.querySelectorAll('[data-pane]').forEach((pn) => { pn.hidden = pn.getAttribute('data-pane') !== tab; });
    }));
    // Palette + couleur personnalisee
    ov.querySelectorAll('[data-color]').forEach((b) => b.addEventListener('click', () => { couleur = b.getAttribute('data-color'); majSelection(); rafraichirApercu(); }));
    const cin = ov.querySelector('#illedColor');
    cin.addEventListener('input', () => { couleur = cin.value; majSelection(); rafraichirApercu(); });
    // Grille + recherche d'icones
    ov.querySelector('#illedIcons').addEventListener('click', (e) => {
      const b = e.target.closest('[data-icone]'); if (!b) return;
      icone = b.getAttribute('data-icone'); majSelection(); rafraichirApercu();
    });
    ov.querySelector('#illedSearch').addEventListener('input', (e) => rendreIcones(e.target.value));
    // Import d'image : ouvre le selecteur ; le choix applique et resout immediatement
    ov.querySelector('[data-illed-import]').addEventListener('click', () => { fileInput.value = ''; fileInput.click(); });
    fileInput.addEventListener('change', () => {
      const f = fileInput.files && fileInput.files[0];
      if (!f || !f.type || !f.type.startsWith('image/')) return;
      const r = new FileReader();
      r.onload = () => finir({ gen: null, image: r.result });
      r.readAsDataURL(f);
    });
    // Valider (onglet Generer) : compose et resout
    ov.querySelector('[data-illed-ok]').addEventListener('click', async () => {
      const image = await genererIllustration({ nom, icone, couleur });
      finir({ gen: { icone, couleur }, image });
    });
    // Annuler / fermer / clic hors modale
    ov.querySelectorAll('[data-illed-close]').forEach((b) => b.addEventListener('click', () => finir(null)));
    ov.addEventListener('click', (e) => { if (e.target === ov) finir(null); });

    rendreIcones('');
    majSelection();
    rafraichirApercu();
  });
}
