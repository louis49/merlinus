// Lecteur audio UNIQUE d'une grille de cartes (episodes du catalogue, histoires
// perso) : un seul contenu joue a la fois, et la lecture survit au re-rendu des
// cartes (l'element <audio> n'est pas dans leur DOM). Chaque carte est reperee
// par une cle (index d'episode, uuid...) ; l'hote fournit :
//   carteDe(cle)   -> element .ep-card monte, ou null
//   invalider(cle) -> re-rend la carte (bouton + bloc de progression)
//   onErreur(cle)  -> source injoignable (message a l'utilisateur)
// Le HTML des cartes appelle bouton(cle, avecAudio) et progres(cle, dureeConnue).

import { t } from './i18n.js';
import { escAttr } from './composants.js';

export const ICO_PLAY = '<svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.5 L19 12 L8 18.5 Z"/></svg>';
export const ICO_PAUSE = '<svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="7" y="5" width="3.6" height="14" rx="1.2"/><rect x="13.4" y="5" width="3.6" height="14" rx="1.2"/></svg>';

// fmtTemps : secondes -> "m:ss" (durees, position de lecture).
export function fmtTemps(s) {
  s = Math.max(0, Math.floor(Number(s) || 0));
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}

export function creerLecteur({ carteDe, invalider, onErreur }) {
  const audio = new Audio();
  audio.preload = 'none';
  let joue = null; // cle de la carte chargee dans le lecteur

  const majProgres = () => {
    const c = joue === null ? null : carteDe(joue); if (!c) return;
    const fill = c.querySelector('.track-bar > i'), bar = c.querySelector('.track-bar');
    const cur = c.querySelector('.cur'), dur = c.querySelector('.dur');
    if (cur) cur.textContent = fmtTemps(audio.currentTime);
    if (dur && isFinite(audio.duration)) dur.textContent = fmtTemps(audio.duration);
    if (fill && audio.duration) {
      const pct = 100 * audio.currentTime / audio.duration;
      fill.style.width = pct + '%'; bar.setAttribute('aria-valuenow', Math.round(pct));
    }
  };
  const majBouton = () => {
    const c = joue === null ? null : carteDe(joue);
    const b = c && c.querySelector('.ep-play'); if (!b) return;
    b.innerHTML = audio.paused ? ICO_PLAY : ICO_PAUSE;
    b.setAttribute('aria-label', t(audio.paused ? 'ihm_player_play' : 'ihm_player_pause'));
  };
  const finir = () => { const c = joue; joue = null; if (c !== null) invalider(c); };
  const couper = () => { audio.pause(); audio.removeAttribute('src'); audio.load(); };

  audio.addEventListener('timeupdate', majProgres);
  audio.addEventListener('loadedmetadata', majProgres);
  audio.addEventListener('play', majBouton);
  audio.addEventListener('pause', majBouton);
  audio.addEventListener('ended', finir);
  audio.addEventListener('error', () => {
    if (joue === null || !audio.getAttribute('src')) return; // coupure volontaire
    const c = joue;
    finir();
    if (onErreur) onErreur(c);
  });

  return {
    actif: (cle) => joue === cle,
    // Lecture / pause de la carte `cle` ; une autre carte en cours s'arrete.
    basculer(cle, src) {
      if (joue === cle) { if (audio.paused) audio.play().catch(() => {}); else audio.pause(); return; }
      const avant = joue;
      joue = cle;
      audio.src = src;
      audio.play().catch(() => {});
      if (avant !== null) invalider(avant);
      invalider(cle);
    },
    // Clic sur la barre de progression.
    chercher(bar, clientX) {
      const r = bar.getBoundingClientRect();
      const pos = Math.min(1, Math.max(0, (clientX - r.left) / r.width));
      if (audio.duration) audio.currentTime = pos * audio.duration;
    },
    // Fleches gauche/droite sur la barre (focus) : -/+ 10 s.
    clavier(ev) {
      if (!ev.target.classList.contains('track-bar') || !audio.duration) return;
      if (ev.key === 'ArrowRight') audio.currentTime = Math.min(audio.duration, audio.currentTime + 10);
      else if (ev.key === 'ArrowLeft') audio.currentTime = Math.max(0, audio.currentTime - 10);
    },
    // Arret avec re-rendu de la carte (ex. on quitte la vue).
    arreter() { couper(); finir(); },
    // Arret sans re-rendu (la grille est detruite).
    detruire() { joue = null; couper(); },
    // HTML du bouton rond (vide si pas d'audio).
    bouton(cle, avecAudio) {
      if (!avecAudio) return '';
      const enPause = joue !== cle || audio.paused;
      return '<button class="ep-play" type="button" aria-label="' + escAttr(t(enPause ? 'ihm_player_play' : 'ihm_player_pause')) + '">'
        + (enPause ? ICO_PLAY : ICO_PAUSE) + '</button>';
    },
    // HTML du bloc de progression de la carte active (vide sinon).
    progres(cle, dureeConnue) {
      if (joue !== cle) return '';
      const pct = audio.duration ? (100 * audio.currentTime / audio.duration) : 0;
      const dur = isFinite(audio.duration) ? fmtTemps(audio.duration) : (dureeConnue ? fmtTemps(dureeConnue) : '--:--');
      return '<div class="ep-prog"><div class="track-bar" role="slider" tabindex="0" aria-label="' + escAttr(t('ihm_player_pos')) + '"'
        + ' aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(pct) + '"><i style="width:' + pct + '%"></i></div>'
        + '<div class="times"><span class="cur">' + fmtTemps(audio.currentTime) + '</span><span class="dur">' + dur + '</span></div></div>';
    },
  };
}
