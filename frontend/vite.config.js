// Config Vite. En DEV uniquement, on reproduit les handlers d'assets du Go
// (internal/app/run.go + catalogue.go) : sans ca, le serveur de dev Vite repond
// index.html (fallback SPA) et les images n'apparaissent pas. En PROD, c'est
// l'AssetServer Wails qui sert /boiteimg et /catimg ; ces plugins n'ont aucun
// effet (le build ne les embarque pas).
//
// Arborescence du store (cf. internal/store + internal/app) :
//   cache/boites/<mac>/affichage/<uuid>.jpg   illustrations d'arbre (cache)
//   travail/bibliotheque/<uuid>.jpg           bibliotheque (repli)
//   cache/catalogue/<sha256(url)>             images de catalogue (cache)

import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';

// storeDir : repertoire de donnees de l'app, comme store.Dir() cote Go
// (dossierBase : Application Support, LOCALAPPDATA, XDG_DATA_HOME).
function storeDir() {
  if (process.platform === 'darwin') {
    return path.join(os.homedir(), 'Library', 'Application Support', 'merlinus');
  }
  if (process.platform === 'win32') {
    return path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local'), 'merlinus');
  }
  const xdg = process.env.XDG_DATA_HOME;
  return path.join(xdg && path.isAbsolute(xdg) ? xdg : path.join(os.homedir(), '.local', 'share'), 'merlinus');
}

// macKey : normalisation identique a macKey() cote Go (minuscules, sans separateurs).
function macKey(m) {
  return String(m).toLowerCase().replace(/[:\-. ]/g, '');
}

const UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 '
  + '(KHTML, like Gecko) Chrome/120 Safari/537.36';

// /boiteimg/<mac>/<uuid> : illustration d'arbre en cache d'affichage, repli
// bibliotheque (miroir de servirImageBoite dans run.go).
const boiteimgDev = {
  name: 'boiteimg-dev',
  configureServer(server) {
    const dir = storeDir();
    server.middlewares.use((req, res, next) => {
      if (!req.url || !req.url.startsWith('/boiteimg/')) return next();
      try {
        const reste = decodeURIComponent(req.url.slice('/boiteimg/'.length)).split('?')[0];
        const parts = reste.split('/');
        if (parts.length < 2) return next();
        const mac = macKey(parts[0]);
        const uuid = parts[1];
        let fichier = path.join(dir, 'cache', 'boites', mac, 'affichage', `${uuid}.jpg`);
        if (!fs.existsSync(fichier)) fichier = path.join(dir, 'travail', 'bibliotheque', `${uuid}.jpg`); // repli
        if (!fs.existsSync(fichier)) { res.statusCode = 404; res.end(); return; }
        res.setHeader('Content-Type', 'image/jpeg');
        res.setHeader('Cache-Control', 'max-age=3600'); // anti-scintillement au re-rendu de l'arbre
        fs.createReadStream(fichier).pipe(res);
      } catch (_) {
        next();
      }
    });
  },
};

// /catimg?u=<url> : image de catalogue, cache disque sha256(url) -> fetch si
// absent (miroir de imageCatalogue dans catalogue.go).
const catimgDev = {
  name: 'catimg-dev',
  configureServer(server) {
    const dir = path.join(storeDir(), 'cache', 'catalogue');
    server.middlewares.use(async (req, res, next) => {
      if (!req.url || !req.url.startsWith('/catimg')) return next();
      try {
        const u = new URL(req.url, 'http://localhost').searchParams.get('u');
        if (!u || !/^https?:\/\//i.test(u)) { res.statusCode = 404; res.end(); return; }
        const hash = crypto.createHash('sha256').update(u).digest('hex');
        const fichier = path.join(dir, hash);
        if (!fs.existsSync(fichier)) {
          const r = await fetch(u, { headers: { 'User-Agent': UA } });
          if (!r.ok) { res.statusCode = 404; res.end(); return; }
          const buf = Buffer.from(await r.arrayBuffer());
          fs.mkdirSync(dir, { recursive: true });
          fs.writeFileSync(fichier, buf);
        }
        // Content-Type best-effort : le webview rend l'image d'apres ses octets.
        const head = fs.readFileSync(fichier).subarray(0, 12);
        const ct = (head.length >= 12 && head.subarray(0, 4).toString('latin1') === 'RIFF'
          && head.subarray(8, 12).toString('latin1') === 'WEBP') ? 'image/webp'
          : (head[0] === 0x89 && head[1] === 0x50) ? 'image/png' : 'image/jpeg';
        res.setHeader('Content-Type', ct);
        res.setHeader('Cache-Control', 'max-age=86400');
        fs.createReadStream(fichier).pipe(res);
      } catch (_) {
        next();
      }
    });
  },
};

// /biblioaudio/<uuid> : audio de la bibliotheque, avec requetes Range pour que
// le lecteur puisse se positionner (miroir de servirAudioBibliotheque, run.go).
const biblioaudioDev = {
  name: 'biblioaudio-dev',
  configureServer(server) {
    const dir = path.join(storeDir(), 'travail', 'bibliotheque');
    server.middlewares.use((req, res, next) => {
      if (!req.url || !req.url.startsWith('/biblioaudio/')) return next();
      const uuid = decodeURIComponent(req.url.slice('/biblioaudio/'.length)).split('?')[0];
      if (!/^[A-Za-z0-9-]{1,64}$/.test(uuid)) { res.statusCode = 404; res.end(); return; }
      let fichier = path.join(dir, `${uuid}.mp3`), type = 'audio/mpeg';
      if (!fs.existsSync(fichier)) { fichier = path.join(dir, `${uuid}.aac`); type = 'audio/aac'; }
      if (!fs.existsSync(fichier)) { res.statusCode = 404; res.end(); return; }
      const taille = fs.statSync(fichier).size;
      res.setHeader('Content-Type', type);
      res.setHeader('Accept-Ranges', 'bytes');
      const m = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range || '');
      if (m) {
        const debut = m[1] ? Number(m[1]) : Math.max(0, taille - Number(m[2]));
        const fin = m[1] && m[2] ? Math.min(Number(m[2]), taille - 1) : taille - 1;
        if (debut >= taille || debut > fin) { res.statusCode = 416; res.setHeader('Content-Range', `bytes */${taille}`); res.end(); return; }
        res.statusCode = 206;
        res.setHeader('Content-Range', `bytes ${debut}-${fin}/${taille}`);
        res.setHeader('Content-Length', fin - debut + 1);
        fs.createReadStream(fichier, { start: debut, end: fin }).pipe(res);
        return;
      }
      res.setHeader('Content-Length', taille);
      fs.createReadStream(fichier).pipe(res);
    });
  },
};

export default {
  plugins: [boiteimgDev, catimgDev, biblioaudioDev],
};
