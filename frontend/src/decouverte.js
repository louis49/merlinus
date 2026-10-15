// Composant de decouverte d'une boite, PARTAGE entre l'ecran d'accueil (mode
// 'welcome', premiere boite) et le modal d'ajout (mode 'add', boite suivante).
//
// Le composant rend la CARTE COMPLETE (icone + marque + sous-titre + flot) avec
// sa CSS dediee (decouverte.css). L'hote ne fournit que le fond (etoiles du
// Welcome, voile du modal) et le centrage : le rendu est donc strictement
// identique dans les deux contextes.
//
// opts : host, mode ('welcome'|'add'), nomDefaut, onTermine(pret) (pret.__nom =
//        nom saisi), onAnnuler() (mode add : fermeture).

import './decouverte.css';
import iconeUrl from './assets/merlinus.svg';
import { DemarrerVeille, ArreterVeille, DefinirSauvegardeInitiale } from '../wailsjs/go/app/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { t } from './i18n.js';
import { esc, msgErreur } from './composants.js';
import { fmtOctets, fmtDuree } from './format.js';

// Compte rendu de la boite lue (nb histoires, firmware, stockage, SSID, MAC).
function rapportHTML(p) {
  const r = (p && p.rapport) || {};
  const infos = [[t('ihm_label_histoires'), r.histoires || 0]];
  if (p.firmware) infos.push([t('label_firmware'), p.firmware]);
  if (p.stockage_total) {
    const used = Math.round((p.stockage_total - (p.stockage_libre || 0)) / 1048576);
    infos.push([t('label_stockage'), used + ' / ' + Math.round(p.stockage_total / 1048576) + ' MB']);
  }
  if (p.ssid) infos.push([t('label_ssid'), p.ssid]);
  if (p.mac) infos.push([t('label_mac'), p.mac]);
  return '<div class="dec-report">'
    + infos.map(([k, v]) => '<div class="dec-meta"><span>' + esc(k) + '</span><b>' + esc(v) + '</b></div>').join('')
    + '</div>';
}

export function creerDecouverte(opts) {
  const host = opts.host;
  const mode = opts.mode || 'welcome';
  let phase = 'search';   // search | read | ready
  let pret = null, images = null, inventaire = null, sauvegarde = null, erreur = null;
  // Option "sauvegarder tout le contenu" : cochee par defaut, etat conserve entre
  // les re-rendus de l'ecran d'attente (sinon un boite:attente la reinitialiserait).
  let avecSauvegarde = true;
  const unsub = [];

  function barreLecture() {
    // Sauvegarde complete en cours : barre dediee (pourcentage sur les octets,
    // volume, titre du contenu courant, temps restant estime).
    if (sauvegarde) {
      const s = sauvegarde;
      const pct = Math.round(s.pct || 0);
      const vol = fmtOctets(s.octets_faits) + ' / ' + fmtOctets(s.octets_total);
      let h = '<div class="dec-prog"><i style="width:' + pct + '%"></i></div>'
        + '<p class="dec-lbl">' + t('sauv_phase') + ' (' + pct + '%)</p>'
        + '<p class="dec-lbl">' + t('sauv_prog', { faits: s.faits || 0, total: s.total || 0, vol }) + '</p>';
      if (s.titre) h += '<p class="dec-lbl dec-sauv-titre">' + esc(s.titre) + '</p>';
      if (s.eta_secondes != null && s.eta_secondes >= 0) h += '<p class="dec-lbl">' + t('sauv_eta', { eta: fmtDuree(s.eta_secondes) }) + '</p>';
      return h;
    }
    const src = (images && images.total) ? { p: images, cle: 'etat_images' }
      : (inventaire && inventaire.total) ? { p: inventaire, cle: 'etat_inventaire' } : null;
    const pct = src ? Math.round(100 * src.p.fait / src.p.total) : 0;
    return '<div class="dec-prog"><i style="width:' + pct + '%"></i></div>'
      + (src ? '<p class="dec-lbl">' + t(src.cle, { fait: src.p.fait, total: src.p.total }) + '</p>' : '');
  }

  function flot() {
    // Banniere d'erreur (hors phase 'ready') : la veille continue de reessayer,
    // on affiche le dernier message au lieu de laisser l'ecran fige.
    const err = (erreur && phase !== 'ready') ? '<p class="dec-err">' + esc(erreur) + '</p>' : '';
    if (phase === 'search') {
      return '<h1 class="dec-titre">' + t('titre_attente') + '</h1>'
        + '<p class="dec-accroche"><span>' + t('consigne_1') + '</span><span>' + t('consigne_2') + '</span></p>'
        + '<div class="dec-etat"><span class="dec-pastille attente"></span><span>' + t('etat_attente') + '</span></div>'
        + '<label class="dec-opt"><input type="checkbox" id="decSauv"' + (avecSauvegarde ? ' checked' : '') + '><span>' + t('sauv_option') + '</span></label>'
        + '<p class="dec-opt-aide">' + t('sauv_option_aide') + '</p>' + err;
    }
    if (phase === 'read') {
      return '<h1 class="dec-titre">' + t('titre_attente') + '</h1>'
        + '<div class="dec-lecture"><div class="dec-etat"><span class="dec-pastille active"></span><span>' + t('etat_lecture') + '</span></div>'
        + barreLecture() + '</div>' + err;
    }
    // ready
    return '<h1 class="dec-titre">' + t('titre_pret') + '</h1>' + rapportHTML(pret || {})
      + '<div class="dec-champ"><label for="decNom">' + t('nom_label') + '</label>'
      + '<input id="decNom" type="text" value="" autocomplete="off" spellcheck="false"></div>'
      + '<div class="dec-actions"><button class="dec-btn" data-valider>' + t('bouton_go') + '</button></div>';
  }

  function render() {
    host.innerHTML = '<div class="dec-card"><div class="dec-in">'
      + '<img class="dec-icone" src="' + iconeUrl + '" alt="">'
      + '<div class="dec-marque">' + t('marque') + '</div>'
      + '<div class="dec-sub">' + t('ihm_brand_sub') + '</div>'
      + '<div class="dec-flot">' + flot() + '</div>'
      + '</div></div>';
    // Valeur par defaut affectee en .value (pas d'injection HTML : esc n'echappe
    // pas le guillemet, dangereux en contexte d'attribut si nomDefaut devenait
    // dynamique). Le champ n'existe qu'en phase 'ready'.
    const champNom = host.querySelector('#decNom');
    if (champNom) champNom.value = opts.nomDefaut || t('ihm_nom_exemple');
    // Case "sauvegarder tout le contenu" (phase attente) : conserve l'etat et le
    // transmet au backend pour la PROCHAINE lecture de boite.
    const caseSauv = host.querySelector('#decSauv');
    if (caseSauv) caseSauv.addEventListener('change', () => {
      avecSauvegarde = caseSauv.checked;
      try { DefinirSauvegardeInitiale(avecSauvegarde); } catch (_) { /* binding indispo */ }
    });
    const v = host.querySelector('[data-valider]');
    if (v) v.addEventListener('click', () => {
      const nom = (host.querySelector('#decNom').value || '').trim() || (opts.nomDefaut || t('ihm_nom_exemple'));
      const p = Object.assign({}, pret || {}); p.__nom = nom;
      if (opts.onTermine) opts.onTermine(p);
    });
  }

  unsub.push(EventsOn('boite:attente', () => { if (phase === 'search') render(); }));
  unsub.push(EventsOn('boite:connectee', () => { if (phase !== 'ready') { phase = 'read'; render(); } }));
  unsub.push(EventsOn('boite:progres', () => { if (phase !== 'ready') { phase = 'read'; render(); } }));
  unsub.push(EventsOn('boite:inventaire', (p) => { inventaire = p; if (phase !== 'ready') { phase = 'read'; } render(); }));
  unsub.push(EventsOn('boite:images', (p) => { images = p; if (phase !== 'ready') { phase = 'read'; } render(); }));
  unsub.push(EventsOn('sauvegarde:progres', (p) => { sauvegarde = p; if (phase !== 'ready') { phase = 'read'; } render(); }));
  // Erreur de lecture : la veille reessaie en boucle, on affiche le message sans
  // figer l'ecran (sinon l'utilisateur ne voit rien bouger). Efface a la reussite.
  unsub.push(EventsOn('boite:erreur', (m) => {
    erreur = m ? msgErreur(m) : t('err_lecture_generique');
    if (phase !== 'ready') render();
  }));
  // Boite deja connue : le backend n'emet rien (ni connectee, ni pret). Le
  // composant reste donc en attente ; l'utilisateur peut connecter une autre boite.
  unsub.push(EventsOn('boite:pret', (p) => { pret = p || {}; erreur = null; phase = 'ready'; render(); }));

  render();
  // Etat initial de l'option (cochee par defaut) transmis avant la veille.
  try { DefinirSauvegardeInitiale(avecSauvegarde); } catch (_) { /* binding indispo */ }
  try { DemarrerVeille(); } catch (_) { /* la veille se relance seule */ }

  return {
    // Annulable seulement tant que la lecture n'a pas commence (phase attente).
    // Des que la boite est en cours de lecture, on force l'aller jusqu'au bout.
    estAnnulable() { return phase === 'search'; },
    detruire() {
      // Arrete la goroutine de veille cote Go (sinon elle continue de sonder la
      // boite indefiniment, y compris pendant une future synchro). No-op si la
      // veille s'est deja arretee seule (boite lue avec succes).
      try { ArreterVeille(); } catch (_) { /* binding indispo */ }
      unsub.forEach((u) => { try { u(); } catch (_) { /* deja detache */ } });
      host.innerHTML = '';
    },
  };
}
