// IHM principale de Merlinus : barre du haut (logo + onglets + sélecteur
// multi-boîte variante C) et quatre vues (Accueil, Catalogue, Mes histoires,
// Boîte).
//
// Les donnees viennent des bindings Go (registre des boites, arbre de travail,
// catalogue iTunes / Radio France, histoires perso) ; le front ne persiste rien
// de metier (localStorage : la langue seulement, cf. i18n.js). Tout le texte
// passe par l'i18n (fr/en/debug).

import './ihm.css';
import iconeUrl from './assets/merlinus.svg';
import { t, plur, lang } from './i18n.js';
import { barreStockage, esc, escAttr, imgCat, icone, PAL, msgErreur } from './composants.js';
import { fmtOctets, fmtDuree } from './format.js';
import { creerVueArbre } from './arbreboite.js';
import { creerDecouverte } from './decouverte.js';

import { EtatBoite, CatalogueInitial, RechercheCatalogue, EpisodesSerie, Internet, ArbreBoite, ArbreTravail, EnregistrerArbreTravail, Boites, NommerBoite, DemarrerVeille, PublierEditions, DefinirWifi, DefinirHoraires, LireHoraires, LireFavoris, LireEcoutes, DernieresEcoutes, EjecterBoite, EtatSauvegarde, RestaurerBoite, RetirerBoite, SupprimerBoite, TelechargerEpisode, TelechargerImageSerie, EtatsBoite, EditionsEnAttente, EstimationStockage, LireStockage, TaillesArbre, EnregistrerSuivi, NouveautesAccueil, ImporterHistoirePerso, ListerHistoiresPerso, SupprimerHistoirePerso, DefinirIllustrationPerso, RenommerHistoirePerso } from '../wailsjs/go/app/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { creerListeVirtuelle } from './vliste.js';
import { creerLecteur, fmtTemps } from './lecteur.js';
import { ouvrirEditeurIllustration } from './editeur-illustration.js';
import { genererIllustration, ICONE_DEFAUT, COULEUR_DEFAUT } from './illustration-cat.js';

// Taille de lot pour l'ajout d'une serie : nombre d'episodes telecharges en
// parallele a la fois (les insertions dans l'arbre restent serielles).
const LOT_TELECHARGEMENT = 10;

// hashGrad : hash stable d'une chaine -> index de dégradé (couleur par MAC).
function hashGrad(s) { let h = 0; s = String(s || ''); for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0; return h; }

// Capacite nominale (Mo) d'une boite Merlin, utilisee comme repli d'affichage de
// la jauge tant que le stockage reel n'a pas ete lu (avant la 1re connexion).
const CAPACITE_MO_DEFAUT = 3908;

// construireBoite : projette une boîte du registre Go (clé MAC) en modèle d'IHM.
// Le nom, le SSID et le rapport viennent du registre ; connected/batt sont
// volatils (recalculés par la sonde). Le contenu réel est l'arbre (vueArbre) ;
// home reste un jeu de démonstration pour la vue Accueil.
// jaugeStockage : Mo occupes / capacite, depuis les octets annonces par la boite
// (repli sur la capacite nominale tant que rien n'a ete lu).
function jaugeStockage(total, libre) {
  return { totalMo: total ? Math.round(total / 1048576) : CAPACITE_MO_DEFAUT, usedMo: total ? Math.round((total - libre) / 1048576) : 0 };
}
function construireBoite(m) {
  m = m || {};
  const { totalMo, usedMo } = jaugeStockage(m.stockage_total || 0, m.stockage_libre || 0);
  const cle = m.mac || m.ssid || '';
  return {
    id: cle, mac: cle, ssid: m.ssid || '-', wifiMaison: m.wifi_maison || '', name: m.nom || t('ihm_boite_nom_defaut'), fw: m.firmware || '-',
    batt: null, connected: false,
    onSpk: (m.rapport && m.rapport.histoires) || 0, add: 0, rem: 0,
    usedMo, totalMo, addMo: 0, remMo: 0, derniereSync: m.derniere_sync || 0, accueil: [],
    grad: GRADS[hashGrad(cle) % GRADS.length],
  };
}

function grad(a, b) { return 'linear-gradient(140deg,' + a + ',' + b + ')'; }
const GRADS = PAL.slice(0, 4).map(([a, b]) => grad(a, b)); // 4 premieres paires de la palette commune

function srcLabel(s) { return s === 'itunes' ? 'iTunes' : 'Radio France'; } // marques : non traduites

// Squelette HTML de l'IHM (chrome via i18n ; données de démo littérales).
function shell() {
  return `
  <div class="ihm-root">
    <div class="topbar">
      <div class="brand">
        <span class="glyph"><img src="${iconeUrl}" alt=""></span>
        <span>Merlinus<small>${t('ihm_brand_sub')}</small></span>
      </div>
      <div class="tabs" role="tablist" aria-label="Sections">
        <button class="tab" role="tab" aria-selected="true" data-view="accueil">${t('ihm_tab_accueil')}</button>
        <button class="tab" role="tab" aria-selected="false" data-view="catalogue">${t('ihm_tab_catalogue')}</button>
        <button class="tab" role="tab" aria-selected="false" data-view="histoires">${t('ihm_tab_histoires')}</button>
        <button class="tab" role="tab" aria-selected="false" data-view="boite">${t('ihm_tab_boite')}</button>
      </div>
      <div id="boxControl"></div>
    </div>
    <main>
      <section class="view active" id="accueil" role="tabpanel">
        <h1 class="page-title" id="accueilTitre">${t('ihm_accueil_titre', { name: '' })}</h1>
        <div class="summary-pill" id="homeSummary"></div>
        <div id="homeContent"></div>
        <section class="ecoutes" id="homeEcoutes" aria-labelledby="ecoutesTitre" hidden></section>
      </section>
      <section class="view" id="catalogue" role="tabpanel">
        <h1 class="page-title">${t('ihm_cat_titre')}</h1>
        <p class="lead">${t('ihm_cat_lead')}</p>
        <div class="catbar">
          <div class="seg" id="srcSeg" role="group" aria-label="Source">
            <button data-src="itunes" aria-pressed="true"><span class="src-dot" style="background:var(--c-hist)"></span>iTunes</button>
            <button data-src="radiofrance" aria-pressed="false"><span class="src-dot" style="background:var(--c-doc)"></span>Radio France</button>
          </div>
          <div class="search">
            <svg class="mag" width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 20 20"/></svg>
            <input id="searchInput" type="search" placeholder="${t('ihm_cat_search_ph')}" aria-label="${t('ihm_cat_search_ph')}">
            <button class="search-clear" id="searchClear" hidden aria-label="${t('ihm_cat_search_clear')}">${icone('x')}</button></div>
        </div>
        <div id="catList"><p class="count-note" id="catCount"></p><div class="grid catalog" id="catGrid"></div></div>
        <div id="catDetail" class="detail" hidden></div>
      </section>
      <section class="view" id="histoires" role="tabpanel">
        <h1 class="page-title">${t('ihm_perso_titre')}</h1>
        <p class="lead">${t('ihm_perso_lead')}</p>
        <div id="persoWrap"></div>
      </section>
      <section class="view" id="boite" role="tabpanel">
        <h1 class="page-title" id="boitePageTitre">${t('ihm_boite_titre', { name: '' })}</h1>
        <p class="lead">${t('ihm_boite_lead')}</p>
        <div class="box-grid">
          <div><div class="panel">
            <h2>${t('ihm_boite_panel1_titre')}</h2>
            <p class="sub">${t('ihm_boite_panel1_sub')}</p>
            <div class="gauge-wrap">
              <div class="gauge-top"><span class="big"><span id="gUsed">0</span><small> / <span id="gTotal">0</span> Mo</small></span>
                <span class="pct" id="gPct">0 %</span></div>
              <div id="gBar"></div>
              <div class="gauge-cap" id="gCap"></div>
            </div>
            <div class="counters">
              <div class="counter"><div class="v" id="cOn">0</div><div class="k">${t('ihm_boite_c_on')}</div></div>
              <div class="counter add"><div class="v" id="cAdd">0</div><div class="k">${t('ihm_boite_c_add')}</div></div>
              <div class="counter rem"><div class="v" id="cRem">0</div><div class="k">${t('ihm_boite_c_rem')}</div></div>
            </div>
            <button class="sync-btn" id="syncBtn">${t('ihm_boite_sync')}</button>
            <div class="sync-prog" id="syncProg" hidden>
              <div class="sync-prog-top"><span id="syncProgLabel"></span><span id="syncProgPct"></span></div>
              <div class="sync-prog-bar"><i id="syncProgFill"></i></div>
              <div class="sync-prog-info"><span id="syncProgDetail"></span><span id="syncProgEta"></span></div>
            </div>
            <div class="sync-msg" id="syncMsg" hidden></div>
            <div class="hint"><h4>${t('ihm_boite_hint_titre')}</h4>
              <ol><li>${t('ihm_boite_hint_1')}</li><li>${t('ihm_boite_hint_2')}</li>
                <li>${t('ihm_boite_hint_3')}</li><li>${t('ihm_boite_hint_4')}</li></ol></div>
            <div class="box-options">
              <button class="opt-btn" id="optHoraires" type="button">${t('ihm_boite_opt_horaires')}</button>
            </div>
          </div>
          <div class="panel panel-radio">
            <h2>${t('ihm_radio_titre')}</h2>
            <p class="sub" id="radioSub"></p>
            <div class="box-options">
              <button class="opt-btn" id="optWifi" type="button">${t('ihm_boite_opt_wifi')}</button>
              <button class="opt-btn" id="radiosBtn" type="button" hidden></button>
            </div>
          </div>
          <div class="panel panel-sensible">
            <h2>${t('ihm_boite_retirer_titre')}</h2>
            <p class="sub" id="retirerSub"></p>
            <button class="danger-btn" id="restaurerBtn" type="button" hidden>${t('ihm_boite_restaurer')}</button>
            <div class="sync-msg" id="restaurerMsg" hidden></div>
          </div></div>
          <div class="panel">
            <div class="cat-head"><h2>${t('ihm_boite_panel2_titre')}</h2></div>
            <p class="sub">${t('ihm_boite_panel2_sub')}</p>
            <div id="boxCats"></div>
          </div>
        </div>
      </section>
    </main>
    <div class="modal-back" id="modal" hidden>
      <div class="onb" role="dialog" aria-modal="true"><div class="inner" id="onbInner"></div></div>
    </div>
    <div class="modal-back" id="horModal" hidden>
      <div class="onb" role="dialog" aria-modal="true" aria-labelledby="horTitre">
        <div class="inner">
          <h3 id="horTitre">${t('ihm_hor_titre')}</h3>
          <p class="sub">${t('ihm_hor_sub')}</p>
          <div id="horBody"></div>
          <label class="hor-toggle"><input type="checkbox" id="horNePasCouper">
            <span><b>${t('ihm_hor_nepascouper')}</b><small>${t('ihm_hor_nepascouper_aide')}</small></span></label>
          <div class="sync-msg" id="horMsg" hidden></div>
          <div class="wifi-actions">
            <button class="opt-btn" id="horCancel" type="button">${t('ihm_hor_annuler')}</button>
            <button class="sync-btn" id="horSave" type="button">${t('ihm_hor_enregistrer')}</button>
          </div>
        </div>
      </div>
    </div>
    <div class="modal-back" id="retraitModal" hidden>
      <div class="onb" role="dialog" aria-modal="true" aria-labelledby="retraitTitre">
        <div class="inner">
          <h3 id="retraitTitre"></h3>
          <p class="sub" id="retraitTexte"></p>
          <label class="wifi-f" id="retraitConsigne">
            <input type="text" id="retraitSaisie" autocomplete="off" spellcheck="false" autocapitalize="characters"></label>
          <div class="sync-prog" id="retraitProg" hidden>
            <div class="sync-prog-top"><span id="retraitProgLabel"></span><span id="retraitProgPct"></span></div>
            <div class="sync-prog-bar"><i id="retraitProgFill"></i></div>
            <div class="sync-prog-info"><span id="retraitProgDetail"></span><span id="retraitProgEta"></span></div>
          </div>
          <div class="sync-msg" id="retraitMsg" hidden></div>
          <div class="wifi-actions" id="retraitActions">
            <button class="opt-btn" id="retraitAnnuler" type="button">${t('ihm_wifi_annuler')}</button>
            <button class="danger-btn" id="retraitValider" type="button" disabled></button>
          </div>
          <div class="wifi-actions" id="retraitFin" hidden>
            <button class="sync-btn" id="retraitFermer" type="button">${t('ihm_retrait_fermer')}</button>
          </div>
        </div>
      </div>
    </div>
    <div class="modal-back" id="wifiModal" hidden>
      <div class="onb" role="dialog" aria-modal="true" aria-labelledby="wifiTitre">
        <div class="inner">
          <h3 id="wifiTitre">${t('ihm_wifi_titre')}</h3>
          <p class="sub">${t('ihm_wifi_sub')}</p>
          <label class="wifi-f">${t('ihm_wifi_ssid')}
            <input type="text" id="wifiSsid" maxlength="32" autocomplete="off" spellcheck="false"></label>
          <label class="wifi-f">${t('ihm_wifi_pwd')}
            <input type="password" id="wifiPwd" maxlength="63" autocomplete="off"></label>
          <div class="sync-msg" id="wifiMsg" hidden></div>
          <div class="wifi-actions">
            <button class="opt-btn" id="wifiCancel" type="button">${t('ihm_wifi_annuler')}</button>
            <button class="sync-btn" id="wifiSave" type="button">${t('ihm_wifi_enregistrer')}</button>
          </div>
        </div>
      </div>
    </div>
  </div>`;
}

// Monte l'IHM dans l'élément app et câble toute l'interactivité (client-side).
export async function monterIHM(app) {
  let boites;
  try { boites = (await Boites()).map(construireBoite); } catch (_) { boites = []; }
  if (!boites.length) boites = [construireBoite({ nom: t('ihm_nom_exemple') })];
  let active = 0;

  app.innerHTML = shell();
  const q = (sel) => app.querySelector(sel);

  const boxCtl = q('#boxControl');
  function box() { return boites[active]; }
  function pct(b) { return b.totalMo ? Math.round((b.usedMo / b.totalMo) * 100) : 0; }
  function initial(b) { return (b.name || b.ssid.slice(-2)).charAt(0).toUpperCase(); }
  function avatar(b, cls) { return '<span class="avatar ' + (cls || '') + '" style="background:' + b.grad + '">' + initial(b) + '</span>'; }
  let editing = false; // édition du nom de la boîte active (dans la popup)

  // Différentiel de stockage : ajouts en taille RÉELLE (médias en bibliothèque) ;
  // retraits estimés (~6 Mo/épisode : les tailles réelles sont sur la boîte).
  function deltas(b) { return { addMo: b.addMo || 0, remMo: b.remMo || 0 }; }
  // Usage futur (après synchro) et dépassement de capacité.
  function futurMo(b) { return (b.usedMo || 0) + (b.addMo || 0) - (b.remMo || 0); }
  // Options de barre en variation NETTE (ajouts − retraits) : un seul bloc, vert
  // si l'usage augmente (croissance après l'occupé), rouge s'il diminue (part
  // liberee, en fin de remplissage courant).
  function barOptsNet(b) {
    const d = deltas(b);
    const net = d.addMo - d.remMo;
    return { usedMo: b.usedMo || 0, totalMo: b.totalMo || 1, addMo: net > 0 ? net : 0, remMo: net < 0 ? -net : 0 };
  }
  function depasse(b) { return b && b.totalMo > 0 && futurMo(b) > b.totalMo; }
  // Rafraîchit la taille réelle des ajouts (asynchrone) puis re-rend les blocs
  // stockage. Fire-and-forget : ne bloque pas l'IHM.
  function rafraichirEspaceAjouts() {
    const b = box(); if (!b || !b.mac) return;
    EstimationStockage(b.mac).then((e) => {
      b.addMo = (e && e.ajoutMo) || 0;
      b.remMo = (e && e.retraitMo) || 0;
      renderContext(); renderBoxControl(); majEtatBoutons();
    }).catch(() => {});
  }
  let espaceTimer = null;
  function planifierEspaceAjouts() { clearTimeout(espaceTimer); espaceTimer = setTimeout(() => { rafraichirEspaceAjouts(); rafraichirAccueil(); majPanneauRetrait(); }, 150); }

  // rafraichirAccueil : enregistre les series suivies (identite catalogue depuis
  // le travail), puis interroge le backend pour les nouveautes (cache 24 h).
  async function rafraichirAccueil() {
    const b = box(); if (!b || !b.mac) return;
    try {
      const a = await chargerTravailBoite(b.mac);
      for (const cat of (a || [])) {
        if (!Array.isArray(cat.child)) continue;
        for (const sn of cat.child) {
          if (Array.isArray(sn.child) && sn._src && sn._srcId) {
            // Sequentiel (await) : une seule ecriture de suivis-<mac>.json a la
            // fois (le backend serialise aussi via muSuivis, ceinture-bretelles).
            try { await EnregistrerSuivi(b.mac, sn.uuid, sn._src, sn._srcId, sn._srcImg || '', sn._titre || sn.title || ''); } catch (_) {}
          }
        }
      }
      rafraichirEcoutes(); // local (historique cumule) : n'attend pas le reseau
      const list = await NouveautesAccueil(b.mac);
      b.accueil = (list || []).map((s) => ({ uuid: s.uuid, title: s.title, src: s.src, srcId: s.srcId, img: s.img, nbNew: s.nbNouv }));
      renderContext();
    } catch (_) { /* accueil best-effort */ }
  }

  // ---- Dernieres ecoutes (accueil) ----
  // Historique cumule cote Go (releve a chaque connexion de la boite). Les
  // heures de la boite sont son heure MURALE encodee "comme UTC" : on les
  // formate donc en UTC, jamais dans le fuseau du Mac (sinon decalage de 2 h).
  const NB_ECOUTES = 10;
  const locale = () => (lang === 'en' ? 'en-GB' : 'fr-FR');
  const jourBoite = (s) => new Date(s * 1000).toISOString().slice(0, 10);
  const capitale = (s) => s.charAt(0).toUpperCase() + s.slice(1);
  function libelleJour(s) {
    try { return capitale(new Date(s * 1000).toLocaleDateString(locale(), { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' })); } catch (_) { return jourBoite(s); }
  }
  function heureBoite(s) {
    try { return new Date(s * 1000).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit', timeZone: 'UTC' }); } catch (_) { return ''; }
  }
  // Date REELLE du releve (horloge du Mac) : fuseau local.
  function dateReleve(s) {
    try { return new Date(s * 1000).toLocaleString(locale(), { weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' }); } catch (_) { return ''; }
  }
  async function rafraichirEcoutes() {
    const b = box(); const host = q('#homeEcoutes');
    if (!host) return;
    if (!b || !b.mac) { host.hidden = true; return; }
    let d = null;
    try { d = await DernieresEcoutes(b.mac, NB_ECOUTES); } catch (_) { /* best-effort */ }
    if (b !== box()) return; // la boite active a change pendant l'appel
    const ecoutes = (d && d.ecoutes) || [];
    host.hidden = false;
    let h = '<div class="ec-tete"><div><h2 id="ecoutesTitre">' + t('ihm_ecoutes_titre') + '</h2>'
      + '<p>' + (d && d.releve ? t('ihm_ecoutes_releve', { d: esc(dateReleve(d.releve)) }) : t('ihm_ecoutes_jamais')) + '</p></div>';
    if (ecoutes.length) {
      const min = ecoutes.reduce((s, e) => s + (e.minutes || 0), 0);
      h += '<span class="ec-resume">' + plur(ecoutes.length, 'ihm_ecoutes_resume_un', 'ihm_ecoutes_resume_n', { m: min }) + '</span>';
    }
    h += '</div>';
    if (!ecoutes.length) {
      host.innerHTML = h + (d && d.releve ? '<div class="ec-vide">' + t('ihm_ecoutes_vide') + '</div>' : '');
      return;
    }
    h += '<div class="ec-liste">';
    let jour = '';
    for (const e of ecoutes) {
      const j = jourBoite(e.debut);
      if (j !== jour) {
        if (jour) h += '</ol>';
        jour = j;
        h += '<div class="ec-jour">' + esc(libelleJour(e.debut)) + '</div><ol>';
      }
      const dureeMin = e.duree_s ? Math.max(1, Math.round(e.duree_s / 60)) : 0;
      // La boite compte les minutes ecoutees (bitmap), sans la derniere minute
      // entamee : une ecoute qui arrive a moins d'une minute de la fin est complete.
      const entier = e.duree_s > 0 && e.minutes * 60 >= e.duree_s - 60;
      const pct = !e.duree_s ? 0 : entier ? 100 : Math.min(100, Math.round(100 * e.minutes * 60 / e.duree_s));
      const ecoute = e.minutes ? plur(e.minutes, 'ihm_ecoutes_min_un', 'ihm_ecoutes_min_n') : t('ihm_ecoutes_moins1');
      h += '<li class="ec-ligne">'
        + '<img src="boiteimg/' + encodeURIComponent(b.mac) + '/' + encodeURIComponent(e.uuid) + '" alt="" loading="lazy" onerror="this.style.visibility=\'hidden\'">'
        + '<div class="ec-txt"><b>' + esc(e.titre || '-') + '</b><span>' + esc(e.serie || '') + '</span></div>'
        + '<div class="ec-prog"><div class="ec-chiffres"><span>' + ecoute + '</span>'
        + (dureeMin ? '<span>' + t('ihm_ecoutes_sur', { n: dureeMin }) + '</span>' : '') + '</div>'
        + (dureeMin ? '<div class="ec-barre" role="img" aria-label="' + escAttr(t('ihm_ecoutes_aria', { m: e.minutes, d: dureeMin })) + '"><i class="' + (entier ? 'fini' : '') + '" style="width:' + pct + '%"></i></div>' : '')
        + '</div>'
        + '<div class="ec-fin">' + (entier ? '<span class="ec-entier">' + t('ihm_ecoutes_entier') + '</span>' : '')
        + '<span class="ec-heure">' + esc(heureBoite(e.debut)) + '</span></div></li>';
    }
    host.innerHTML = h + '</ol></div>';
  }
  // Releve de l'historique a la connexion de la boite (connexion partagee avec
  // la sonde : quelques nouvelles tentatives si elle est occupee).
  async function chargerEcoutesBoite(b) {
    for (let essai = 0; essai < 4; essai++) {
      try { if (await LireEcoutes(b.mac)) { if (b === box()) rafraichirEcoutes(); return; } } catch (_) { /* reessaie */ }
      await new Promise((r) => setTimeout(r, 1500));
    }
  }

  // ouvrirDepuisAccueil : depuis une carte d'accueil, bascule sur le catalogue,
  // met a jour le picker source, et ouvre directement le detail de la serie.
  async function ouvrirDepuisAccueil(s) {
    if (!s) return;
    // Identite catalogue connue : bascule + ouverture directe du detail.
    if (s.src && s.srcId) {
      curSrc = s.src;
      q('#srcSeg').querySelectorAll('button').forEach((x) => x.setAttribute('aria-pressed', x.getAttribute('data-src') === curSrc));
      catInit = true; // evite un rechargement de la liste qui masquerait le detail
      show('catalogue');
      const p = { id: s.srcId, uuid: s.uuid, title: s.title, artist: '', src: s.src,
        image: s.img || ('boiteimg/' + encodeURIComponent(box().mac) + '/' + encodeURIComponent(s.uuid)) };
      await ouvrirSerie(p);
      return;
    }
    // Sans identite (serie ancienne) : repli sur une recherche par titre.
    gotoSeries({ src: s.src || curSrc, t: s.title });
  }
  // Métriques du bloc haut : nb d'histoires · à ajouter · à retirer.
  function metrics(b) {
    return '<span class="m-h">' + plur(b.onSpk || 0, 'ihm_metric_hist_un', 'ihm_metric_hist_n') + '</span>'
      + '<span class="sep">·</span><span class="add">' + b.add + '</span>'
      + '<span class="sep">·</span><span class="rem">' + b.rem + '</span>';
  }
  function statutLibelle(b) {
    if (b.connected) return t('ihm_ctrl_connected');
    if (b.occupee) return t('ihm_ctrl_busy');
    return t('ihm_ctrl_disconnected');
  }
  function statutDot(b) {
    const cls = b.connected ? 'on' : (b.occupee ? 'busy' : 'off');
    return '<span class="statusdot ' + cls + '" title="' + statutLibelle(b) + '"></span>';
  }
  // Badge de niveau de charge (affiché seulement si la boîte est connectée).
  function batterieBadge(niveau) {
    const cls = niveau <= 20 ? 'low' : 'ok';
    const w = Math.max(8, Math.min(100, niveau));
    return '<span class="ctl-charge"><span class="batt ' + cls + '"><i style="width:' + w + '%"></i></span>' + niveau + '%</span>';
  }

  // formatDepuis : date relative de la derniere synchro ("il y a 3 j", "jamais").
  function formatDepuis(ts) {
    if (!ts) return t('ihm_sync_jamais');
    const sec = Math.floor(Date.now() / 1000) - ts;
    if (sec < 60) return t('ihm_sync_instant');
    const j = Math.floor(sec / 86400); if (j >= 1) return t('ihm_sync_ilya_j', { n: j });
    const h = Math.floor(sec / 3600); if (h >= 1) return t('ihm_sync_ilya_h', { n: h });
    return t('ihm_sync_ilya_m', { n: Math.max(1, Math.floor(sec / 60)) });
  }

  function boxrows(includeActive) {
    return boites.map((b, i) => {
      if (!includeActive && i === active) return '';
      return '<button class="boxrow' + (i === active ? ' active' : '') + '" data-box="' + b.id + '">'
        + '<span class="ava-wrap">' + avatar(b, '') + statutDot(b) + '</span>'
        + '<span class="info"><b>' + esc(b.name) + '</b><small>' + t('ihm_ctrl_row_sub', { ssid: esc(b.ssid), n: b.onSpk })
          + '</small><small class="boxrow-sync">' + t('ihm_ctrl_row_sync', { d: formatDepuis(b.derniereSync) }) + '</small></span>'
        + (i === active ? '<span class="check">✓</span>' : '') + '</button>';
    }).join('');
  }

  function renderBoxControl(keepOpen) {
    const b = box();
    const barOpts = barOptsNet(b);
    const others = boxrows(false) || '<div class="none">' + t('ihm_ctrl_none') + '</div>';

    // Carte active : entête en mode lecture (nom + statut + crayon) ou édition (champ).
    const head = editing
      ? '<div class="ac-edit-row"><input id="acNameInput" value="' + escAttr(b.name) + '" maxlength="24" autocomplete="off" spellcheck="false">'
        + '<button class="ac-btn" data-save aria-label="' + t('ihm_ctrl_save') + '">✓</button>'
        + '<button class="ac-btn ghost" data-cancel aria-label="' + t('ihm_cancel') + '">✕</button></div>'
      : '<div class="top"><span class="ava-wrap">' + avatar(b, 'lg') + statutDot(b) + '</span>'
        + '<span class="info"><b>' + esc(b.name) + '</b><small class="ac-status ' + (b.connected ? 'on' : (b.occupee ? 'busy' : 'off')) + '">'
        + statutLibelle(b) + '</small></span>'
        + '<button class="ac-edit" data-edit title="' + t('ihm_ctrl_rename') + '" aria-label="' + t('ihm_ctrl_rename') + '">✎</button></div>'
        // Derniere synchro (deplacee ici depuis le resume de l'accueil).
        + '<div class="cap ac-sync">' + (b.derniereSync ? t('ihm_ctrl_sync', { d: formatDepuis(b.derniereSync) }) : t('ihm_ctrl_sync_jamais')) + '</div>';

    const charge = (b.connected && b.batt != null) ? batterieBadge(b.batt) : '';
    boxCtl.innerHTML = '<button class="ctl-c" data-pop="c" aria-haspopup="true">'
      + '<span class="ava-wrap">' + avatar(b, 'lg') + statutDot(b) + '</span>'
      + '<span class="txt"><b class="ctl-name">' + esc(b.name) + '</b>'
        + '<span class="ctl-metrics">' + metrics(b)
          + (depasse(b) ? '<span class="ctl-alerte" title="' + escAttr(t('ihm_boite_depasse', { n: futurMo(b) - b.totalMo })) + '">' + icone('triangle-alert') + '</span>' : '')
          + '</span>'
        + barreStockage(Object.assign({ classe: 'mini' }, barOpts)) + '</span>'
      + charge
      + '</button>'
      // Ejecter : bouton FRERE (pas dans le bouton du bloc : HTML invalide).
      // Visible seulement boite connectee ; desactive pendant une synchro.
      + (b.connected
        ? '<button class="ctl-eject" data-eject type="button" title="' + escAttr(t('ihm_ejecter')) + '" aria-label="' + escAttr(t('ihm_ejecter')) + '"'
          + (syncEnCours || b.occupee ? ' disabled' : '') + '>' + icone('eject') + '</button>'
        : '')
      + '<div class="pop" hidden data-popfor="c">'
        + '<div class="activecard">' + head + '</div>'
        + '<div class="phead">' + t('ihm_ctrl_autres') + '</div>' + others
        + '<button class="addbox" data-add>' + t('ihm_ctrl_addbox') + '</button>'
      + '</div>';
    bindControl();
    if (keepOpen) { const pop = boxCtl.querySelector('.pop[data-popfor="c"]'); if (pop) pop.removeAttribute('hidden'); }
    if (editing) { const inp = boxCtl.querySelector('#acNameInput'); if (inp) { inp.focus(); inp.select(); } }
  }

  // ejecterBoite : fin de session propre (op9). La sonde tient parfois la
  // connexion un instant : quelques tentatives avant d'abandonner.
  async function ejecterBoite(bouton) {
    const b = box();
    if (!b || !b.connected || syncEnCours) return;
    bouton.disabled = true;
    let err = 'occupee';
    for (let essai = 0; essai < 4 && err === 'occupee'; essai++) {
      try { await EjecterBoite(b.mac); err = ''; }
      catch (e) { err = String((e && e.message) || e); if (err === 'occupee') await new Promise((r) => setTimeout(r, 700)); }
    }
    if (!err) {
      // Le passage du statut a "deconnectee" suffit comme retour.
      b.connected = false; b.batt = null;
      renderBoxControl(); renderContext(); majEtatBoutons();
    } else {
      bouton.disabled = false;
      bouton.title = t('ihm_ejecte_echec'); // explication au survol, sans message flottant
    }
  }

  function sauverNom() {
    const inp = boxCtl.querySelector('#acNameInput'); if (!inp) return;
    const v = inp.value.trim(); if (v) { box().name = v; try { NommerBoite(box().mac, v); } catch (_) {} }
    editing = false; renderBoxControl(true); renderContext();
  }
  function closePops() { boxCtl.querySelectorAll('.pop').forEach((p) => p.setAttribute('hidden', '')); }
  function bindControl() {
    boxCtl.querySelectorAll('[data-pop]').forEach((tg) => tg.addEventListener('click', (e) => {
      e.stopPropagation();
      const pop = boxCtl.querySelector('.pop[data-popfor="' + tg.getAttribute('data-pop') + '"]');
      const open = pop.hasAttribute('hidden'); closePops();
      if (open) pop.removeAttribute('hidden');
    }));
    boxCtl.querySelectorAll('.pop').forEach((p) => p.addEventListener('click', (e) => e.stopPropagation()));
    boxCtl.querySelectorAll('[data-box]').forEach((b) => b.addEventListener('click', () => switchBox(b.getAttribute('data-box'))));
    boxCtl.querySelectorAll('[data-add]').forEach((b) => b.addEventListener('click', () => { closePops(); openModal(); }));
    boxCtl.querySelectorAll('[data-eject]').forEach((el) => el.addEventListener('click', (e) => { e.stopPropagation(); ejecterBoite(el); }));
    boxCtl.querySelectorAll('[data-edit]').forEach((b) => b.addEventListener('click', (e) => { e.stopPropagation(); editing = true; renderBoxControl(true); }));
    boxCtl.querySelectorAll('[data-save]').forEach((b) => b.addEventListener('click', (e) => { e.stopPropagation(); sauverNom(); }));
    boxCtl.querySelectorAll('[data-cancel]').forEach((b) => b.addEventListener('click', (e) => { e.stopPropagation(); editing = false; renderBoxControl(true); }));
    const inp = boxCtl.querySelector('#acNameInput');
    if (inp) inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') sauverNom();
      else if (e.key === 'Escape') { editing = false; renderBoxControl(true); }
    });
  }
  document.addEventListener('click', (e) => { if (!e.target.closest('#boxControl')) closePops(); });

  function switchBox(id) {
    const i = boites.findIndex((b) => b.id === id);
    if (i >= 0) active = i;
    editing = false;
    closePops(); renderBoxControl(); renderContext(); rafraichirEspaceAjouts(); rafraichirAccueil(); majPanneauRetrait();
    // Recharge l'arbre de la nouvelle boîte, meme onglet Boite masque : les
    // compteurs du bloc haut-droite (histoires, a ajouter / retirer) en viennent.
    chargerArbre(true);
  }

  // Panneau Radio : Wi-Fi domestique + radios. Le bouton des radios n'apparait
  // que si la boite a un Wi-Fi domestique (sta_wifi.json : lu a l'association ou
  // pose via "Definir le Wi-Fi") : sans Internet, la boite ne les joue pas.
  // "Ajouter" si l'arbre n'a pas de radios, sinon "Restaurer" (remplace la
  // branche par toutes les radios disponibles).
  function aWifi(b) { return !!(b && b.wifiMaison); }
  function majPanneauRadio() {
    const b = box(); const btn = q('#radiosBtn'), sub = q('#radioSub');
    if (!btn || !sub) return;
    const wifi = aWifi(b);
    const radios = !!(vueArbre.aDesRadios && vueArbre.aDesRadios());
    sub.textContent = !wifi ? t('ihm_radio_sub_sanswifi')
      : t(radios ? 'ihm_radio_sub_avec' : 'ihm_radio_sub_sans', { ssid: b.wifiMaison });
    btn.hidden = !wifi;
    if (!btn.disabled) btn.textContent = '＋ ' + t(radios ? 'ihm_radio_restaurer' : 'ihm_radio_ajouter');
  }

  let syncEnCours = false;
  let finSync = null; // Promise resolue a la fin de la synchro en cours (null hors synchro)
  function majEtatBoutons() {
    const b = box();
    const c = !!(b && b.connected); // connectee = boite en mode point d'acces
    const trop = depasse(b);        // capacite depassee : synchro bloquee
    // Rien a publier (editions evaluees cote Go, cf. rafraichirEditions) : une
    // synchro republierait l'arbre a l'identique, sans effet.
    const rien = !!(b && b.editions === false);
    const sb = q('#syncBtn');
    if (sb) { sb.disabled = syncEnCours || !c || trop || rien; sb.title = rien && c ? t('ihm_sync_rien') : ''; }
    const ow = q('#optWifi'); if (ow) ow.disabled = syncEnCours || !c; // Wi-Fi : boite en AP requise
    const oh = q('#optHoraires'); if (oh) oh.disabled = syncEnCours || !c; // Horaires : boite en AP requise
    // Restauration : boite en AP requise ; simple suppression : toujours possible.
    const rb = q('#restaurerBtn'); if (rb) rb.disabled = syncEnCours || (rb.dataset.mode === 'restaurer' && !c);
    const ej = boxCtl.querySelector('[data-eject]'); if (ej) ej.disabled = syncEnCours || !!(b && b.occupee);
  }

  function renderContext() {
    const b = box();
    // Accueil REEL : les series ayant des histoires en attente de synchro (_new),
    // adressees a l'enfant (b.name). Donnees locales (arbre de travail).
    const acc = b.accueil || [];
    const nbHist = acc.reduce((s, x) => s + x.nbNew, 0);
    const jamais = !b.derniereSync; // jamais synchronisee -> pas de "(derniere synchro ...)"
    // Premiere utilisation : boite fraichement ajoutee (jamais synchro) et aucune
    // serie suivie avec des nouveautes. On incite a parcourir le catalogue plutot
    // que d'afficher un trompeur "tout est a jour".
    const premiere = jamais && !acc.length;
    const tt = q('#accueilTitre');
    if (tt) tt.textContent = premiere ? t('ihm_home_bienvenue_titre')
      : (nbHist > 0 ? t('ihm_accueil_titre', { name: b.name }) : t('ihm_accueil_titre_ajour', { name: b.name }));
    if (premiere) {
      q('#homeSummary').innerHTML = t('ihm_home_bienvenue_sum', { name: esc(b.name) });
    } else if (nbHist > 0) {
      q('#homeSummary').innerHTML = plur(nbHist, 'ihm_home_intro_un', 'ihm_home_intro_n');
    } else {
      q('#homeSummary').innerHTML = jamais ? t('ihm_home_ajour_j') : t('ihm_home_ajour', { d: formatDepuis(b.derniereSync) });
    }
    const host = q('#homeContent');
    if (!acc.length) {
      if (premiere) {
        host.innerHTML = '<div class="empty"><div class="e-ico">🧭</div><h3>' + t('ihm_home_bienvenue_h') + '</h3>'
          + '<p>' + t('ihm_home_bienvenue_p') + '</p>'
          + '<button class="link-btn" data-aller-catalogue>' + t('ihm_home_bienvenue_cta') + ' →</button></div>';
        const bcat = host.querySelector('[data-aller-catalogue]');
        if (bcat) bcat.addEventListener('click', () => show('catalogue'));
      } else {
        host.innerHTML = '<div class="empty"><div class="e-ico">🎉</div><h3>' + t('ihm_home_empty_titre') + '</h3>'
          + '<p>' + t('ihm_home_empty_p', { name: esc(b.name) }) + '</p></div>';
      }
    } else {
      host.innerHTML = '<div class="grid home">' + acc.map((s, i) => {
        // Illustration haute resolution du web si connue ; sinon la version boite (128).
        const ill = s.img || ('boiteimg/' + encodeURIComponent(b.mac) + '/' + encodeURIComponent(s.uuid));
        return '<article class="card mini-card" tabindex="0" data-acc="' + i + '">'
          + '<div class="thumb sq" style="background:' + GRADS[i % GRADS.length] + '"><img loading="lazy" decoding="async" src="' + escAttr(imgCat(ill)) + '" alt="" onerror="this.remove()"></div>'
          + '<div class="body"><h3>' + esc(s.title) + '</h3>'
          + '<div class="metarow"><span class="tag"><span class="n">' + s.nbNew + '</span> ' + t('ihm_card_tag_new') + '</span></div>'
          + (s.src ? '<div class="serie-tagrow"><span class="link-btn">' + t('ihm_card_voir', { src: srcLabel(s.src) }) + ' →</span></div>' : '')
          + '</div></article>';
      }).join('') + '</div>';
      host.querySelectorAll('[data-acc]').forEach((el) => {
        const s = acc[+el.getAttribute('data-acc')];
        el.addEventListener('click', () => ouvrirDepuisAccueil(s));
        el.addEventListener('keydown', (e) => { if (e.key === 'Enter') ouvrirDepuisAccueil(s); });
      });
    }
    q('#boitePageTitre').textContent = t('ihm_boite_titre', { name: b.name });
    // Barre : espace occupe (accent) + bloc vert des histoires a ajouter (taille
    // reelle). Le remplissage total = usage futur (usedMo + addMo).
    const d = deltas(b);
    const gbar = q('#gBar');
    if (gbar) gbar.innerHTML = barreStockage(Object.assign({ classe: 'big' }, barOptsNet(b)));
    q('#gTotal').textContent = b.totalMo;
    // Valeur courante, et valeur future (apres synchro) si des editions sont en
    // attente : "3454 -> 4000" et "93 % -> 107 %". Le futur passe en rouge s'il
    // depasse la capacite.
    const futur = futurMo(b);
    const futurPct = b.totalMo > 0 ? Math.round(futur / b.totalMo * 100) : 0;
    const aDelta = (d.addMo > 0 || d.remMo > 0);
    const clsFutur = futur > b.totalMo ? 'futur over' : 'futur';
    if (aDelta) {
      q('#gUsed').innerHTML = b.usedMo + ' <span class="' + clsFutur + '">&rarr; ' + futur + '</span>';
      q('#gPct').innerHTML = pct(b) + ' % <span class="' + clsFutur + '">&rarr; ' + futurPct + ' %</span>';
    } else {
      q('#gUsed').textContent = b.usedMo;
      q('#gPct').textContent = pct(b) + ' %';
    }
    const dispo = b.totalMo - futur; // espace restant APRES la prochaine synchro
    const cap = q('#gCap');
    if (cap) {
      if (dispo < 0) { cap.className = 'gauge-cap warn'; cap.textContent = t('ihm_boite_depasse', { n: -dispo }); }
      // La légende cite AJOUTS et RETRAITS : la barre et le "575 -> 544" sont
      // nets (ajouts - retraits) ; ne citer que l'ajout semblait contradictoire.
      else if (d.addMo > 0 && d.remMo > 0) { cap.className = 'gauge-cap'; cap.textContent = t('ihm_boite_gauge_mixte', { a: d.addMo, r: d.remMo, n: dispo }); }
      else if (d.addMo > 0) { cap.className = 'gauge-cap'; cap.textContent = t('ihm_boite_gauge_ajout', { a: d.addMo, n: dispo }); }
      else if (d.remMo > 0) { cap.className = 'gauge-cap'; cap.textContent = t('ihm_boite_gauge_retrait', { r: d.remMo, n: dispo }); }
      else { cap.className = 'gauge-cap'; cap.textContent = t('ihm_boite_gauge_cap', { n: dispo }); }
    }
    q('#cOn').textContent = b.onSpk;
    q('#cAdd').textContent = b.add;
    q('#cRem').textContent = b.rem;
    const hs = q('#hintSsid'); if (hs) hs.textContent = b.ssid;
    // Le contenu de #boxCats est géré par la vue arbre réelle (voir vueArbre).
  }

  // ---- Vue arbre de la boîte (Contenu par catégorie) ----
  let arbreMac = null; // MAC de la boîte dont l'arbre est actuellement affiché
  let chargementEnCours = false; // évite de persister pendant le chargement initial
  let ajoutEnCours = false; // un ajout catalogue ecrit le travail : la vue arbre
  //                           ne doit pas persister sa copie (potentiellement
  //                           perimee) par-dessus, sous peine de tronquer.
  // Pendant un ajout, l'arbre est aussi VERROUILLE visuellement (tree-lock) :
  // l'ajout recharge l'arbre depuis le disque a la fin, donc une edition faite
  // pendant ce temps serait perdue. Compteur pour gerer les ajouts imbriques.
  let ajoutProfondeur = 0;
  function debutAjout() {
    ajoutProfondeur++;
    ajoutEnCours = true;
    const bc = q('#boxCats'); if (bc) bc.classList.add('tree-lock');
  }
  function finAjout() {
    if (--ajoutProfondeur > 0) return;
    ajoutProfondeur = 0;
    ajoutEnCours = false;
    const bc = q('#boxCats'); if (bc) bc.classList.remove('tree-lock');
  }
  const vueArbre = creerVueArbre({ host: q('#boxCats'), onChange: majCompteursBoite, onErreur: (m) => afficheMsg(m, 'err') });

  let genArbre = 0; // chargements de la vue : seul le plus recent s'affiche
  async function chargerArbre(force) {
    const mac = box().mac;
    if (!force && arbreMac === mac) return;
    arbreMac = mac;
    const gen = ++genArbre;
    try {
      // Arbre de travail (editions en cours), sinon arbre de la boite.
      const nodes = await chargerTravailBoite(mac);
      if (gen !== genArbre) return; // un chargement plus recent l'emporte
      chargementEnCours = true;
      vueArbre.charger(nodes, mac);
      majCompteursBoite();
      chargementEnCours = false;
      rafraichirEditions();
      chargerEtats(); // favoris (etoiles) de la boite chargee
      // Tailles (inventaire boite + ajouts en bibliotheque) : chargees a part
      // (asynchrone) et injectees dans la vue, qui se re-rend.
      TaillesArbre(mac).then((m) => { if (arbreMac === mac) vueArbre.setTailles(m || {}); }).catch(() => {});
    } catch (_) {
      if (gen !== genArbre) return;
      arbreMac = null;  // échec (boîte non lue) : on retentera au prochain affichage
      chargementEnCours = false;
      vueArbre.vide();
    }
  }
  // majCompteursBoite : réinjecte les compteurs réels (histoires / à retirer) de
  // l'arbre dans la boîte active, puis rafraîchit panneau et bloc haut-droite.
  function majCompteursBoite() {
    const c = vueArbre.compteurs();
    const b = box();
    b.onSpk = c.surBoite; b.add = c.aAjouter || 0; b.rem = c.aRetirer;
    renderContext();
    renderBoxControl();
    majPanneauRadio();
    planifierEspaceAjouts(); // taille reelle des ajouts (asynchrone, debounce)
    // Persiste l'arbre de travail (hors chargement, hors ajout catalogue en
    // cours) pour survivre aux redémarrages. Pendant un ajout, l'ajout est le
    // seul ecrivain : la vue arbre ne doit pas ecraser avec une copie perimee.
    // Idem quand la vue a ete invalidee (arbreMac != boite active) : sa copie
    // ne reflete plus le travail sur disque, seul un rechargement la rafraichit.
    if (!chargementEnCours && !ajoutEnCours && b.mac && arbreMac === b.mac) {
      try {
        persisterTravail(b.mac, JSON.stringify(vueArbre.arbreActuel(), (k, v) => (k === '_editing' ? undefined : v)));
      } catch (_) { /* serialisation impossible : rien a ecrire */ }
    }
  }

  // File d'ecriture du travail depuis la vue arbre. Les appels Wails partent en
  // parallele et peuvent aboutir dans le desordre : un etat plus ancien
  // ecraserait le plus recent. On n'a donc qu'une ecriture en vol ; les etats
  // arrives entre-temps sont fusionnes (le dernier par boite gagne). Un echec est
  // affiche (sinon les editions seraient perdues au redemarrage sans un mot).
  const travailEnAttente = new Map(); // mac -> json
  let fileTravail = null;             // Promise de la file en cours, ou null
  function persisterTravail(mac, json) {
    travailEnAttente.set(mac, json);
    if (!fileTravail) fileTravail = viderFileTravail();
  }
  async function viderFileTravail() {
    let ecrit = false;
    while (travailEnAttente.size) {
      const [mac, json] = travailEnAttente.entries().next().value;
      travailEnAttente.delete(mac);
      try { await EnregistrerArbreTravail(mac, json); ecrit = true; }
      catch (e) { afficheMsg(t('ihm_travail_erreur', { e: msgErreur(e) }), 'err'); }
    }
    fileTravail = null;
    if (ecrit) rafraichirEtats(); // catalogue et Mes histoires suivent la vue arbre
  }

  // modifierTravail : point d'ecriture UNIQUE du travail hors de la vue arbre
  // (catalogue, histoires perso). Les modifications passent une a une (deux
  // ajouts concurrents relisant le meme travail s'ecraseraient) : relecture du
  // travail, mutation(a) en memoire (renvoyer false = rien a ecrire), ecriture,
  // puis rechargement de la vue arbre, meme onglet Boite masque. La vue reste
  // ainsi le miroir du fichier, et tout ce qui en derive (compteurs du bloc
  // haut-droite, espace des ajouts, panneau radio) est a jour. Resout a vrai si
  // le travail a ete modifie.
  let fileModifs = Promise.resolve();
  function modifierTravail(mac, mutation) {
    const tache = fileModifs.then(async () => {
      // Pendant une synchro, la publication a deja lu le travail et le soldera a
      // la fin : une modification en vol (telechargement lance juste avant)
      // attend la fin de la synchro pour s'appliquer au travail suivant.
      while (finSync) await finSync;
      debutAjout();
      let fait = false;
      try {
        const a = await chargerTravailBoite(mac);
        fait = (await mutation(a)) !== false;
        if (fait) await EnregistrerArbreTravail(mac, JSON.stringify(a));
      } finally {
        finAjout();
      }
      if (fait && box() && box().mac === mac) {
        await chargerArbre(true);
        await rafraichirEtats();
      }
      return fait;
    });
    fileModifs = tache.catch(() => {});
    return tache;
  }

  // ---- Catalogue (données réelles : iTunes + Radio France) ----
  let curSrc = 'itunes';
  let catInit = false;         // chargé au moins une fois ?
  const catCache = {};         // séries initiales, par source
  let searchTimer = null;
  const searchInput = q('#searchInput'), catListWrap = q('#catList'), catDetail = q('#catDetail');

  async function enLigne() { try { return await Internet(); } catch (_) { return true; } }

  function etatCarte(cls, ico, titre, texte, retry) {
    return '<div class="cat-state ' + cls + '">' + (ico ? '<div class="cs-ico">' + ico + '</div>' : '<div class="spin"></div>')
      + (titre ? '<h3>' + titre + '</h3>' : '') + (texte ? '<p>' + texte + '</p>' : '')
      + (retry ? '<button class="link-btn" data-retry>' + t('ihm_cat_retry') + '</button>' : '') + '</div>';
  }
  function afficheHorsLigne(hote) {
    hote.innerHTML = (hote === catDetail ? boutonRetour() : '') + etatCarte('offline', '📡', t('ihm_cat_offline_titre'), t('ihm_cat_offline_p'), true);
    if (hote === catDetail) lierRetour();
    const r = hote.querySelector('[data-retry]');
    if (r) r.addEventListener('click', rafraichirCatalogue);
  }

  // Ferme la vue detail (sans recharger) puis charge la liste du catalogue.
  // Appele par l'onglet catalogue et par le bouton retry.
  async function rafraichirCatalogue() {
    fermerVueDetail();
    await chargerCatalogue();
  }

  // Charge et affiche la liste des series. NE ferme PAS le detail et ne
  // s'appelle jamais lui-meme via fermerDetail : pas de recursion possible.
  async function chargerCatalogue() {
    catInit = true;
    if (!(await enLigne())) { afficheHorsLigne(catListWrap); return; }
    const terme = (searchInput.value || '').trim();
    catListWrap.innerHTML = etatCarte('', '', '', t('ihm_cat_loading'));
    try {
      let list;
      if (terme) list = await RechercheCatalogue(curSrc, terme);
      else {
        if (!catCache[curSrc]) catCache[curSrc] = await CatalogueInitial(curSrc);
        list = catCache[curSrc];
      }
      // Liste vide : on revérifie la connexion (elle a pu tomber entre-temps,
      // ex. bascule sur l'AP de la boite) pour ne pas afficher "aucun résultat"
      // alors que c'est Internet qui manque.
      if (!(list && list.length) && !(await enLigne())) { afficheHorsLigne(catListWrap); return; }
      await chargerEtats();
      renderSeries(list || [], terme);
    } catch (_) {
      if (!(await enLigne())) { afficheHorsLigne(catListWrap); return; }
      catListWrap.innerHTML = etatCarte('', '⚠️', t('ihm_cat_error'), '', true);
      const r = catListWrap.querySelector('[data-retry]'); if (r) r.addEventListener('click', chargerCatalogue);
    }
  }

  // ---- Ajout catalogue -> boite : etats, insertion dans l'arbre de travail ----
  let etatsCat = { boite: new Set(), ajout: new Set(), suppr: new Set(), fav: new Set() };
  async function chargerEtats() {
    try {
      const e = await EtatsBoite(box().mac);
      etatsCat = { boite: new Set(e.surBoite || []), ajout: new Set(e.attenteAjout || []), suppr: new Set(e.attenteSuppr || []), fav: new Set(e.favoris || []) };
    } catch (_) { etatsCat = { boite: new Set(), ajout: new Set(), suppr: new Set(), fav: new Set() }; }
    // Les etoiles de l'arbre suivent les memes favoris (favoris.json) : sans
    // ca, elles n'arrivaient qu'au passage connecte, rate apres un appairage.
    if (vueArbre.majFavoris) vueArbre.majFavoris(etatsCat.fav);
  }
  // rafraichirEditions : y a-t-il quelque chose a synchroniser ? Meme regle que
  // la publication (EditionsEnAttente) ; pilote l'activation du bouton.
  async function rafraichirEditions() {
    const b = box(); if (!b || !b.mac) return;
    try {
      const v = await EditionsEnAttente(b.mac);
      if (b === box()) { b.editions = !!v; majEtatBoutons(); }
    } catch (_) { /* inconnu : bouton laisse actif */ }
  }
  // rafraichirEtats : relit les etats (EtatsBoite, fichiers locaux, sans reseau)
  // et re-rend EN PLACE tout ce qui les affiche : liste des series, cellules
  // d'episode et bouton Suivre/Suivie du detail ouvert, Mes histoires. Appele
  // apres CHAQUE ecriture du travail (vue arbre, modifierTravail) et apres une
  // synchro : ces vues ne doivent jamais deriver d'un etat perime. Les etats
  // transitoires ne gardent que ce qui est en cours (telechargement, attente,
  // erreur) ; "a synchroniser" / "a supprimer" viennent des etats relus.
  const ETATS_EN_COURS = new Set(['load', 'attente', 'err']);
  async function rafraichirEtats() {
    rafraichirEditions();
    for (const [u, e] of etatTransitoire) if (!ETATS_EN_COURS.has(e)) etatTransitoire.delete(u);
    try { await chargerEtats(); } catch (_) { /* local, best-effort */ }
    if (!catListWrap.hidden && derniereListe.length) renderSeries(derniereListe);
    if (!catDetail.hidden) {
      catDetail.querySelectorAll('.ep-card[data-ep-uuid]').forEach((c) => {
        const uuid = c.getAttribute('data-ep-uuid');
        const cur = c.querySelector('.ep-pied > .ep-ind');
        if (cur) cur.outerHTML = indicateurEp(etatTransitoire.get(uuid) || etatUUID(uuid), +c.dataset.i || 0);
      });
      const bs = catDetail.querySelector('[data-serie-add]');
      if (bs && !bs.classList.contains('loading')) { // pas pendant l'ajout de la serie
        const suivie = serieSuivie(bs.dataset.uuid);
        bs.classList.toggle('suivie', suivie);
        bs.disabled = suivie;
        bs.textContent = t(suivie ? 'ihm_serie_suivie' : 'ihm_serie_ajouter');
      }
    }
    const vh = q('#histoires'); if (vh && vh.classList.contains('active')) chargerPerso();
  }
  function etatUUID(u) {
    if (!u) return '';
    if (etatsCat.suppr.has(u)) return 'suppr';
    if (etatsCat.ajout.has(u)) return 'ajout';
    if (etatsCat.boite.has(u)) return 'boite';
    return '';
  }
  function serieSuivie(u) { return !!u && (etatsCat.boite.has(u) || etatsCat.ajout.has(u)); }
  // Favori pose par l'enfant sur la boite (fav.bin, lecture seule).
  function estFavori(u) { return !!u && etatsCat.fav.has(u); }

  // Arbre de travail de la boite active (travail persiste, sinon arbre reel filtre).
  async function chargerTravailBoite(mac) {
    // Les autres ecrivains relisent ici : attendre la file de la vue arbre, pour
    // qu'aucune de ses ecritures n'arrive APRES la leur.
    while (fileTravail) await fileTravail;
    try {
      const brut = await ArbreTravail(mac);
      if (brut) { const a = JSON.parse(brut); if (Array.isArray(a)) return a; }
    } catch (_) { /* ignore */ }
    return (await ArbreBoite(mac)) || []; // sans les categories systeme
  }
  // Categories editables (hors systeme) de l'arbre de travail.
  async function categoriesBoite(mac) {
    const arbre = await chargerTravailBoite(mac);
    // _img : illustration locale (dataURL) d'une categorie creee/editee dans
    // l'IHM, pas encore poussee -> a utiliser avant l'image de la boite.
    return arbre.filter((c) => Array.isArray(c.child)).map((c) => ({ uuid: c.uuid, title: c._titre || c.title, img: c._img || '' }));
  }
  function trouverSerie(arbre, serieUUID) {
    for (const cat of arbre) if (Array.isArray(cat.child)) for (const sn of cat.child) if (sn.uuid === serieUUID) return sn;
    return null;
  }
  // Insere une feuille episode sous son noeud serie (cree si besoin sous categorie).
  function insererEpisode(arbre, serie, ep, categorieUUID) {
    let sn = trouverSerie(arbre, serie.uuid);
    if (!sn) {
      const cat = arbre.find((c) => c.uuid === categorieUUID);
      if (!cat) return false;
      if (!Array.isArray(cat.child)) cat.child = [];
      // _src / _srcId / _srcImg : identite catalogue (source, id, illustration
      // haute resolution), pour rouvrir la serie et l'afficher sur l'accueil.
      // Cles d'edition locales : jamais poussees a la boite.
      sn = { uuid: serie.uuid, title: serie.title, child: [], _new: true };
      cat.child.push(sn);
    }
    // Backfill de l'identite catalogue (utile pour une serie creee avant, ou
    // reconstruite depuis la boite, qui ne l'avait pas encore).
    if (!sn._src && curSrc) sn._src = curSrc;
    if (!sn._srcId && serie.id) sn._srcId = serie.id;
    if (!sn._srcImg && serie.image) sn._srcImg = serie.image;
    if (!Array.isArray(sn.child)) sn.child = [];
    if (!sn.child.some((x) => x.uuid === ep.uuid)) {
      sn.child.push({ uuid: ep.uuid, title: ep.title, add_time: Math.floor(Date.now() / 1000), _new: true });
    }
    return true;
  }

  // Selecteur de categorie (liste des categories existantes, sans "nouvelle").
  let catPop = null;
  function choisirCategorie(cats, anchorEl) {
    return new Promise((resolve) => {
      if (!catPop) {
        catPop = document.createElement('div');
        catPop.className = 'catpop'; catPop.hidden = true;
        (q('#boxCats').closest('.ihm-root') || document.body).appendChild(catPop);
      }
      const mac = box().mac;
      const items = cats.length ? cats.map((c) => {
        // Illustration locale (dataURL) en priorite, sinon l'image de la boite.
        const ill = c.img || ('boiteimg/' + encodeURIComponent(mac) + '/' + encodeURIComponent(c.uuid));
        return '<button class="catpop-item" data-uuid="' + escAttr(c.uuid) + '">'
          + '<span class="catpop-ill" style="' + escAttr('background-image:url("' + imgCat(ill) + '")') + '"></span>'
          + '<span class="catpop-nom">' + esc(c.title) + '</span></button>';
      }).join('') : '<div class="catpop-vide">' + t('ihm_add_cat_vide') + '</div>';
      catPop.innerHTML = '<div class="catpop-t">' + t('ihm_add_cat_titre') + '</div>' + items;
      catPop.hidden = false;
      const r = anchorEl.getBoundingClientRect();
      catPop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - catPop.offsetWidth - 8)) + 'px';
      catPop.style.top = Math.min(r.bottom + 6, window.innerHeight - catPop.offsetHeight - 8) + 'px';
      let fini = false;
      const fermer = (uuid) => { if (fini) return; fini = true; catPop.hidden = true; document.removeEventListener('mousedown', horsClic, true); resolve(uuid); };
      const horsClic = (e) => { if (!catPop.contains(e.target) && e.target !== anchorEl) fermer(null); };
      setTimeout(() => document.addEventListener('mousedown', horsClic, true), 0);
      catPop.querySelectorAll('[data-uuid]').forEach((b) => b.addEventListener('click', () => fermer(b.getAttribute('data-uuid'))));
    });
  }

  // Indicateur d'etat d'un episode dans le detail. Enveloppe unique `.ep-ind`
  // (une seule cible pour majIndicateurEp). Etats "sur la boite"/"a synchroniser"
  // portent un bouton Supprimer ; "a supprimer" un bouton Annuler.
  function indicateurEp(etat, i) {
    const del = '<button class="ep-del" data-del title="' + escAttr(t('ihm_ep_suppr')) + '" aria-label="' + escAttr(t('ihm_ep_suppr')) + '">' + icone('trash-2') + '</button>';
    const undo = '<button class="ep-undo" data-undo title="' + escAttr(t('ihm_ep_annuler')) + '" aria-label="' + escAttr(t('ihm_ep_annuler')) + '">' + icone('undo-2') + '</button>';
    const wrap = (inner) => '<span class="ep-ind">' + inner + '</span>';
    if (etat === 'boite') return wrap('<span class="ep-badge surboite">' + t('ihm_etat_boite') + '</span>' + del);
    if (etat === 'attente') return wrap('<span class="ep-badge attente" title="' + t('ihm_etat_attente') + '">' + t('ihm_etat_attente') + '</span>');
    if (etat === 'ajout') return wrap('<span class="ep-badge ajout">' + t('ihm_etat_ajout') + '</span>' + del);
    if (etat === 'suppr') return wrap('<span class="ep-badge suppr">' + t('ihm_etat_suppr') + '</span>' + undo);
    if (etat === 'load') return wrap('<span class="ep-etat load"><span class="mini-spin"></span></span>');
    if (etat === 'err') return wrap('<span class="ep-etat err" title="' + t('ihm_etat_bloque') + '">⚠</span>');
    return wrap('<button class="ep-add" data-ep="' + i + '">' + t('ihm_add') + '</button>');
  }
  // urlAudio : URL de flux utilisable par l'element audio. new URL encode espaces
  // et accents mais garde les %XX deja presents ; encodeURI les re-encodait
  // (%3A -> %253A) et cassait les URL de suivi (Ausha : podcastUrl=https%3A...).
  function urlAudio(src) {
    try { return new URL(src).href; } catch (_) { return src; }
  }
  // Etat transitoire des indicateurs pendant un ajout (modele, pas DOM) : la
  // liste est virtualisee, une cellule non montee doit retrouver son etat au
  // remontage. Reinitialise a chaque ouverture de detail.
  let etatTransitoire = new Map();
  let vlActive = null; // liste virtuelle du detail courant (a detruire au changement)
  let nettoyageDetail = null; // coupe le lecteur et retire les ecouteurs du detail courant
  function detruireVL() {
    if (vlActive) { vlActive.detruire(); vlActive = null; }
    if (nettoyageDetail) { nettoyageDetail(); nettoyageDetail = null; }
  }

  // Met a jour l'indicateur d'un episode : dans le modele, et sur la cellule si
  // elle est montee dans le DOM.
  function majIndicateurEp(uuid, etat) {
    etatTransitoire.set(uuid, etat);
    const c = catDetail.querySelector('.ep-card[data-ep-uuid="' + (window.CSS && CSS.escape ? CSS.escape(uuid) : uuid) + '"]');
    if (!c) return;
    const cur = c.querySelector('.ep-pied > .ep-ind');
    if (cur) cur.outerHTML = indicateurEp(etat, +c.dataset.i || 0);
  }

  // Categorie cible : celle de la serie si deja placee, sinon demandee a l'utilisateur.
  async function categoriePourSerie(serie, anchorEl) {
    const arbre = await chargerTravailBoite(box().mac);
    const sn = trouverSerie(arbre, serie.uuid);
    if (sn) return true; // deja placee : insererEpisode la retrouvera
    const cats = arbre.filter((c) => Array.isArray(c.child)).map((c) => ({ uuid: c.uuid, title: c._titre || c.title }));
    return await choisirCategorie(cats, anchorEl); // uuid choisi, ou null si annule
  }

  // Telecharge + insere UN episode (sans demander de categorie).
  async function ajouterUnEpisode(serie, ep, categorieUUID) {
    const mac = box().mac;
    const r = await TelechargerEpisode(ep.uuid, ep.audioSrc, ep.image || serie.image);
    if (!r.ok) return { ok: false, message: r.message };
    await modifierTravail(mac, (a) => { insererEpisode(a, serie, ep, categorieUUID); });
    // Serie suivie : memorise son identite catalogue (survit aux synchros).
    if (serie && serie.uuid && serie.id) { try { EnregistrerSuivi(mac, serie.uuid, curSrc, serie.id, serie.image || '', serie.title || ''); } catch (_) {} }
    return { ok: true };
  }

  // supprimerEpisode : si l'histoire est un ajout en attente (_new), retrait
  // IMMEDIAT du travail (et de la serie neuve devenue vide) ; si elle est deja
  // sur la boite, on la marque _rem (elle passera "a supprimer" a la synchro).
  function supprimerEpisode(uuid) {
    return modifierTravail(box().mac, (a) => {
      let fait = false;
      for (let ci = 0; ci < a.length && !fait; ci++) {
        const cat = a[ci];
        if (!Array.isArray(cat.child)) continue;
        for (let si = 0; si < cat.child.length && !fait; si++) {
          const sn = cat.child[si];
          if (!Array.isArray(sn.child)) continue;
          const ei = sn.child.findIndex((e) => e.uuid === uuid);
          if (ei < 0) continue;
          if (sn.child[ei]._new || sn._new || cat._new) {
            sn.child.splice(ei, 1);                                  // ajout en attente -> retrait
            if (sn._new && sn.child.length === 0) cat.child.splice(si, 1); // serie neuve vide
          } else {
            sn.child[ei]._rem = true;                               // sur la boite -> a supprimer
          }
          fait = true;
        }
      }
      return fait;
    });
  }

  // annulerSuppression : retire le drapeau _rem d'une histoire (repasse "sur la boite").
  function annulerSuppression(uuid) {
    return modifierTravail(box().mac, (a) => {
      let fait = false;
      const walk = (ns) => { for (const n of ns) { if (n.uuid === uuid && n._rem) { delete n._rem; fait = true; } if (Array.isArray(n.child)) walk(n.child); } };
      walk(a);
      return fait;
    });
  }

  // Ajout d'un seul episode (bouton par episode).
  async function ajouterEpisode(serie, ep, anchorEl) {
    const cat = await categoriePourSerie(serie, anchorEl);
    if (cat === null) return { ok: false, annule: true };
    if (serie.uuid) await TelechargerImageSerie(serie.uuid, serie.image);
    const r = await ajouterUnEpisode(serie, ep, cat === true ? null : cat);
    if (!r.ok) return r;
    await chargerEtats();
    return { ok: true };
  }

  let derniereListe = [];
  function renderSeries(list) {
    derniereListe = list || [];
    if (!list.length) {
      catListWrap.innerHTML = '<div class="empty"><div class="e-ico">🔍</div><p>' + t('ihm_cat_empty') + '</p></div>';
      return;
    }
    const cards = list.map((p, i) => {
      // Image en <img loading="lazy"> : les vignettes hors écran ne sont pas
      // décodées, ce qui allège fortement la mémoire de la webview.
      const vis = p.image
        ? '<div class="thumb sq"><img loading="lazy" decoding="async" src="' + escAttr(imgCat(p.image)) + '" alt=""></div>'
        : '<div class="thumb sq" style="background:' + GRADS[i % GRADS.length] + '"><span class="ico">🎧</span></div>';
      // Pas de nombre d'épisodes sur les cartes : Radio France ne le fournit pas
      // dans son listing, donc on ne l'affiche nulle part (cohérence iTunes/RF).
      const meta = esc(p.artist || '');
      const suivi = serieSuivie(p.uuid) ? '<div class="serie-tagrow"><span class="serie-tag suivie">' + t('ihm_serie_suivie') + '</span></div>' : '';
      return '<article class="card mini-card" tabindex="0" data-serie="' + i + '">' + vis
        + '<div class="body"><h3>' + esc(p.title) + '</h3>' + (meta ? '<div class="metarow">' + meta + '</div>' : '') + suivi + '</div></article>';
    }).join('');
    catListWrap.innerHTML = '<div class="grid catalog">' + cards + '</div>';
    catListWrap.querySelectorAll('[data-serie]').forEach((el) => {
      const p = list[+el.getAttribute('data-serie')];
      el.addEventListener('click', () => ouvrirSerie(p));
      el.addEventListener('keydown', (e) => { if (e.key === 'Enter') ouvrirSerie(p); });
    });
  }

  // Ferme uniquement la vue detail et revele la liste. Aucun rechargement.
  function fermerVueDetail() {
    detruireVL(); // libere les ecouteurs de scroll de la liste virtuelle
    catDetail.hidden = true; catListWrap.hidden = false;
  }

  // Bouton Retour : ferme le detail, puis reaffiche la liste connue, ou la
  // charge si elle est vide (serie ouverte depuis l'accueil). Appelle
  // chargerCatalogue (pas rafraichirCatalogue) pour eviter toute recursion.
  function fermerDetail() {
    fermerVueDetail();
    if (derniereListe.length) chargerEtats().then(() => renderSeries(derniereListe));
    else chargerCatalogue();
  }
  function boutonRetour() { return '<button class="back-btn" data-back>' + t('ihm_cat_back') + '</button>'; }
  function lierRetour() { const b = catDetail.querySelector('[data-back]'); if (b) b.addEventListener('click', fermerDetail); }

  function enteteSerie(p, feed) {
    const img = imgCat((feed && feed.image) || p.image);
    const vis = img
      ? escAttr('background-image:url("' + img + '");background-size:cover;background-position:center')
      : 'background:' + grad('#8CA0E6', '#63B9A2');
    const n = feed && feed.episodes ? feed.episodes.length : (p.nbEpisode || 0);
    const desc = feed && feed.description ? '<p class="serie-desc">' + esc(feed.description) + '</p>' : '';
    // Bouton Suivre/Suivie a DROITE du titre. Affiche seulement une fois le flux
    // charge ET non vide : suivre une serie sans episode ecoutable n'a pas de sens
    // (et on evite un bouton qui apparait puis disparait pendant le chargement).
    const suivie = serieSuivie(p.uuid);
    const avecEpisodes = !!(feed && feed.episodes && feed.episodes.length);
    const bouton = avecEpisodes
      ? '<div class="detail-suivre"><button class="serie-add' + (suivie ? ' suivie' : '') + '" data-serie-add'
        + (suivie ? ' disabled' : '') + '>' + (suivie ? t('ihm_serie_suivie') : t('ihm_serie_ajouter')) + '</button></div>'
      : '';
    // Illustration grand format a gauche (meme gabarit que les cartes de
    // l'accueil), titre + bouton puis description a droite.
    return '<div class="detail-head"><div class="detail-thumb" style="' + vis + '">' + (img ? '' : '🎧') + '</div>'
      + '<div class="detail-info"><div class="detail-titre"><div class="detail-titre-txt">'
      + '<p class="eyebrow" style="margin-bottom:2px">' + esc(srcLabel(curSrc)) + (p.artist ? ' · ' + esc(p.artist) : '') + '</p>'
      + '<h1 class="page-title" style="font-size:26px">' + esc(p.title) + '</h1>'
      + (n ? '<p class="lead" style="margin-top:6px">' + t('ihm_ep_count', { n }) + '</p>' : '') + '</div>'
      + bouton + '</div>' + desc + '</div></div>';
  }

  function datePub(s) {
    if (!s) return '';
    const d = new Date(s);
    // Radio France fournit une date deja lisible ("Mardi 10 mars 2026"), que Date
    // ne sait pas lire : on la garde, sans le jour de la semaine.
    if (isNaN(d.getTime())) return String(s).replace(/^\p{L}+\s+(?=\d)/u, '');
    try { return d.toLocaleDateString(lang === 'en' ? 'en-US' : 'fr-FR', { year: 'numeric', month: 'short', day: 'numeric' }); } catch (_) { return ''; }
  }
  function afficheMsgCat(txt, type) {
    let m = catDetail.querySelector('#catMsg');
    if (!m) { m = document.createElement('div'); m.id = 'catMsg'; m.className = 'cat-msg'; catDetail.insertBefore(m, catDetail.firstChild.nextSibling); }
    m.className = 'cat-msg' + (type ? ' ' + type : ' err'); m.textContent = txt || '';
    m.hidden = !txt;
  }

  async function ouvrirSerie(p) {
    catListWrap.hidden = true; catDetail.hidden = false;
    catDetail.innerHTML = boutonRetour() + enteteSerie(p) + etatCarte('', '', '', t('ihm_cat_loading'));
    lierRetour();
    window.scrollTo({ top: 0, behavior: 'auto' });
    if (!(await enLigne())) { afficheHorsLigne(catDetail); return; }
    try {
      await chargerEtats();
      const feed = await EpisodesSerie(curSrc, p.id);
      const eps = (feed && feed.episodes) || [];
      if (!eps.length && !(await enLigne())) { afficheHorsLigne(catDetail); return; }
      afficherDetailSerie(p, feed, eps);
    } catch (_) {
      if (!(await enLigne())) { afficheHorsLigne(catDetail); return; }
      catDetail.innerHTML = boutonRetour() + etatCarte('', '⚠️', t('ihm_cat_error'), '', true);
      lierRetour();
      const r = catDetail.querySelector('[data-retry]'); if (r) r.addEventListener('click', () => ouvrirSerie(p));
    }
  }
  function afficherDetailSerie(p, feed, eps) {
    detruireVL();
    etatTransitoire = new Map();
    const deplies = new Set(); // cartes a description depliee : etat porte par le modele, pas la cellule
    catDetail.innerHTML = boutonRetour() + enteteSerie(p, feed)
      + (eps.length ? '<div class="ep-grille"></div>'
        : '<div class="empty"><div class="e-ico">🎧</div><p>' + t('ihm_ep_vide') + '</p></div>');
    lierRetour();
    const liste = catDetail.querySelector('.ep-grille');

    // Grille virtualisee PAR LIGNES : la liste virtuelle gere des lignes de
    // `cols` cartes. Le nombre de colonnes suit la largeur (re-montage au resize).
    const GAP = 20, MIN_CARTE = 280;
    let cols = 1;
    const colonnesPour = (w) => Math.max(1, Math.floor((w + GAP) / (MIN_CARTE + GAP)));
    const invaliderEp = (i) => { if (vlActive && i >= 0) vlActive.invalider(Math.floor(i / cols)); };
    const carteDe = (i) => (liste ? liste.querySelector('.ep-card[data-i="' + i + '"]') : null);
    // Lecteur unique de la serie (cle = index d'episode) : la lecture survit au
    // recyclage des cartes par la liste virtuelle.
    const lecteur = creerLecteur({
      carteDe, invalider: invaliderEp,
      onErreur: (i) => afficheMsgCat(eps[i].title + ' : ' + t('ihm_player_erreur')),
    });

    // Carte de l'episode i. Indicateur porte par `etatTransitoire` (ajout en
    // cours) sinon l'etat catalogue ; progression lue sur le lecteur unique.
    const carte = (i) => {
      const e = eps[i];
      const etat = etatTransitoire.get(e.uuid) || etatUUID(e.uuid);
      const actif = lecteur.actif(i);
      const ouvert = deplies.has(i);
      const src = e.image ? escAttr(imgCat(e.image)) : '';
      // Image entiere ("contain") sur un fond flou d'elle-meme : les vignettes
      // carrees (iTunes) ne sont pas rognees dans le cadre 16:9 (Radio France).
      const vis = e.image
        ? '<img class="fond" loading="lazy" decoding="async" src="' + src + '" alt="">'
          + '<img class="net" loading="lazy" decoding="async" src="' + src + '" alt="">'
        : '<span class="ph">🎧</span>';
      const fav = estFavori(e.uuid) ? '<span class="ep-fav" title="' + escAttr(t('ihm_fav_titre')) + '">♥</span>' : '';
      const duree = e.duration ? '<span class="ep-duree">' + fmtTemps(e.duration) + '</span>' : '';
      const play = lecteur.bouton(i, !!e.audioSrc);
      const progres = lecteur.progres(i, e.duration);
      const desc = e.description ? esc(e.description) : '<span class="muted">' + t('ihm_ep_nodesc') + '</span>';
      return '<article class="ep-card' + (actif ? ' joue' : '') + (ouvert ? ' ouvert' : '') + (e.audioSrc ? '' : ' sans-audio') + '"'
        + ' data-i="' + i + '" data-ep-uuid="' + escAttr(e.uuid) + '">'
        + '<div class="ep-vis-wrap"><div class="ep-vis">' + vis + fav + duree + '</div>' + play + '</div>'
        + '<button class="ep-txt" type="button" aria-expanded="' + ouvert + '"><b>' + esc(e.title) + '</b>'
        + '<span class="ep-date">' + esc(datePub(e.published)) + '</span><span class="ep-desc">' + desc + '</span></button>'
        + progres
        + '<div class="ep-pied">' + indicateurEp(etat, i) + '</div></article>';
    };
    const rendreLigne = (el, r) => {
      el.classList.add('ep-ligne');
      el.style.gridTemplateColumns = 'repeat(' + cols + ',minmax(0,1fr))';
      let h = '';
      for (let i = r * cols; i < Math.min(eps.length, (r + 1) * cols); i++) h += carte(i);
      el.innerHTML = h;
    };

    const onEpAdd = async (btn) => {
      const ep = eps[+btn.getAttribute('data-ep')];
      btn.disabled = true; btn.classList.add('loading'); btn.innerHTML = '<span class="mini-spin" aria-label="' + t('ihm_add_encours') + '"></span>';
      try {
        const r = await ajouterEpisode(p, ep, btn);
        if (r && r.annule) { btn.disabled = false; btn.classList.remove('loading'); btn.textContent = t('ihm_add'); return; }
        if (r && !r.ok) { afficheMsgCat(ep.title + ' : ' + msgErreur(r.message || t('ihm_add_echec'))); btn.disabled = false; btn.classList.remove('loading'); btn.classList.add('err'); btn.textContent = t('ihm_add'); return; }
        await majCellule(ep.uuid); // rafraichit la seule cellule (garde les accordeons ouverts)
      } catch (e) {
        afficheMsgCat(ep.title + ' : ' + msgErreur(e));
        btn.disabled = false; btn.classList.remove('loading'); btn.classList.add('err'); btn.textContent = t('ihm_add');
      }
    };

    // Suppression / annulation : agit sur le travail puis re-rend la cellule.
    const idxDe = (el) => { const c = el.closest('.ep-card'); return c ? +c.dataset.i : -1; };
    const epDe = (btn) => { const i = idxDe(btn); return i >= 0 ? eps[i] : null; };
    const majCellule = async (uuid) => {
      await chargerEtats();
      etatTransitoire.delete(uuid);
      invaliderEp(eps.findIndex((e) => e.uuid === uuid));
    };
    const onEpSuppr = async (btn) => { const ep = epDe(btn); if (!ep) return; btn.disabled = true; try { await supprimerEpisode(ep.uuid); await majCellule(ep.uuid); } catch (e) { afficheMsgCat(msgErreur(e)); } };
    const onEpUndo = async (btn) => { const ep = epDe(btn); if (!ep) return; btn.disabled = true; try { await annulerSuppression(ep.uuid); await majCellule(ep.uuid); } catch (e) { afficheMsgCat(msgErreur(e)); } };

    // Delegation : un seul listener pour toute la liste (cellules recyclees).
    if (liste) liste.addEventListener('click', (ev) => {
      const add = ev.target.closest('.ep-add');
      if (add) { ev.stopPropagation(); onEpAdd(add); return; }
      const del = ev.target.closest('.ep-del');
      if (del) { ev.stopPropagation(); onEpSuppr(del); return; }
      const undo = ev.target.closest('.ep-undo');
      if (undo) { ev.stopPropagation(); onEpUndo(undo); return; }
      const pl = ev.target.closest('.ep-play');
      if (pl) { const i = idxDe(pl); if (i >= 0) lecteur.basculer(i, urlAudio(eps[i].audioSrc)); return; }
      const bar = ev.target.closest('.track-bar');
      if (bar) { lecteur.chercher(bar, ev.clientX); return; }
      const txt = ev.target.closest('.ep-txt');
      if (!txt) return;
      const i = idxDe(txt); if (i < 0) return;
      if (deplies.has(i)) deplies.delete(i); else deplies.add(i);
      invaliderEp(i); // re-rend + remesure la ligne
    });
    if (liste) liste.addEventListener('keydown', (ev) => lecteur.clavier(ev));

    // (Re)monte la grille pour la largeur courante. Hauteur d'une ligne au repos
    // (uniforme : titre, date et description ont une hauteur reservee) mesuree
    // sur un echantillon reel ; les lignes plus hautes (description depliee,
    // lecture en cours) sont mesurees au montage.
    const monterGrille = () => {
      if (vlActive) { vlActive.detruire(); vlActive = null; }
      cols = colonnesPour(liste.clientWidth);
      let hEstim = 420;
      const ech = document.createElement('div');
      ech.style.cssText = 'visibility:hidden;position:absolute;left:0;right:0';
      rendreLigne(ech, 0); liste.appendChild(ech);
      if (ech.offsetHeight > 0) hEstim = ech.offsetHeight;
      ech.remove();
      vlActive = creerListeVirtuelle({ hote: liste, nombre: Math.ceil(eps.length / cols), hauteurEstimee: hEstim, gap: GAP, rendre: rendreLigne });
    };
    let tResize = 0;
    const surResize = () => {
      clearTimeout(tResize);
      tResize = setTimeout(() => { if (liste && colonnesPour(liste.clientWidth) !== cols) monterGrille(); }, 120);
    };
    if (eps.length) {
      monterGrille();
      window.addEventListener('resize', surResize);
    }
    // Liberation a la fermeture du detail : lecteur coupe, ecouteur retire.
    nettoyageDetail = () => {
      window.removeEventListener('resize', surResize);
      clearTimeout(tResize);
      lecteur.detruire();
    };
    // Ajout de la serie entiere (avancement global + par episode). Ecouteur pose
    // meme si la serie est suivie : rafraichirEtats peut reactiver le bouton.
    const bs = catDetail.querySelector('[data-serie-add]');
    if (bs) bs.dataset.uuid = p.uuid || '';
    if (bs) bs.addEventListener('click', async () => {
      if (serieSuivie(p.uuid)) return;
      bs.disabled = true; bs.classList.add('loading');
      const cat = await categoriePourSerie(p, bs);
      if (cat === null) { bs.disabled = false; bs.classList.remove('loading'); bs.textContent = t('ihm_serie_ajouter'); return; }
      const mac = box().mac;
      // Pendant l'ajout, l'ajout est le seul ecrivain du travail (voir ajoutEnCours).
      debutAjout();
      try {
        if (p.uuid) await TelechargerImageSerie(p.uuid, p.image);
        const catUUID = cat === true ? null : cat;
        const aFaire = eps.filter((e) => { const et = etatUUID(e.uuid); return et !== 'boite' && et !== 'ajout'; });
        // Tout en attente de telechargement d'emblee.
        aFaire.forEach((ep) => majIndicateurEp(ep.uuid, 'attente'));
        // Insertion et persistance par lot, via modifierTravail (ecritures
        // serielles ; la vue et les compteurs suivent a chaque lot).
        const bloques = [];
        let faits = 0;
        const majBtn = () => { bs.textContent = t('ihm_serie_encours', { i: faits, n: aFaire.length }); };
        majBtn();
        for (let d = 0; d < aFaire.length; d += LOT_TELECHARGEMENT) {
          const lot = aFaire.slice(d, d + LOT_TELECHARGEMENT);
          // Telechargements du lot en parallele (la partie lente : reseau). Chaque
          // echec est isole : il ne doit jamais interrompre l'ajout des autres.
          await Promise.all(lot.map(async (ep) => {
            majIndicateurEp(ep.uuid, 'load');
            try {
              const r = await TelechargerEpisode(ep.uuid, ep.audioSrc, ep.image || p.image);
              if (!r.ok) { bloques.push({ titre: ep.title, message: r.message }); majIndicateurEp(ep.uuid, 'err'); ep._echec = true; }
            } catch (e) {
              bloques.push({ titre: ep.title, message: msgErreur(e) }); majIndicateurEp(ep.uuid, 'err'); ep._echec = true;
            }
            faits++; majBtn();
          }));
          // Insertion des episodes telecharges, puis persistance du lot.
          const ok = lot.filter((ep) => !ep._echec);
          if (ok.length) await modifierTravail(mac, (a) => { ok.forEach((ep) => insererEpisode(a, p, ep, catUUID)); });
          ok.forEach((ep) => majIndicateurEp(ep.uuid, 'ajout'));
        }
        await chargerEtats();
        if (bloques.length) afficheMsgCat(t('ihm_serie_bloques', { n: bloques.length }) + ' : ' + bloques.map((b) => b.titre).join(', '));
      } catch (e) { afficheMsgCat(msgErreur(e)); }
      finally { finAjout(); }
      afficherDetailSerie(p, feed, eps);
    });
  }

  q('#srcSeg').addEventListener('click', function (e) {
    const b = e.target.closest('button'); if (!b) return;
    curSrc = b.getAttribute('data-src');
    this.querySelectorAll('button').forEach((x) => x.setAttribute('aria-pressed', x === b));
    rafraichirCatalogue();
  });
  const searchClear = q('#searchClear');
  function majSearchClear() { if (searchClear) searchClear.hidden = !searchInput.value; }
  searchInput.addEventListener('input', () => { majSearchClear(); clearTimeout(searchTimer); searchTimer = setTimeout(rafraichirCatalogue, 400); });
  if (searchClear) searchClear.addEventListener('click', () => { searchInput.value = ''; majSearchClear(); searchInput.focus(); rafraichirCatalogue(); });

  function gotoSeries(s) {
    if (!s) return;
    curSrc = s.src;
    q('#srcSeg').querySelectorAll('button').forEach((x) => x.setAttribute('aria-pressed', x.getAttribute('data-src') === curSrc));
    searchInput.value = s.t;   // le titre de démo devient le terme de recherche réel
    majSearchClear();
    show('catalogue');
    rafraichirCatalogue();
  }

  // ---- Navigation onglets ----
  const tabs = app.querySelectorAll('.tab');
  function show(view) {
    app.querySelectorAll('.view').forEach((v) => v.classList.toggle('active', v.id === view));
    tabs.forEach((tb) => tb.setAttribute('aria-selected', tb.getAttribute('data-view') === view));
    window.scrollTo({ top: 0, behavior: 'auto' });
    // Chargement paresseux du catalogue (nécessite Internet) à la 1re ouverture.
    if (view === 'catalogue' && !catInit) rafraichirCatalogue();
    // Chargement paresseux de l'arbre de la boîte à la 1re ouverture.
    if (view === 'boite') { chargerArbre(); majEtatBoutons(); majPanneauRetrait(); }
    if (view === 'accueil') rafraichirEcoutes(); // lecture locale, sans reseau
    if (view !== 'histoires') lecteurPerso.arreter(); // pas de lecture "fantome" hors de la vue
    // Mes histoires : rechargement a chaque ouverture (liste courte, toujours fraiche).
    if (view === 'histoires') chargerPerso();
  }
  tabs.forEach((tb) => tb.addEventListener('click', () => show(tb.getAttribute('data-view'))));

  // ---- Mes histoires (bibliotheque perso) ----
  let persoBusy = false;
  async function chargerPerso() {
    let liste = [];
    try { liste = (await ListerHistoiresPerso()) || []; } catch (_) { liste = []; }
    try { await chargerEtats(); } catch (_) { /* etats de la boite active, best-effort */ }
    renderPerso(liste);
  }
  // Ligne d'etat vis-a-vis de l'arbre : badge d'etat + bouton d'action sur la
  // MEME ligne (meme motif que les episodes du catalogue, .ep-badge/.ep-del...).
  // Non dans l'arbre -> bouton "Ajouter". Sinon badge + retrait / annulation.
  function ligneArbrePerso(etat) {
    const rem = '<button class="ep-del" data-act="arbre-rem" title="' + escAttr(t('ihm_perso_arbre_rem')) + '" aria-label="' + escAttr(t('ihm_perso_arbre_rem')) + '">' + icone('trash-2') + '</button>';
    const undo = '<button class="ep-undo" data-act="arbre-undo" title="' + escAttr(t('ihm_perso_arbre_undo')) + '" aria-label="' + escAttr(t('ihm_perso_arbre_undo')) + '">' + icone('undo-2') + '</button>';
    const ligne = (inner) => '<span class="ep-ind">' + inner + '</span>';
    if (etat === 'boite') return ligne('<span class="ep-badge surboite">' + t('ihm_etat_boite') + '</span>' + rem);
    if (etat === 'ajout') return ligne('<span class="ep-badge ajout">' + t('ihm_etat_ajout') + '</span>' + rem);
    if (etat === 'suppr') return ligne('<span class="ep-badge suppr">' + t('ihm_etat_suppr') + '</span>' + undo);
    return ligne('<button class="ep-add" data-act="arbre-add">' + t('ihm_add') + '</button>');
  }
  // Cartes "Mes histoires" : meme gabarit que les episodes du catalogue
  // (.ep-card : vignette + bouton lecture + duree + favori, titre, pied d'etat),
  // plus les outils perso (renommer, illustrer, supprimer) au survol.
  let persoListe = [];
  const cartePersoDe = (u) => {
    const w = q('#persoWrap');
    return w ? w.querySelector('.ep-card[data-uuid="' + (window.CSS && CSS.escape ? CSS.escape(u) : u) + '"]') : null;
  };
  const invaliderPerso = (u) => {
    const el = cartePersoDe(u); const i = persoListe.findIndex((x) => x.uuid === u);
    if (el && i >= 0) el.outerHTML = cartePerso(persoListe[i], i);
  };
  // Lecteur unique de la vue (cle = uuid) ; audio servi par /biblioaudio.
  const lecteurPerso = creerLecteur({
    carteDe: cartePersoDe, invalider: invaliderPerso,
    onErreur: () => afficheMsg(t('ihm_player_erreur'), 'err'),
  });
  function dateAjoutPerso(s) {
    if (!s) return '';
    try { return t('ihm_perso_ajoutee', { d: new Date(s * 1000).toLocaleDateString(lang === 'en' ? 'en-GB' : 'fr-FR', { day: 'numeric', month: 'short', year: 'numeric' }) }); } catch (_) { return ''; }
  }
  function cartePerso(h, i) {
    const etat = etatUUID(h.uuid);
    const actif = lecteurPerso.actif(h.uuid);
    const vis = h.img
      ? '<img class="net" loading="lazy" decoding="async" src="' + h.img + '" alt="">'
      : '<span class="ph">🎙️</span>';
    const fond = h.img ? '' : ' style="background:' + GRADS[i % GRADS.length] + '"';
    const fav = estFavori(h.uuid) ? '<span class="ep-fav" title="' + escAttr(t('ihm_fav_titre')) + '">♥</span>' : '';
    const duree = h.duree ? '<span class="ep-duree">' + fmtTemps(h.duree) + '</span>' : '';
    const meta = esc((h.ext || '').toUpperCase() + (h.taille ? ' · ' + fmtOctets(h.taille) : ''));
    return '<article class="ep-card perso' + (actif ? ' joue' : '') + '" data-uuid="' + escAttr(h.uuid) + '">'
      + '<div class="ep-vis-wrap"><div class="ep-vis"' + fond + '>' + vis + fav + duree + '</div>' + lecteurPerso.bouton(h.uuid, true) + '</div>'
      + '<div class="perso-tools">'
        + '<button class="perso-tool" data-act="renom" title="' + t('ihm_perso_renommer') + '" aria-label="' + t('ihm_perso_renommer') + '">' + icone('pencil') + '</button>'
        + '<button class="perso-tool" data-act="illus" title="' + t('ihm_perso_illustrer') + '" aria-label="' + t('ihm_perso_illustrer') + '">' + icone('image') + '</button>'
        + '<button class="perso-tool" data-act="suppr"' + (h.referencee ? ' disabled' : '') + ' title="' + (h.referencee ? t('ihm_perso_suppr_ref') : t('ihm_perso_suppr')) + '" aria-label="' + t('ihm_perso_suppr') + '">' + icone('trash-2') + '</button>'
      + '</div>'
      + '<div class="ep-txt"><b>' + esc(h.titre) + '</b><span class="ep-date">' + esc(dateAjoutPerso(h.ajout)) + '</span>'
      + '<span class="ep-meta">' + meta + '</span></div>'
      + lecteurPerso.progres(h.uuid, h.duree)
      + '<div class="ep-pied">' + ligneArbrePerso(etat) + '</div></article>';
  }
  function renderPerso(liste) {
    const wrap = q('#persoWrap'); if (!wrap) return;
    persoListe = liste;
    const imp = '<button class="dropzone" id="importBtn"' + (persoBusy ? ' disabled' : '') + '><span class="di">＋</span><span>'
      + t('ihm_perso_import') + '</span><small>' + t('ihm_perso_import_sub') + '</small></button>';
    wrap.innerHTML = '<div class="perso-grille">' + imp + liste.map(cartePerso).join('') + '</div>';
    const ib = wrap.querySelector('#importBtn');
    if (ib) { ib.addEventListener('click', importerPerso); brancherFeedbackDrop(ib); }
    if (!wrap.dataset.delegue) { // un seul jeu d'ecouteurs : les cartes sont re-rendues
      wrap.dataset.delegue = '1';
      wrap.addEventListener('click', (ev) => {
        const c = ev.target.closest('.ep-card'); if (!c) return;
        const uuid = c.getAttribute('data-uuid');
        const h = persoListe.find((x) => x.uuid === uuid); if (!h) return;
        const act = ev.target.closest('[data-act]');
        if (act) {
          ev.stopPropagation();
          const a = act.getAttribute('data-act');
          if (a === 'renom') renommerPerso(h, c);
          else if (a === 'illus') illustrerPerso(h);
          else if (a === 'suppr') supprimerPerso(h);
          else actionArbrePerso(h, a, act);
          return;
        }
        if (ev.target.closest('.ep-play')) { lecteurPerso.basculer(uuid, 'biblioaudio/' + encodeURIComponent(uuid)); return; }
        const bar = ev.target.closest('.track-bar');
        if (bar) lecteurPerso.chercher(bar, ev.clientX);
      });
      wrap.addEventListener('keydown', (ev) => lecteurPerso.clavier(ev));
    }
  }
  // Renommage inline : le titre devient un champ, valide sur Entree / perte de focus.
  function renommerPerso(h, el) {
    if (!h || !el) return;
    const body = el.querySelector('.ep-txt'); const h3 = body && body.querySelector('b');
    if (!h3 || body.querySelector('.perso-rename')) return;
    const inp = document.createElement('input');
    inp.className = 'perso-rename'; inp.type = 'text'; inp.value = h.titre || ''; inp.maxLength = 63;
    h3.replaceWith(inp); inp.focus(); inp.select();
    let fini = false;
    const valider = async (annuler) => {
      if (fini) return; fini = true;
      const val = (inp.value || '').trim();
      if (!annuler && val && val !== h.titre) { try { await RenommerHistoirePerso(h.uuid, val); } catch (_) {} }
      await chargerPerso();
    };
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); valider(false); }
      else if (e.key === 'Escape') { e.preventDefault(); valider(true); }
    });
    inp.addEventListener('blur', () => valider(false));
  }
  async function importerPerso() {
    if (persoBusy) return;
    persoBusy = true; renderPerso(await safePerso()); // reflete l'etat desactive
    try {
      lecteurPerso.arreter(); // un re-import ecrit peut-etre le fichier en cours de lecture
      await ImporterHistoirePerso(); // selection multiple : importe tous les fichiers
    } catch (e) {
      persoBusy = false;
      await chargerPerso();
      alert(t('ihm_perso_import_err') + '\n' + msgErreur(e));
      return;
    }
    persoBusy = false;
    await chargerPerso();
  }
  async function safePerso() { try { return (await ListerHistoiresPerso()) || []; } catch (_) { return []; } }
  async function supprimerPerso(h) {
    if (!h || h.referencee) return; // bouton grise ; garde-fou cote backend aussi
    lecteurPerso.arreter(); // le fichier lu ne doit pas etre tenu ouvert (Windows)
    try { await SupprimerHistoirePerso(h.uuid); } catch (e) { afficheMsg(msgErreur(e), 'err'); }
    await chargerPerso();
  }
  async function illustrerPerso(h) {
    if (!h) return;
    const res = await ouvrirEditeurIllustration({ nom: h.titre, valeur: {}, hote: q('.ihm-root') || app });
    if (!res) return;
    try { await DefinirIllustrationPerso(h.uuid, res.image); } catch (_) {}
    await chargerPerso();
  }
  // Un audio pousse a la boite exige une illustration : si l'histoire n'en a pas
  // (ni cover ni picto), on en genere une par defaut avant l'ajout a l'arbre.
  async function assurerIllustrationPerso(h) {
    if (h.img) return;
    try {
      const url = await genererIllustration({ nom: h.titre, icone: ICONE_DEFAUT, couleur: COULEUR_DEFAUT });
      await DefinirIllustrationPerso(h.uuid, url);
    } catch (_) { /* le picto par defaut est un confort, pas un bloquant */ }
  }
  function actionArbrePerso(h, act, anchorEl) {
    if (act === 'arbre-add') return ajouterPersoArbre(h, anchorEl);
    if (act === 'arbre-rem') return retirerPersoArbre(h.uuid).then(chargerPerso);
    if (act === 'arbre-undo') return annulerSuppression(h.uuid).then(chargerPerso);
  }
  // Insere l'histoire perso comme feuille audio DIRECTEMENT sous une categorie
  // choisie (comme le catalogue demande sa categorie). Le media est deja en
  // bibliotheque : aucun telechargement.
  async function ajouterPersoArbre(h, anchorEl) {
    if (!h) return;
    const mac = box().mac; if (!mac) return;
    const cats = await categoriesBoite(mac);
    if (!cats.length) return;
    const catUUID = await choisirCategorie(cats, anchorEl);
    if (!catUUID) return; // annule
    await assurerIllustrationPerso(h);
    const fait = await modifierTravail(mac, (a) => {
      const cat = a.find((c) => c.uuid === catUUID);
      if (!cat) return false;
      if (!Array.isArray(cat.child)) cat.child = [];
      if (!cat.child.some((x) => x.uuid === h.uuid)) {
        cat.child.push({ uuid: h.uuid, title: h.titre, add_time: Math.floor(Date.now() / 1000), _new: true });
      }
    });
    if (!fait) return;
    await chargerPerso();
  }
  // Retrait d'une feuille perso (1 niveau : directement sous une categorie).
  // _new -> retrait immediat ; deja sur la boite -> marquee _rem (a supprimer).
  async function retirerPersoArbre(uuid) {
    const mac = box().mac; if (!mac) return false;
    return modifierTravail(mac, (a) => {
      let fait = false;
      for (let ci = 0; ci < a.length && !fait; ci++) {
        const cat = a[ci];
        if (!Array.isArray(cat.child)) continue;
        const li = cat.child.findIndex((n) => n.uuid === uuid && !Array.isArray(n.child));
        if (li < 0) continue;
        if (cat.child[li]._new || cat._new) cat.child.splice(li, 1);
        else cat.child[li]._rem = true;
        fait = true;
      }
      return fait;
    });
  }
  // Fichiers laches sur la zone perso (drop natif Wails) : le backend a importe,
  // on rafraichit la liste ; on signale seulement un echec total.
  EventsOn('perso:depose', (p) => {
    chargerPerso();
    if (p && p.erreur && !(p.importes > 0)) alert(t('ihm_perso_import_err') + '\n' + p.erreur);
  });
  // Retour visuel du survol d'un fichier, limite au bloc d'import (la zone de
  // depot reelle). Compteur enter/leave pour ignorer le bouillonnement sur les
  // enfants du bouton. La dropzone etant recreee a chaque rendu, on rebranche.
  function brancherFeedbackDrop(zone) {
    let n = 0;
    zone.addEventListener('dragenter', (e) => { e.preventDefault(); n++; zone.classList.add('drop-actif'); });
    zone.addEventListener('dragover', (e) => { e.preventDefault(); });
    zone.addEventListener('dragleave', () => { n = Math.max(0, n - 1); if (!n) zone.classList.remove('drop-actif'); });
    zone.addEventListener('drop', (e) => { e.preventDefault(); n = 0; zone.classList.remove('drop-actif'); });
  }

  // ---- Synchro (retour visuel seulement, pas de metier) ----
  const syncMsg = q('#syncMsg');
  function afficheMsg(texte, type) {
    if (!syncMsg) return;
    if (!texte) { syncMsg.hidden = true; syncMsg.textContent = ''; return; }
    syncMsg.hidden = false; syncMsg.textContent = texte;
    syncMsg.className = 'sync-msg' + (type ? ' ' + type : '');
  }
  // Barre de progression de la synchro (avancement reel par octets).
  // m (optionnel) : evenement sync:progres complet (eta_secondes, faits, total,
  // octets_*) pour le detail et le temps restant estime.
  // racine : prefixe des identifiants du bloc de progression ("syncProg" dans le
  // panneau Boite, "retraitProg" dans la fenetre de restauration).
  function majProgres(libelle, pct, m, racine = 'syncProg') {
    const prog = q('#' + racine); if (!prog) return;
    prog.hidden = false;
    const p = Math.max(0, Math.min(100, Math.round(pct || 0)));
    const fill = q('#' + racine + 'Fill'); if (fill) fill.style.width = p + '%';
    const lab = q('#' + racine + 'Label'); if (lab) lab.textContent = libelle ? t(libelle) : ''; // cle i18n envoyee par le Go
    const pc = q('#' + racine + 'Pct'); if (pc) pc.textContent = p + '%';
    const det = q('#' + racine + 'Detail'), eta = q('#' + racine + 'Eta');
    if (det) det.textContent = (m && m.total > 0)
      ? t('sauv_prog', { faits: m.faits || 0, total: m.total, vol: fmtOctets(m.octets_faits) + ' / ' + fmtOctets(m.octets_total) })
      : '';
    if (eta) eta.textContent = (m && m.eta_secondes >= 0 && p < 100) ? t('sauv_eta', { eta: fmtDuree(m.eta_secondes) }) : '';
  }
  function cacherProgres() { const prog = q('#syncProg'); if (prog) prog.hidden = true; }

  q('#syncBtn').addEventListener('click', async function () {
    const btn = this;
    const b = box();
    syncEnCours = true; majEtatBoutons();
    let libererSync;
    finSync = new Promise((r) => { libererSync = r; });
    app.classList.add('sync-lock'); // ajouts / retraits du catalogue et de Mes histoires figes
    btn.textContent = t('ihm_boite_sync_ing');
    afficheMsg('', '');
    majProgres(t('ihm_sync_prepa'), 0);
    q('#boxCats').classList.add('tree-lock'); // pas d'edition de l'arbre pendant la synchro
    pausePoll(); // le ping recurrent doit s'arreter pendant la synchro (socket mono-usage)
    const off = EventsOn('sync:progres', (m) => { if (m) majProgres(m.libelle, m.pct, m); });
    try {
      await PublierEditions(b.mac);
      majProgres(t('ihm_sync_done'), 100);
      btn.textContent = t('ihm_boite_sync_done');
      afficheMsg(t('ihm_sync_done'), 'ok');
      arbreMac = null; chargerArbre(true); // le travail est soldé : on recharge l'arbre publié
      rechargerStockageRegistre(b); // jauge : espace releve en fin de publication
      await rafraichirEtats(); // maj tags catalogue/perso ("A synchroniser" -> "Sur la boite")
    } catch (e) {
      cacherProgres();
      btn.textContent = '✗ ' + t('ihm_boite_sync');
      afficheMsg(msgErreur(e), 'err');
    } finally {
      off();
      q('#boxCats').classList.remove('tree-lock');
      app.classList.remove('sync-lock');
      syncEnCours = false;
      finSync = null; libererSync();
      resumePoll();
      setTimeout(() => { cacherProgres(); btn.textContent = t('ihm_boite_sync'); majEtatBoutons(); }, 2500);
    }
  });
  function resumePoll() { demarrerPoll(); }
  // Pendant la synchro, aucune edition du travail : la publication l'a lu au
  // debut et le soldera a la fin. Les boutons d'edition du catalogue et de Mes
  // histoires sont interceptes (capture) avec une explication.
  const SELECTEUR_EDITION = '.ep-add, .ep-del, .ep-undo, [data-serie-add]';
  app.addEventListener('click', (ev) => {
    if (!syncEnCours) return;
    const el = ev.target.closest(SELECTEUR_EDITION);
    if (!el) return;
    ev.preventDefault(); ev.stopPropagation();
    if (!catDetail.hidden && catDetail.contains(el)) afficheMsgCat(t('ihm_sync_verrou'));
    else alert(t('ihm_sync_verrou'));
  }, true);

  // ---- Options de la boîte : Wi-Fi station (sta_wifi.json). ----
  const wifiModal = q('#wifiModal');
  const wifiMsgEl = q('#wifiMsg');
  function wifiMsg(txt, type) {
    if (!wifiMsgEl) return;
    wifiMsgEl.textContent = txt || '';
    wifiMsgEl.className = 'sync-msg' + (type ? ' ' + type : '');
    wifiMsgEl.hidden = !txt;
  }
  function ouvrirWifi() {
    const b = box(); if (!b || !b.mac) return;
    wifiMsg('', '');
    q('#wifiSsid').value = '';
    q('#wifiPwd').value = '';
    wifiModal.hidden = false;
    q('#wifiSsid').focus();
  }
  function fermerWifi() { wifiModal.hidden = true; }
  const radiosBtn = q('#radiosBtn');
  if (radiosBtn) radiosBtn.addEventListener('click', () => { if (vueArbre.ajouterRadios) vueArbre.ajouterRadios(radiosBtn); });
  const optWifiBtn = q('#optWifi');
  if (optWifiBtn) optWifiBtn.addEventListener('click', ouvrirWifi);
  q('#wifiCancel').addEventListener('click', fermerWifi);
  wifiModal.addEventListener('mousedown', (e) => { if (e.target === wifiModal) fermerWifi(); });
  q('#wifiSave').addEventListener('click', async function () {
    const b = box(); if (!b || !b.mac) return;
    const ssid = q('#wifiSsid').value.trim();
    const pwd = q('#wifiPwd').value;
    if (!ssid) { wifiMsg(t('ihm_wifi_ssid'), 'err'); q('#wifiSsid').focus(); return; }
    const save = this, cancel = q('#wifiCancel');
    save.disabled = true; cancel.disabled = true;
    const txt = save.textContent; save.textContent = t('ihm_wifi_envoi');
    wifiMsg('', '');
    pausePoll();
    try {
      await DefinirWifi(b.mac, ssid, pwd);
      b.wifiMaison = ssid; majPanneauRadio(); // Wi-Fi domestique pose : radios possibles
      wifiMsg(t('ihm_wifi_titre') + ' ✓', 'ok');
      setTimeout(fermerWifi, 1200);
    } catch (e) {
      wifiMsg(msgErreur(e), 'err');
    } finally {
      save.disabled = false; cancel.disabled = false; save.textContent = txt;
      resumePoll();
    }
  });

  // ---- Options de la boîte : plages horaires (set_enabling_hours.cmd). ----
  // Ordre d'affichage lundi-first ; index firmware (lc6) : Dim=0 .. Sam=6.
  const HOR_JOURS = [[1, 'lun'], [2, 'mar'], [3, 'mer'], [4, 'jeu'], [5, 'ven'], [6, 'sam'], [0, 'dim']];
  const HOR_MAX = 23 * 60 + 59; // 23:59 en minutes
  const horModal = q('#horModal');
  const horBody = q('#horBody');
  const horMsgEl = q('#horMsg');
  const fmtHM = (m) => String((m / 60) | 0).padStart(2, '0') + ':' + String(m % 60).padStart(2, '0');
  function horMsg(txt, type) {
    if (!horMsgEl) return;
    horMsgEl.textContent = txt || '';
    horMsgEl.className = 'sync-msg' + (type ? ' ' + type : '');
    horMsgEl.hidden = !txt;
  }
  // Double slider pour une ligne jour. Renvoie une fonction lire() -> {debutMin, finMin}.
  function brancherDoubleSlider(row) {
    const lo = row.querySelector('.ds-lo');
    const hi = row.querySelector('.ds-hi');
    const fill = row.querySelector('.ds-fill');
    const val = row.querySelector('.hor-val');
    const maj = () => {
      let a = +lo.value, b = +hi.value;
      if (a > b) { if (document.activeElement === lo) b = a, hi.value = b; else a = b, lo.value = a; }
      lo.style.zIndex = a >= HOR_MAX - 1 ? 5 : 3; // rester attrapable au bord droit
      const pa = (a / HOR_MAX) * 100, pb = (b / HOR_MAX) * 100;
      fill.style.left = pa + '%';
      fill.style.width = (pb - pa) + '%';
      val.textContent = (b <= a) ? t('ihm_hor_desactive') : (fmtHM(a) + ' - ' + fmtHM(b));
    };
    lo.addEventListener('input', maj);
    hi.addEventListener('input', maj);
    maj();
    return () => ({ debutMin: +lo.value, finMin: +hi.value });
  }
  let horLire = []; // lecteurs par ligne, dans l'ordre HOR_JOURS
  function construireHoraires(hor) {
    const jours = (hor && hor.jours) || [];
    horBody.innerHTML = HOR_JOURS.map(([idx, cle]) => {
      const p = jours[idx] || { debutMin: 6 * 60, finMin: 20 * 60 };
      return '<div class="hor-row" data-idx="' + idx + '">'
        + '<span class="hor-nom">' + esc(t('ihm_hor_' + cle)) + '</span>'
        + '<div class="dslider">'
        + '<div class="ds-track"></div><div class="ds-fill"></div>'
        + '<input type="range" class="ds-lo" min="0" max="' + HOR_MAX + '" step="5" value="' + (p.debutMin | 0) + '">'
        + '<input type="range" class="ds-hi" min="0" max="' + HOR_MAX + '" step="5" value="' + (p.finMin | 0) + '">'
        + '</div><span class="hor-val"></span></div>';
    }).join('');
    horLire = HOR_JOURS.map(([idx], i) => {
      const row = horBody.querySelectorAll('.hor-row')[i];
      return { idx, lire: brancherDoubleSlider(row) };
    });
  }
  function ouvrirHoraires() {
    const b = box(); if (!b || !b.mac) return;
    horMsg('', '');
    construireHoraires(b.horaires);
    q('#horNePasCouper').checked = !!(b.horaires && b.horaires.nePasCouper);
    horModal.hidden = false;
  }
  function fermerHoraires() { horModal.hidden = true; }
  // Lecture des plages au passage deconnecte -> connecte (pas a chaque sonde).
  async function chargerHorairesBoite(b) {
    try { const h = await LireHoraires(b.mac); if (h) b.horaires = h; } catch (_) { /* reessaie au prochain passage */ }
  }
  // Stockage releve au passage deconnecte -> connecte (et persiste cote Go) :
  // la jauge suit ce qui a change sur la carte, y compris par l'app officielle.
  async function chargerStockageBoite(b) {
    try { appliquerStockage(b, await LireStockage(b.mac)); } catch (_) { /* reessaie au prochain passage */ }
  }
  function appliquerStockage(b, st) {
    if (!b || !st || !st.total) return;
    Object.assign(b, jaugeStockage(st.total, st.libre));
    if (b === box()) { renderBoxControl(); renderContext(); }
  }
  // Apres une publication : le registre porte le stockage releve juste avant
  // EndSync (la boite a quitte l'AP, on ne peut plus l'interroger).
  async function rechargerStockageRegistre(b) {
    try {
      const m = ((await Boites()) || []).find((x) => x.mac === b.mac);
      if (m) appliquerStockage(b, { total: m.stockage_total, libre: m.stockage_libre });
    } catch (_) { /* jauge inchangee */ }
  }
  // Lecture des favoris (fav.bin, lecture seule) au passage deconnecte -> connecte.
  // Met a jour l'etat ; les etoiles apparaissent au (re)rendu des episodes et de
  // l'arbre, qui suit le passage connecte. Le fichier favoris.json est persiste
  // cote Go, donc chargerEtats() le relira aussi au chargement du catalogue.
  async function chargerFavorisBoite(b) {
    // LireFavoris renvoie null si la connexion est occupee (sonde en cours) :
    // quelques nouvelles tentatives espacees avant d'attendre le prochain passage.
    for (let essai = 0; essai < 4; essai++) {
      try {
        const fav = await LireFavoris(b.mac);
        if (fav) {
          if (b === box()) { etatsCat.fav = new Set(fav); if (vueArbre.majFavoris) vueArbre.majFavoris(etatsCat.fav); }
          return;
        }
      } catch (_) { /* reessaie */ }
      await new Promise((r) => setTimeout(r, 1500));
    }
  }
  // Panneau "Retirer la boite" : conditionnel a l'existence d'une sauvegarde
  // complete. Sans sauvegarde, pas de bouton : renvoi vers l'app officielle.
  async function majPanneauRetrait() {
    const b = box(); const sub = q('#retirerSub'), btn = q('#restaurerBtn');
    if (!b || !sub || !btn) return;
    let e = null;
    try { e = await EtatSauvegarde(b.mac); } catch (_) { /* considere absente */ }
    if (b !== box()) return; // la boite active a change pendant l'appel
    if (e && e.disponible) {
      const vol = fmtOctets(e.octets);
      sub.textContent = e.date
        ? t('ihm_boite_retirer_ok', { date: dateLongue(e.date), n: e.fichiers, vol })
        : t('ihm_boite_retirer_ok_sansdate', { n: e.fichiers, vol });
      btn.dataset.mode = 'restaurer';
      btn.textContent = t('ihm_boite_restaurer');
    } else {
      sub.textContent = t('ihm_boite_retirer_non');
      btn.dataset.mode = 'supprimer';
      btn.textContent = t('ihm_boite_supprimer');
    }
    btn.hidden = false;
    majEtatBoutons();
    const m = q('#restaurerMsg'); if (m) m.hidden = true;
  }
  function dateLongue(ts) {
    try { return new Date(ts * 1000).toLocaleDateString(lang === 'en' ? 'en-GB' : 'fr-FR', { day: 'numeric', month: 'long', year: 'numeric' }); } catch (_) { return ''; }
  }
  // Retrait de la boite. Avec sauvegarde : "Supprimer et restaurer" (boite en AP
  // requise) remet la boite dans l'etat de sa sauvegarde puis la retire de
  // Merlinus en GARDANT la sauvegarde. Sans sauvegarde : "Supprimer" la retire
  // seulement. Confirmation forte (facon GitHub) : il faut taper le mot exact
  // ("RESTAURER" ou "SUPPRIMER") pour activer le bouton de validation.
  const restaurerBtn = q('#restaurerBtn');
  function msgRetrait(txt, type) {
    const m = q('#restaurerMsg'); if (!m) return;
    m.hidden = !txt; m.className = 'sync-msg' + (type ? ' ' + type : ''); m.textContent = txt || '';
  }
  // Ouvre la fenetre de confirmation ; resout true si le mot a ete tape et valide.
  // Fenetre de retrait, en trois temps : confirmation (mot a taper), avancement
  // (restauration), resultat. modeRetrait bascule l'affichage.
  function modeRetrait(mode) {
    const conf = mode === 'confirmation';
    q('#retraitConsigne').hidden = !conf;
    q('#retraitActions').hidden = !conf;
    q('#retraitProg').hidden = mode !== 'avancement' && mode !== 'fin';
    q('#retraitFin').hidden = mode !== 'fin';
    if (conf) { const m = q('#retraitMsg'); m.hidden = true; m.textContent = ''; }
  }
  function msgFenetreRetrait(txt, type) {
    const m = q('#retraitMsg'); m.hidden = !txt; m.className = 'sync-msg' + (type ? ' ' + type : ''); m.textContent = txt || '';
  }
  // Ouvre la fenetre ; resout true si le mot a ete tape et valide. La fenetre
  // reste OUVERTE apres validation (elle affiche ensuite l'avancement).
  function confirmerRetrait(restaurer, nomBoite) {
    const modal = q('#retraitModal'), saisie = q('#retraitSaisie'), ok = q('#retraitValider'), ann = q('#retraitAnnuler');
    const mot = t(restaurer ? 'ihm_retrait_mot_restaurer' : 'ihm_retrait_mot_supprimer');
    q('#retraitTitre').textContent = t(restaurer ? 'ihm_retrait_titre_restaurer' : 'ihm_retrait_titre_supprimer', { name: nomBoite });
    q('#retraitTexte').textContent = t(restaurer ? 'ihm_retrait_texte_restaurer' : 'ihm_retrait_texte_supprimer', { name: nomBoite });
    q('#retraitConsigne').firstChild.textContent = t('ihm_retrait_consigne', { mot });
    ok.textContent = t(restaurer ? 'ihm_boite_restaurer' : 'ihm_boite_supprimer');
    modeRetrait('confirmation');
    saisie.value = ''; ok.disabled = true; modal.hidden = false; saisie.focus();
    return new Promise((fin) => {
      const verifier = () => { ok.disabled = saisie.value.trim() !== mot; };
      const clore = (r) => {
        if (!r) modal.hidden = true;
        saisie.removeEventListener('input', verifier); saisie.removeEventListener('keydown', clavier);
        ok.removeEventListener('click', valider); ann.removeEventListener('click', annuler);
        fin(r);
      };
      const valider = () => { if (!ok.disabled) clore(true); };
      const annuler = () => clore(false);
      const clavier = (e) => { if (e.key === 'Enter') valider(); else if (e.key === 'Escape') annuler(); };
      saisie.addEventListener('input', verifier); saisie.addEventListener('keydown', clavier);
      ok.addEventListener('click', valider); ann.addEventListener('click', annuler);
    });
  }
  // Fin de restauration : bouton "Fermer" (rechargement si la boite a ete retiree).
  function attendreFermeture() {
    return new Promise((fin) => {
      const f = q('#retraitFermer');
      const clic = () => { f.removeEventListener('click', clic); q('#retraitModal').hidden = true; fin(); };
      f.addEventListener('click', clic); f.focus();
    });
  }
  if (restaurerBtn) restaurerBtn.addEventListener('click', async () => {
    const b = box(); if (!b || !b.mac) return;
    const restaurer = restaurerBtn.dataset.mode === 'restaurer';
    if (!(await confirmerRetrait(restaurer, b.name))) return;
    msgRetrait('', '');
    if (!restaurer) {
      try { await SupprimerBoite(b.mac); } catch (e) { q('#retraitModal').hidden = true; msgRetrait(msgErreur(e), 'err'); return; }
      location.reload(); // registre relu : boite suivante, ou ecran d'accueil
      return;
    }
    // Avancement dans la fenetre (comme la sauvegarde de l'association) : pas
    // d'annulation possible, la boite doit rester connectee jusqu'au bout.
    q('#retraitTitre').textContent = t('ihm_retrait_encours_titre', { name: b.name });
    q('#retraitTexte').textContent = t('ihm_retrait_encours_texte');
    modeRetrait('avancement');
    majProgres(t('ihm_sync_prepa'), 0, null, 'retraitProg');
    syncEnCours = true; majEtatBoutons();
    pausePoll(); // socket mono-usage : pas de sonde pendant la restauration
    const off = EventsOn('sync:progres', (m) => { if (m) majProgres(m.libelle, m.pct, m, 'retraitProg'); });
    let retiree = false;
    try {
      await RestaurerBoite(b.mac);
      await RetirerBoite(b.mac); // donnees de restauration conservees (dossier date)
      retiree = true;
      majProgres(t('ihm_retrait_fini_label'), 100, null, 'retraitProg');
      q('#retraitTitre').textContent = t('ihm_retrait_fini_titre', { name: b.name });
      q('#retraitTexte').textContent = t('ihm_retrait_fini_texte');
      msgFenetreRetrait(t('ihm_boite_restaurer_ok'), 'ok');
    } catch (e) {
      q('#retraitTitre').textContent = t('ihm_retrait_echec_titre');
      q('#retraitTexte').textContent = '';
      msgFenetreRetrait(msgErreur(e), 'err');
    } finally {
      off();
    }
    modeRetrait('fin');
    await attendreFermeture();
    if (retiree) { location.reload(); return; }
    syncEnCours = false; majEtatBoutons();
    resumePoll();
  });
  const optHorBtn = q('#optHoraires');
  if (optHorBtn) optHorBtn.addEventListener('click', ouvrirHoraires);
  q('#horCancel').addEventListener('click', fermerHoraires);
  horModal.addEventListener('mousedown', (e) => { if (e.target === horModal) fermerHoraires(); });
  q('#horSave').addEventListener('click', async function () {
    const b = box(); if (!b || !b.mac) return;
    const jours = new Array(7).fill(null).map(() => ({ debutMin: 0, finMin: 0 }));
    horLire.forEach((r) => { jours[r.idx] = r.lire(); });
    const h = { jours, nePasCouper: q('#horNePasCouper').checked };
    const save = this, cancel = q('#horCancel');
    save.disabled = true; cancel.disabled = true;
    const txt = save.textContent; save.textContent = t('ihm_hor_envoi');
    horMsg('', '');
    pausePoll();
    try {
      await DefinirHoraires(b.mac, h);
      b.horaires = h;
      horMsg(t('ihm_hor_titre') + ' ✓', 'ok');
      setTimeout(fermerHoraires, 1000);
    } catch (e) {
      horMsg(msgErreur(e), 'err');
    } finally {
      save.disabled = false; cancel.disabled = false; save.textContent = txt;
      resumePoll();
    }
  });

  // ---- Modale d'ajout de boîte : rejoue le VRAI flux d'onboarding ----
  // Réutilise DemarrerVeille + les évènements boite:* du Welcome. La boîte lue
  // est persistée par MAC côté Go ; on la nomme puis on recharge le registre.
  const modal = q('#modal'), onbInner = q('#onbInner');
  let decModal = null;
  function openModal() {
    modal.hidden = false;
    // Le composant rend toute la carte (creerDecouverte) : l'hote n'est qu'un point d'accroche.
    onbInner.innerHTML = '<div id="decHostModal"></div>';
    decModal = creerDecouverte({
      host: q('#decHostModal'), mode: 'add', nomDefaut: t('ihm_nom_exemple'),
      onTermine: (p) => finaliserAjout(p),
    });
  }
  function closeModal() {
    if (decModal) { try { decModal.detruire(); } catch (_) {} decModal = null; }
    modal.hidden = true;
  }
  async function finaliserAjout(p) {
    const cle = p.mac || p.ssid || '';
    const nm = (p.__nom || '').trim() || t('ihm_add_default_name', { x: (p.ssid || '').slice(-4) });
    if (cle) { try { await NommerBoite(cle, nm); } catch (_) {} }
    // La boite a ete persistee par la veille : on recharge le registre Go.
    try { boites = (await Boites()).map(construireBoite); } catch (_) {}
    const i = boites.findIndex((b) => b.mac === cle);
    if (i >= 0) active = i;
    closeModal(); renderBoxControl(); renderContext(); rafraichirEspaceAjouts(); rafraichirAccueil(); majPanneauRetrait();
    arbreMac = null; show('boite');
  }

  // Fermeture (clic exterieur / Echap) permise seulement tant que la lecture de
  // la boite n'a pas commence ; une fois lancee, on force l'aller jusqu'au bout.
  const peutFermer = () => !decModal || decModal.estAnnulable();
  modal.addEventListener('click', (e) => { if (e.target === modal && peutFermer()) closeModal(); });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !modal.hidden && peutFermer()) closeModal(); });

  // ---- Sonde de connexion (lecture seule) ----
  // Interroge périodiquement la boîte en AP ; met à jour l'indicateur de
  // connexion et le niveau de charge de la boîte reconnue (par MAC).
  async function pollConnexion() {
    let etat;
    try { etat = await EtatBoite(); } catch (_) { etat = { joignable: false }; }
    let changed = false;
    const macEq = (x, y) => x && y && String(x).toLowerCase() === String(y).toLowerCase();
    boites.forEach((b) => {
      const was = b.connected;
      const now = !!(etat && etat.joignable && macEq(etat.mac, b.mac));
      if (b.connected !== now) { b.connected = now; changed = true; }
      if (now && etat.batterie && b.batt !== etat.batterie.niveau) { b.batt = etat.batterie.niveau; changed = true; }
      // Transition deconnecte -> connecte : lectures SEQUENTIELLES. Lancees en
      // parallele, horaires et favoris se disputaient le verrou de connexion
      // (TryLock) et le perdant abandonnait : les favoris n'etaient jamais lus.
      if (now && !was) { (async () => { await chargerHorairesBoite(b); await chargerStockageBoite(b); await chargerFavorisBoite(b); await chargerEcoutesBoite(b); })(); }
    });
    // Repli best-effort UNIQUEMENT si le MAC est indisponible (op30 muet) : on
    // marque l'active connectée. Si le MAC est present mais ne correspond a
    // aucune boite, c'est une AUTRE boite -> on ne deverrouille pas (le backend
    // refuserait de toute facon d'ecrire la mauvaise boite).
    if (etat && etat.joignable && !etat.mac && !boites.some((b) => b.connected)) {
      const b = box();
      if (!b.connected) { b.connected = true; changed = true; }
      if (etat.batterie) b.batt = etat.batterie.niveau;
    }
    // Boite occupee (une autre operation tient la connexion) : on le signale sur
    // la boite active, sans la marquer "connectee" (cf. N5).
    const occupe = !!(etat && etat.occupee);
    boites.forEach((b) => {
      const o = occupe && b === box() && !b.connected;
      if (b.occupee !== o) { b.occupee = o; changed = true; }
    });
    // On ne re-rend pas pendant l'édition du nom (ne pas casser la saisie).
    if (changed && !editing) {
      const pop = boxCtl.querySelector('.pop[data-popfor="c"]');
      renderBoxControl(pop && !pop.hasAttribute('hidden'));
    }
    // connected/batt sont volatils : non persistes (recalcules a chaque sonde).
    majEtatBoutons();
  }
  let pollTimer = null;
  function demarrerPoll() { if (!pollTimer) pollTimer = setInterval(pollConnexion, 4000); }
  function pausePoll() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }
  demarrerPoll();
  pollConnexion();

  // ---- Init ----
  renderBoxControl();
  renderContext();
  majEtatBoutons();
  rafraichirEspaceAjouts();
  rafraichirAccueil();
  // Arbre de la boite active charge des le demarrage : sans lui, le bloc
  // haut-droite affichait le compte de l'appairage (rapport du registre) et
  // 0 ajout / 0 retrait jusqu'a la 1re ouverture de l'onglet Boite.
  if (box() && box().mac) chargerArbre();
  majPanneauRadio();
  // Le catalogue se charge à la 1re ouverture de son onglet (voir show()).
}
