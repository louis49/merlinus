import './style.css';
import iconeUrl from './assets/merlinus.svg';

import { EstConfigure, DemarrerVeille, NommerBoite } from '../wailsjs/go/app/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

import { monterIHM } from './ihm.js';
import { creerDecouverte } from './decouverte.js';
import { t } from './i18n.js';

const app = document.querySelector('#app');

// Empeche la WebView de naviguer vers un fichier lache hors zone de depot
// (comportement par defaut du navigateur). Le depot sur la zone "Mes histoires"
// reste gere nativement par Wails (EnableFileDrop + --wails-drop-target).
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => e.preventDefault());

// ---- Ecran d'accueil (premiere utilisation) ----
function ecranAccueil() {
  app.innerHTML = `<div class="scene"><div id="decHost"></div></div>`;
  let dec;
  dec = creerDecouverte({
    host: document.getElementById('decHost'),
    mode: 'welcome',
    nomDefaut: t('ihm_nom_exemple'),
    onTermine: async (p) => {
      const cle = p.mac || p.ssid;
      if (cle) { try { await NommerBoite(cle, p.__nom); } catch (_) { /* nom optionnel */ } }
      if (dec) dec.detruire();
      monterIHM(app);
    },
  });
}

// Point d'entree : premiere utilisation -> accueil ; sinon IHM directe.
// Si la boite est deja configuree mais qu'aucun registre local n'existe (autre
// session), on seme une boite active a partir du dernier compte rendu persiste.
EstConfigure()
  .then((ok) => { if (ok) monterIHM(app); else ecranAccueil(); })
  .catch(() => ecranAccueil());
