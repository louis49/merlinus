// Vue "Contenu par categorie" de la boite : arbre reel sur 3 niveaux
// (Categorie -> Serie -> Histoire), replicable/depliable, avec marquage
// "a retirer" (cascade des conteneurs vers leurs enfants) et compteurs.
//
// Phase 1 : lecture + retraits. Les operations renommer/creer/deplacer/illustrer
// viendront ensuite. Aucune publication : on edite un plan cote front.

import { t, plur } from './i18n.js';
import { genererIllustration, ICONE_DEFAUT, COULEUR_DEFAUT } from './illustration-cat.js';
import { ouvrirEditeurIllustration } from './editeur-illustration.js';
import { esc, escAttr, icone, PAL, msgErreur } from './composants.js';
import { fmtOctets } from './format.js';
import { RadiosDisponibles } from '../wailsjs/go/app/App';

// Limite de titre constatee en V3 : troncature a 63 octets (UTF-8, donc un
// caractere accentue compte double). On tronque sur les frontieres de code point.
const MAX_OCTETS_TITRE = 63;
function tronquerOctets(s, max) {
  s = String(s || '');
  const enc = new TextEncoder();
  if (enc.encode(s).length <= max) return s;
  let out = '';
  for (const ch of s) { if (enc.encode(out + ch).length > max) break; out += ch; }
  return out;
}
// UUID v4 canonique : la publication refuse tout autre format (un UUID non
// canonique serait encode comme la racine). randomUUID n'existe qu'en contexte
// sur ; getRandomValues, disponible partout, sert de repli.
function uuidV4() {
  try { if (crypto && crypto.randomUUID) return crypto.randomUUID(); } catch (_) { /* contexte non sûr */ }
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  return h.slice(0, 8) + '-' + h.slice(8, 12) + '-' + h.slice(12, 16) + '-' + h.slice(16, 20) + '-' + h.slice(20);
}


// Palette de dégradés pour les illustrations auto-générées (catégories/séries).
function hash(s) { let h = 0; for (const c of String(s || '?')) h = (h * 31 + c.charCodeAt(0)) >>> 0; return h; }
function gradTitre(s) { const p = PAL[hash(s) % PAL.length]; return 'linear-gradient(140deg,' + p[0] + ',' + p[1] + ')'; }
function initiale(s) { const m = String(s || '?').trim(); return (m ? m[0] : '?').toUpperCase(); }

function estConteneur(n) { return Array.isArray(n.child); }
function titre(n) { return n._titre != null ? n._titre : (n.title || ''); }
function compterAudios(n) {
  if (!estConteneur(n)) return 1;
  let c = 0; for (const x of n.child) c += compterAudios(x); return c;
}

// compterEtat compte les feuilles (histoires) sous n qui portent le drapeau `cle`
// (_new / _rem), en tenant compte de la cascade (un conteneur marque propage a
// toutes ses feuilles).
function compterEtat(n, cle, anc) {
  const marque = anc || !!n[cle];
  if (!estConteneur(n)) return marque ? 1 : 0;
  let c = 0; for (const x of n.child) c += compterEtat(x, cle, marque); return c;
}

// tagsConteneur : pastilles agregees d'une categorie / serie -> nombre d'histoires
// nouvelles ("X nouvelle(s)", vert) et a supprimer ("Y a supprimer", rouge).
function tagsConteneur(node) {
  const nbNew = compterEtat(node, '_new', false);
  const nbRem = compterEtat(node, '_rem', false);
  let h = '';
  if (nbNew > 0) h += '<span class="bt-badge neuf">' + plur(nbNew, 'ihm_arbre_neuf_un', 'ihm_arbre_neuf_n') + '</span>';
  else if (node._new) h += '<span class="bt-badge neuf">' + t('ihm_arbre_neuf') + '</span>'; // conteneur neuf encore vide
  if (nbRem > 0) h += '<span class="bt-badge rem">' + t('ihm_arbre_rem_n', { n: nbRem }) + '</span>';
  return h;
}

// fmtTaille formate un nombre d'octets : "Mo" au-dela de 1 Mo (1 decimale sous
// 10 Mo), "Ko" en dessous. Renvoie "" pour 0 (taille inconnue).
// creerVueArbre monte la vue dans host et gere l'interactivite.
// opts : { host, onChange? }
export function creerVueArbre(opts) {
  const host = opts.host;
  let arbre = [];
  let tailles = {};                  // uuid -> octets (inventaire boite + ajouts en bibliotheque)
  let macBoite = '';                 // MAC de la boite active (URL des illustrations en cache)
  let creating = false;              // formulaire "nouvelle catégorie" ouvert ?
  const imagesEchec = new Set();     // uuids sans illustration en cache (évite de re-tenter)
  let favSet = new Set();            // uuids favoris (fav.bin, lecture seule) -> etoile
  // gen : incremente a chaque charger()/vide(). Une operation asynchrone (image,
  // radios) note gen au depart et abandonne si l'arbre a ete recharge entre-temps
  // (ajout catalogue, changement de boite) : elle modifierait l'ANCIEN arbre et
  // son onChange ecraserait le travail avec une copie perimee.
  let gen = 0;
  function etoileFav(u) { return (u && favSet.has(u)) ? '<span class="bt-fav" title="' + escAttr(t('ihm_fav_titre')) + '">♥</span>' : ''; }
  function appliquerImage(path, fichier) {
    if (!fichier || !fichier.type || !fichier.type.startsWith('image/')) return;
    const node = resoudre(path); if (!node) return;
    const g = gen;
    const r = new FileReader();
    r.onload = () => {
      if (g !== gen) return; // arbre recharge pendant la lecture
      node._img = r.result;
      delete node._gen; // une image importee remplace l'illustration generee
      if (opts.onChange) opts.onChange(); render();
    };
    r.readAsDataURL(fichier);
  }

  // Editeur d'illustration : composant reutilisable (editeur-illustration.js).
  async function ouvrirEditeur(path) {
    const node = resoudre(path); if (!node) return;
    const g = gen;
    const res = await ouvrirEditeurIllustration({
      nom: titre(node),
      valeur: node._gen ? { icone: node._gen.icone, couleur: node._gen.couleur } : {},
      hote: host.closest('.ihm-root') || document.body,
    });
    if (!res || g !== gen) return; // annulation, ou arbre recharge entre-temps
    if (res.gen) { node._gen = res.gen; node._img = res.image; }
    else { delete node._gen; node._img = res.image; } // image importee : plus de gen
    if (opts.onChange) opts.onChange();
    render();
  }
  // illustrerDefaut pose une illustration generee par defaut (picto + couleur
  // standard) sur un noeud qui n'en a pas, pour qu'un conteneur _new ait toujours
  // un _img (sinon la publication le refuse : illustration manquante). `_gen` est conserve
  // pour que le renommage regenere l'image.
  async function illustrerDefaut(node) {
    if (!node || node._img) return;
    node._gen = { icone: ICONE_DEFAUT, couleur: COULEUR_DEFAUT };
    try {
      node._img = await genererIllustration({ nom: titre(node), icone: ICONE_DEFAUT, couleur: COULEUR_DEFAUT });
    } catch (_) { /* illustration par defaut best-effort */ }
  }
  async function regenererImage(node) {
    if (!node || !node._gen) return;
    const g = gen;
    const img = await genererIllustration({ nom: titre(node), icone: node._gen.icone, couleur: node._gen.couleur });
    if (g !== gen) return; // arbre recharge pendant la generation
    node._img = img;
    if (opts.onChange) opts.onChange(); // persiste la nouvelle image (apres regen async)
    render();
  }

  // illustration : dégradé + initiale en fond, recouverts par l'image si dispo
  // (upload local _img, sinon image de la boîte en cache via boiteimg/<uuid>).
  // Pour les catégories (editable), l'illustration est cliquable et accepte le
  // dépôt d'un fichier image.
  function illustration(node, type, path, editable) {
    const uuid = node.uuid || '';
    const src = node._img || (uuid && macBoite && !imagesEchec.has(uuid) ? 'boiteimg/' + encodeURIComponent(macBoite) + '/' + encodeURIComponent(uuid) : '');
    // eager + decoding sync : l'arbre se re-rend en entier a chaque toggle ; en
    // lazy, l'image recreee se recharge avec un micro-delai pendant lequel le fond
    // (degrade + initiale) reapparait (scintillement). Les categories sont peu
    // nombreuses, le chargement immediat depuis le cache evite ce flash.
    const img = src ? '<img class="bt-illimg" loading="eager" decoding="sync" data-uuid="' + escAttr(uuid) + '" src="' + escAttr(src) + '" alt="">' : '';
    const attrs = editable ? ' data-act="illustrer" data-path="' + path + '" title="' + t('ihm_arbre_illustrer') + '"' : '';
    return '<span class="bt-ill ' + type + (editable ? ' editable' : '') + '"' + attrs + ' style="background:' + gradTitre(titre(node)) + '">'
      + '<span class="bt-ill-init">' + esc(initiale(titre(node))) + '</span>' + img
      + (editable ? '<span class="bt-ill-hint">✎</span>' : '')
      + '</span>';
  }
  function boutonRem(path, rem) {
    return '<button class="bt-del' + (rem ? ' on' : '') + '" data-act="rem" data-path="' + path + '" title="'
      + (rem ? t('ihm_arbre_annuler') : t('ihm_arbre_supprimer')) + '">'
      + (rem ? t('ihm_arbre_annuler') : icone('trash-2')) + '</button>';
  }
  function boutonRenommer(path) {
    return '<button class="bt-ren" data-act="renommer" data-path="' + path + '" title="' + t('ihm_arbre_renommer') + '" aria-label="' + t('ihm_arbre_renommer') + '">✎</button>';
  }
  // Contrôle "nouvelle catégorie" : bouton, ou formulaire de saisie si ouvert.
  function controleCreer() {
    if (creating) {
      return '<div class="bt-creer-form"><input class="bt-input" id="btNouvelleCat" placeholder="' + t('ihm_arbre_creer_ph') + '" maxlength="63" autocomplete="off">'
        + '<button class="bt-ok" data-act="creer-ok" aria-label="' + t('ihm_arbre_valider') + '">✓</button>'
        + '<button class="bt-ok ghost" data-act="creer-annuler" aria-label="' + t('ihm_arbre_annuler') + '">✕</button></div>';
    }
    return '<button class="bt-creer" data-act="creer-ouvrir">＋ ' + t('ihm_arbre_creer') + '</button>';
  }
  // aDesRadios : l'arbre porte-t-il une branche webradio ACTIVE ? On ignore les
  // noeuds marques "a retirer" pour que le bouton reapparaisse quand l'utilisateur
  // a supprime les radios (il peut alors les re-ajouter).
  function aDesRadios() {
    let trouve = false;
    const walk = (ns) => {
      for (const n of ns) {
        if (n._rem) continue;
        if (n.webradio) { trouve = true; return; }
        if (estConteneur(n)) walk(n.child);
        if (trouve) return;
      }
    };
    walk(arbre);
    return trouve;
  }
  // ajouterRadios : recupere le catalogue (en ligne) et insere une branche
  // "Radio" (conteneur webradio + un dossier vide par station, UUID exact + logo).
  // Si une branche existe deja, elle est REMPLACEE par le catalogue complet
  // ("Restaurer les radios"), en gardant l'UUID et l'icone du conteneur.
  // Appelee depuis le panneau Radio de la vue Boite (btn = son bouton).
  async function ajouterRadios(btn) {
    if (!macBoite || btn.disabled) return;
    const prev = btn.textContent;
    const g = gen;
    btn.disabled = true;
    btn.textContent = '＋ ' + t('ihm_arbre_radios_chargement');
    try {
      const res = await RadiosDisponibles(macBoite);
      if (g !== gen) { btn.disabled = false; btn.textContent = prev; return; } // arbre recharge (autre boite, ajout catalogue)
      const stations = (res && res.stations) || [];
      if (!stations.length) throw new Error(t('ihm_arbre_radios_vide'));
      // Logo de station en bibliotheque (comme une serie) : affiche via boiteimg,
      // pousse a la publication. Sans logo, illustration generee (_img) plus bas.
      const avecLogo = new Set(stations.filter((r) => r.logo).map((r) => r.uuid));
      const enfants = stations.map((r) => ({ uuid: r.uuid, title: r.titre, child: [], _new: true }));
      // Conserver l'identite du conteneur "Radio" d'origine pour garder son icone :
      // UUID + logo (en bibliotheque) renvoyes par le backend (arbre reel
      // persiste), sinon un conteneur deja present dans l'arbre, sinon un nouvel UUID.
      const ancien = arbre.find((n) => n.webradio);
      const conteneur = {
        uuid: (res && res.conteneurUuid) || (ancien && ancien.uuid) || uuidV4(),
        title: t('ihm_arbre_radios_conteneur'),
        webradio: 1, _open: true, _new: true, child: enfants,
      };
      if (res && res.conteneurLogo && conteneur.uuid === res.conteneurUuid) avecLogo.add(conteneur.uuid);
      else if (ancien && ancien._img) conteneur._img = ancien._img;
      // On repart du catalogue courant : retirer toute branche webradio existante
      // (y compris l'ancienne) pour eviter les doublons.
      arbre = arbre.filter((n) => !n.webradio);
      arbre.push(conteneur);
      render();
      // Illustration par defaut pour les noeuds radio sans logo (logo station non
      // telecharge, ou conteneur sans logo) : evite un noeud _new sans jpg refuse
      // a la publication (illustration manquante).
      await Promise.all([conteneur, ...enfants].filter((n) => !n._img && !avecLogo.has(n.uuid)).map(illustrerDefaut));
      btn.disabled = false;
      btn.textContent = prev; // libelle recalcule par l'hote (onChange)
      if (g !== gen) return; // arbre recharge pendant les illustrations : branche abandonnee
      render();
      if (opts.onChange) opts.onChange();
    } catch (e) {
      btn.disabled = false;
      btn.textContent = prev;
      if (opts.onErreur) opts.onErreur(msgErreur(e));
    }
  }

  // tailleNoeud : octets d'une feuille (depuis `tailles`), ou somme cumulee d'un
  // conteneur (serie / categorie).
  function tailleNoeud(n) {
    if (!estConteneur(n)) return tailles[n.uuid] || 0;
    let s = 0; for (const c of n.child) s += tailleNoeud(c); return s;
  }
  function tailleSpan(n) { const o = tailleNoeud(n); return o > 0 ? '<span class="bt-taille">' + fmtOctets(o) + '</span>' : ''; }

  function renderNode(node, path, level, ancRem, sousRadio) {
    const rem = ancRem || !!node._rem;
    // Station radio : enfant d'un conteneur webradio. Rendu comme une histoire
    // (pas de chevron, pas de compteur, pas de depliage), mais on garde le grip
    // pour reordonner et le badge "nouvelle" si non synchronisee.
    if (sousRadio) {
      const dndH = ' data-dnd="' + path + '"';
      const gripH = '<span class="bt-grip" data-grip title="Réordonner" aria-hidden="true">⠿</span>';
      return '<div class="bt-node bt-hist bt-radio-sta' + (rem ? ' rem' : '') + '"' + dndH + '>'
        + '<div class="bt-row">'
          + gripH
          + illustration(node, 'hist', path, false)
          + '<span class="bt-nom">' + esc(titre(node)) + '</span>'
          + (node._new ? '<span class="bt-badge neuf">' + t('ihm_arbre_neuf') + '</span>' : '')
          + (ancRem ? '<span class="bt-badge rem">' + t('ihm_arbre_a_retirer') + '</span>' : boutonRem(path, rem))
        + '</div></div>';
    }
    if (estConteneur(node)) {
      const type = level === 1 ? 'cat' : 'serie';
      const nb = compterAudios(node);
      const open = !!node._open;
      const sousR = !!node.webradio; // les enfants sont des stations radio
      const enfants = open
        ? (node.child.length
          ? node.child.map((c, i) => renderNode(c, path + '.' + i, level + 1, rem, sousR)).join('')
          : '<div class="bt-vide">' + t('ihm_arbre_cat_vide') + '</div>')
        : '';
      const milieu = (level === 1 && node._editing)
        ? '<input class="bt-input" data-input="' + path + '" value="' + escAttr(titre(node)) + '" maxlength="63" autocomplete="off">'
          + '<button class="bt-ok" data-act="renommer-ok" data-path="' + path + '">✓</button>'
          + '<button class="bt-ok ghost" data-act="renommer-annuler" data-path="' + path + '">✕</button>'
        : '<span class="bt-nom">' + esc(titre(node)) + '</span>'
          + tagsConteneur(node)
          + (node.webradio ? '' : '<span class="bt-cnt">' + plur(nb, 'ihm_arbre_hist_un', 'ihm_arbre_hist_n') + '</span>')
          + tailleSpan(node)
          + (level === 1 && !ancRem ? boutonRenommer(path) : '')
          + (ancRem ? '<span class="bt-badge rem">' + t('ihm_arbre_a_retirer') + '</span>' : boutonRem(path, rem));
      const dnd = (level === 1 || level === 2) ? ' data-dnd="' + path + '"' : '';
      const grip = (level === 1 || level === 2) ? '<span class="bt-grip" data-grip title="' + escAttr(t('ihm_arbre_grip')) + '" aria-hidden="true">⠿</span>' : '';
      return '<div class="bt-node bt-' + type + (rem ? ' rem' : '') + (open ? ' open' : '') + '"' + dnd + '>'
        + '<div class="bt-row bt-clic" data-act="toggle" data-path="' + path + '" role="button" tabindex="0" aria-expanded="' + (open ? 'true' : 'false') + '">'
          + grip
          + '<span class="bt-chev" aria-hidden="true">▸</span>'
          + illustration(node, type, path, level === 1)
          + milieu
        + '</div>'
        + (open ? '<div class="bt-children">' + enfants + '</div>' : '')
      + '</div>';
    }
    // Audio = histoire (feuille). Une histoire DIRECTEMENT sous une categorie
    // (niveau 2, sans serie) est deplacable entre categories par drag&drop.
    const l2 = (level === 2);
    const dndH = l2 ? ' data-dnd="' + path + '"' : '';
    const gripH = l2 ? '<span class="bt-grip" data-grip title="Déplacer vers une catégorie" aria-hidden="true">⠿</span>' : '';
    return '<div class="bt-node bt-hist' + (rem ? ' rem' : '') + '"' + dndH + '>'
      + '<div class="bt-row">'
        + gripH
        + illustration(node, 'hist', path, false)
        + '<span class="bt-nom">' + esc(titre(node)) + '</span>'
        + etoileFav(node.uuid)
        + (node._new ? '<span class="bt-badge neuf">' + t('ihm_arbre_neuf') + '</span>' : '')
        + tailleSpan(node)
        + (ancRem ? '<span class="bt-badge rem">' + t('ihm_arbre_a_retirer') + '</span>' : boutonRem(path, rem))
      + '</div></div>';
  }

  function render() {
    const cats = arbre.length
      ? arbre.map((c, i) => renderNode(c, String(i), 1, false)).join('')
      : '<div class="bt-empty">' + t('ihm_arbre_vide') + '</div>';
    // "Nouvelle catégorie", puis "Ajouter les radios" seulement si absentes.
    host.innerHTML = cats + controleCreer();
    if (creating) { const inp = host.querySelector('#btNouvelleCat'); if (inp) inp.focus(); }
    // Image absente du cache -> on la mémorise et on retombe sur le dégradé.
    host.querySelectorAll('.bt-illimg').forEach((im) => im.addEventListener('error', () => {
      const u = im.getAttribute('data-uuid'); if (u) imagesEchec.add(u); im.remove();
    }));
  }

  function resoudre(path) {
    const parts = String(path).split('.');
    let n = arbre[+parts[0]];
    for (let i = 1; i < parts.length && n; i++) n = estConteneur(n) ? n.child[+parts[i]] : null;
    return n;
  }
  // Retire réellement un nœud de l'arbre de travail (utilisé pour les nœuds
  // _new : jamais synchronisés, leur suppression est immédiate).
  function supprimerParChemin(path) {
    const parts = String(path).split('.').map(Number);
    if (parts.length === 1) { arbre.splice(parts[0], 1); return; }
    let p = arbre[parts[0]];
    for (let i = 1; i < parts.length - 1 && p; i++) p = estConteneur(p) ? p.child[parts[i]] : null;
    if (!p || !estConteneur(p)) return;
    p.child.splice(parts[parts.length - 1], 1);
    // Serie neuve videe de son dernier episode : retiree aussi (meme regle que
    // le retrait depuis le catalogue). Une categorie neuve vide reste : on la
    // cree vide pour la remplir ensuite.
    if (parts.length === 3 && p._new && p.child.length === 0) arbre[parts[0]].child.splice(parts[1], 1);
  }

  // Limite de saisie a 63 octets (creation + renommage de categorie).
  host.addEventListener('input', (e) => {
    const inp = e.target.closest('.bt-input'); if (!inp) return;
    const coupe = tronquerOctets(inp.value, MAX_OCTETS_TITRE);
    if (coupe !== inp.value) inp.value = coupe;
  });

  host.addEventListener('click', (e) => {
    // Saisie (renommage) et poignée de glisser : ne déclenchent pas le repli.
    if (e.target.closest('input, .bt-grip')) return;
    const b = e.target.closest('[data-act]'); if (!b) return;
    const act = b.getAttribute('data-act');
    // Actions de création (sans nœud cible).
    if (act === 'creer-ouvrir') { creating = true; render(); return; }
    if (act === 'creer-annuler') { creating = false; render(); return; }
    if (act === 'creer-ok') {
      const inp = host.querySelector('#btNouvelleCat');
      const v = tronquerOctets((inp ? inp.value : '').trim(), MAX_OCTETS_TITRE);
      creating = false;
      if (v) {
        const n = { uuid: uuidV4(), title: v, child: [], _new: true, _open: true };
        arbre.push(n);
        render();
        // Illustration par defaut (asynchrone) : un conteneur _new sans _img
        // serait refuse a la publication.
        const g = gen;
        illustrerDefaut(n).then(() => { if (g !== gen) return; render(); if (opts.onChange) opts.onChange(); });
      } else {
        render();
      }
      if (opts.onChange) opts.onChange();
      return;
    }
    if (act === 'illustrer') { ouvrirEditeur(b.getAttribute('data-path')); return; }
    const node = resoudre(b.getAttribute('data-path')); if (!node) return;
    if (act === 'toggle') node._open = !node._open;
    else if (act === 'rem') {
      // Nœud créé localement (non synchronisé) : suppression réelle immédiate.
      // Nœud de la boîte : on marque "à retirer" (effectif à la synchro).
      if (node._new) supprimerParChemin(b.getAttribute('data-path'));
      else node._rem = !node._rem;
      if (opts.onChange) opts.onChange();
    }
    else if (act === 'renommer') {
      node._editing = true; render();
      const inp = host.querySelector('[data-input="' + b.getAttribute('data-path') + '"]');
      if (inp) { inp.focus(); inp.select(); }
      return;
    } else if (act === 'renommer-ok') {
      const inp = host.querySelector('[data-input="' + b.getAttribute('data-path') + '"]');
      if (inp) { const v = tronquerOctets(inp.value.trim(), MAX_OCTETS_TITRE); if (v) node._titre = v; }
      node._editing = false;
      if (node._gen) regenererImage(node); // le nom cuit dans l'illustration suit le renommage
      if (opts.onChange) opts.onChange();
    } else if (act === 'renommer-annuler') { node._editing = false; }
    render();
  });

  // Entrée / Espace sur une ligne de conteneur (hors saisie) = déplier/replier.
  host.addEventListener('keydown', (e) => {
    if (!e.target.closest('input')) {
      const row = e.target.closest('.bt-row.bt-clic');
      if (row && (e.key === 'Enter' || e.key === ' ')) {
        e.preventDefault();
        const n = resoudre(row.getAttribute('data-path'));
        if (n) { n._open = !n._open; render(); }
      }
      return;
    }
  });

  // Entrée = valider / Échap = annuler dans les champs (renommage, création).
  host.addEventListener('keydown', (e) => {
    const inp = e.target.closest('input'); if (!inp) return;
    if (e.key === 'Enter') { e.preventDefault(); const ok = inp.parentElement.querySelector('[data-act$="-ok"]'); if (ok) ok.click(); }
    else if (e.key === 'Escape') { const c = inp.parentElement.querySelector('[data-act$="-annuler"]'); if (c) c.click(); }
  });

  // ---- Drag & drop : réordonner les catégories, et les séries (au sein d'une
  // catégorie ou d'une catégorie à l'autre). Poignée ⠿. ----
  let drag = null;       // etat du drag en cours (geometrie FIGEE au demarrage)
  let dropCible = null;  // cible calculee au dragover, appliquee au drop
  function nettoyerReperes() {
    host.querySelectorAll('.bt-gap-before,.bt-gap-after,.dropinto').forEach((n) => n.classList.remove('bt-gap-before', 'bt-gap-after', 'dropinto'));
  }
  host.addEventListener('mousedown', (e) => {
    const grip = e.target.closest('[data-grip]');
    if (grip) { const node = grip.closest('.bt-node'); if (node) node.setAttribute('draggable', 'true'); }
  });
  // On FIGE au dragstart la geometrie des cibles (centres verticaux). La position
  // d'insertion se decide ensuite d'apres ces centres figes, jamais d'apres
  // l'element sous le curseur : ouvrir l'espace ne deplace donc plus la decision
  // (sinon boucle de retroaction = l'IHM "tremble", cf. mesure Patchright).
  host.addEventListener('dragstart', (e) => {
    const node = e.target.closest('.bt-node[draggable="true"]');
    if (!node) { e.preventDefault(); return; }
    const chemin = node.getAttribute('data-dnd');
    const niveau2 = chemin.indexOf('.') >= 0; // "0.3" = enfant de categorie ; "0" = categorie
    const centre = (el) => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; };
    let cibles = [], parentBounds = null, autresCats = [];
    if (niveau2) {
      const cont = node.closest('.bt-children');
      if (cont) cibles = Array.from(cont.children).filter((c) => c.matches && c.matches('.bt-node[data-dnd]'))
        .map((el) => ({ path: el.getAttribute('data-dnd'), el, mid: centre(el) }));
      const cat = node.closest('.bt-cat');
      if (cat) { const r = cat.getBoundingClientRect(); parentBounds = [r.top, r.bottom]; }
      const parentPath = chemin.split('.')[0];
      // Garde nature : une station radio (parent webradio) ne peut etre
      // deplacee QUE dans son conteneur (reorder) ; un noeud ordinaire ne peut pas
      // etre depose dans le conteneur webradio. On filtre donc les cibles `into`.
      const parent = arbre[+parentPath];
      const sourceRadio = !!(parent && parent.webradio);
      autresCats = sourceRadio ? [] : Array.from(host.querySelectorAll('.bt-cat[data-dnd]'))
        .filter((c) => c.getAttribute('data-dnd') !== parentPath)
        .filter((c) => { const cn = arbre[+c.getAttribute('data-dnd')]; return !(cn && cn.webradio); })
        .map((el) => { const r = el.getBoundingClientRect(); return { path: el.getAttribute('data-dnd'), el, top: r.top, bottom: r.bottom }; });
    } else {
      cibles = Array.from(host.querySelectorAll('.bt-cat[data-dnd]'))
        .map((el) => ({ path: el.getAttribute('data-dnd'), el, mid: centre(el) }));
    }
    drag = { path: chemin, niveau2, cibles, parentBounds, autresCats };
    dropCible = null;
    node.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    try { e.dataTransfer.setData('text/plain', chemin); } catch (_) { /* ignore */ }
  });
  // poserEspace : ouvre un espace a l'index d'insertion (centres figes).
  function poserEspace(cibles, y) {
    let k = 0; while (k < cibles.length && cibles[k].mid < y) k++;
    if (k < cibles.length) cibles[k].el.classList.add('bt-gap-before');
    else if (cibles.length) cibles[cibles.length - 1].el.classList.add('bt-gap-after');
    return k;
  }
  host.addEventListener('dragover', (e) => {
    if (drag) {
      nettoyerReperes();
      dropCible = null;
      const y = e.clientY;
      if (!drag.niveau2) {
        e.preventDefault();
        dropCible = { mode: 'cat', k: poserEspace(drag.cibles, y) };
      } else {
        const dansParent = drag.parentBounds && y >= drag.parentBounds[0] && y <= drag.parentBounds[1];
        if (dansParent && drag.cibles.length) {
          e.preventDefault();
          dropCible = { mode: 'reorder', k: poserEspace(drag.cibles, y) };
        } else {
          const cat = (drag.autresCats || []).find((c) => y >= c.top && y <= c.bottom);
          if (cat) { e.preventDefault(); cat.el.classList.add('dropinto'); dropCible = { mode: 'into', catPath: cat.path }; }
        }
      }
      return;
    }
    // Dépôt d'un fichier image sur une illustration de catégorie.
    if (e.dataTransfer && Array.from(e.dataTransfer.types || []).indexOf('Files') > -1) {
      const ill = e.target.closest('.bt-ill.editable');
      host.querySelectorAll('.bt-ill.dropimg').forEach((n) => { if (n !== ill) n.classList.remove('dropimg'); });
      if (ill) { e.preventDefault(); ill.classList.add('dropimg'); }
    }
  });
  host.addEventListener('drop', (e) => {
    if (drag) {
      e.preventDefault();
      const c = dropCible, cs = drag.cibles;
      if (c && c.mode === 'reorder') {
        if (c.k < cs.length) { if (cs[c.k].path !== drag.path) deplacerSerie(drag.path, cs[c.k].path, false); }
        else if (cs.length && cs[cs.length - 1].path !== drag.path) deplacerSerie(drag.path, cs[cs.length - 1].path, true);
      } else if (c && c.mode === 'into') {
        deposerSerieDansCategorie(drag.path, c.catPath);
      } else if (c && c.mode === 'cat') {
        if (c.k < cs.length) { if (+cs[c.k].path !== +drag.path) deplacerCategorie(+drag.path, +cs[c.k].path, false); }
        else if (cs.length && +cs[cs.length - 1].path !== +drag.path) deplacerCategorie(+drag.path, +cs[cs.length - 1].path, true);
      }
      drag = null; dropCible = null; render();
      return;
    }
    // Fichier image déposé sur une illustration de catégorie.
    const ill = e.target.closest('.bt-ill.editable[data-path]');
    if (ill && e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length) {
      e.preventDefault();
      const f = Array.from(e.dataTransfer.files).find((x) => x.type && x.type.startsWith('image/'));
      ill.classList.remove('dropimg');
      if (f) appliquerImage(ill.getAttribute('data-path'), f);
    }
  });
  host.addEventListener('dragend', () => {
    drag = null; dropCible = null;
    host.querySelectorAll('[draggable="true"]').forEach((n) => n.setAttribute('draggable', 'false'));
    host.querySelectorAll('.dragging').forEach((n) => n.classList.remove('dragging'));
    nettoyerReperes();
  });

  // ---- Déplacements dans l'arbre de travail ----
  function deplacerCategorie(from, to, after) {
    const item = arbre[from];
    const rest = arbre.filter((_, i) => i !== from);
    const t = to > from ? to - 1 : to;
    rest.splice(t + (after ? 1 : 0), 0, item);
    arbre = rest;
    if (opts.onChange) opts.onChange();
  }
  function deplacerSerie(srcPath, destPath, after) {
    const node = resoudre(srcPath); if (!node) return;
    const s = srcPath.split('.').map(Number);   // [categorie, serie]
    const d = destPath.split('.').map(Number);
    arbre[s[0]].child.splice(s[1], 1);           // retire la source
    let di = d[1];
    if (s[0] === d[0] && s[1] < d[1]) di -= 1;    // même catégorie, source avant cible : décale
    arbre[d[0]].child.splice(di + (after ? 1 : 0), 0, node);
    arbre[d[0]]._open = true;
    if (opts.onChange) opts.onChange();
  }
  function deposerSerieDansCategorie(srcPath, catPath) {
    const node = resoudre(srcPath); if (!node) return;
    const s = srcPath.split('.').map(Number);
    const dc = Number(catPath);
    if (s[0] === dc) return;                      // déjà dans cette catégorie
    arbre[s[0]].child.splice(s[1], 1);
    arbre[dc].child.push(node);
    arbre[dc]._open = true;
    if (opts.onChange) opts.onChange();
  }

  return {
    charger(nodes, mac) { gen++; arbre = Array.isArray(nodes) ? nodes : []; macBoite = mac || ''; imagesEchec.clear(); render(); },
    // setTailles : injecte la carte uuid -> octets (inventaire boite + ajouts) et re-rend.
    setTailles(map) { tailles = map || {}; render(); },
    // Panneau Radio : etat et action (le bouton vit hors de l'arbre).
    aDesRadios() { return aDesRadios(); },
    ajouterRadios(btn) { return ajouterRadios(btn); },
    // majFavoris : injecte l'ensemble des uuids favoris (fav.bin) et re-rend les etoiles.
    majFavoris(set) { favSet = set instanceof Set ? set : new Set(set || []); render(); },
    // arbreActuel : arbre de travail courant (avec drapeaux d'edition), pour persistance.
    arbreActuel() { return arbre; },
    vide() { gen++; arbre = []; render(); },
    // compteurs : histoires sur la boite (a retirer comprises), a ajouter, a
    // retirer (cascade prise en compte).
    compteurs() {
      let on = 0, rem = 0, add = 0;
      const walk = (n, anc, ancNew) => {
        const r = anc || !!n._rem;
        const nw = ancNew || !!n._new;
        if (!estConteneur(n)) {
          // "Sur l'enceinte" = ce qui y est AUJOURD'HUI : une histoire marquee
          // a retirer y reste jusqu'a la synchro ; un ajout n'y est pas encore.
          if (!nw) on++;
          if (r) rem++; else if (nw) add++;
          return;
        }
        for (const c of n.child) walk(c, r, nw);
      };
      arbre.forEach((c) => walk(c, false, false));
      return { surBoite: on, aRetirer: rem, aAjouter: add };
    },
  };
}
