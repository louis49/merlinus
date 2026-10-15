// Liste virtualisee facon UITableView : seules les cellules visibles (+ un
// tampon) existent dans le DOM ; elles sont recyclees au scroll (pool). La
// memoire reste constante quel que soit le nombre d'elements (teste a 600+).
//
// Hauteurs variables : chaque cellule est mesuree apres montage. Hypothese pour
// que la hauteur totale reste exacte sans tout mesurer d'avance : les cellules
// NON montees valent `hauteurEstimee`. Il faut donc que l'etat "au repos" ait
// une hauteur uniforme (ici : accordeon replie a hauteur fixe) ; les cellules
// dont la hauteur reelle differe (accordeon deplie) ne le deviennent qu'une fois
// visibles, sont alors mesurees, et leur hauteur est CONSERVEE apres demontage.
//
// Conteneur de defilement : la fenetre (l'app scrolle le document).

export function creerListeVirtuelle({ hote, nombre, hauteurEstimee, gap = 0, rendre, tampon = 6 }) {
  const n = nombre;
  const H = new Array(n).fill(hauteurEstimee); // hauteur mesuree/estimee par index
  const off = new Array(n + 1).fill(0);        // offsets cumules (avec gap)
  const recalc = (from) => {
    for (let i = Math.max(0, from); i < n; i++) off[i + 1] = off[i] + H[i] + gap;
  };
  recalc(0);

  hote.style.position = 'relative';
  const sizer = document.createElement('div');
  sizer.style.cssText = 'position:relative;width:100%';
  const majTotal = () => { sizer.style.height = (n ? off[n] - gap : 0) + 'px'; };
  majTotal();
  hote.appendChild(sizer);

  const monte = new Map(); // index -> element
  const pool = [];

  const creer = () => {
    const el = document.createElement('div');
    el.style.cssText = 'position:absolute;left:0;right:0;top:0;will-change:transform';
    return el;
  };
  const poser = (el, i) => { el.style.transform = 'translateY(' + off[i] + 'px)'; };

  // premier index dont le bas depasse y (recherche binaire sur les offsets)
  const indexA = (y) => {
    let lo = 0, hi = n;
    while (lo < hi) { const m = (lo + hi) >> 1; if (off[m + 1] <= y) lo = m + 1; else hi = m; }
    return Math.min(lo, Math.max(0, n - 1));
  };

  // mesure une cellule montee ; si sa hauteur a change, recale les offsets et
  // repositionne les cellules situees plus bas. Renvoie true si ca a bouge.
  const mesurer = (el, i) => {
    const h = el.offsetHeight;
    if (h > 0 && h !== H[i]) {
      H[i] = h; recalc(i); majTotal();
      for (const [j, e] of monte) if (j > i) poser(e, j);
      return true;
    }
    return false;
  };

  let raf = 0;
  const rendreVisible = () => {
    raf = 0;
    if (n === 0) return;
    const r = hote.getBoundingClientRect();
    const vh = window.innerHeight || document.documentElement.clientHeight;
    const haut = Math.max(0, -r.top);
    const bas = haut + vh;
    const first = Math.max(0, indexA(haut) - tampon);
    const last = Math.min(n - 1, indexA(bas) + tampon);
    // demonte hors plage (recycle)
    for (const [i, el] of monte) {
      if (i < first || i > last) { el.remove(); monte.delete(i); if (pool.length < 40) pool.push(el); }
    }
    // monte la plage manquante
    for (let i = first; i <= last; i++) {
      if (monte.has(i)) continue;
      const el = pool.pop() || creer();
      el.dataset.idx = i;
      rendre(el, i);
      poser(el, i);
      sizer.appendChild(el);
      monte.set(i, el);
      mesurer(el, i);
    }
  };
  const planifier = () => { if (!raf) raf = requestAnimationFrame(rendreVisible); };

  // capture:true pour attraper aussi le defilement d'un eventuel conteneur
  // interne (les evenements scroll ne bouillonnent pas, mais passent en capture).
  const optScroll = { passive: true, capture: true };
  window.addEventListener('scroll', planifier, optScroll);
  window.addEventListener('resize', planifier);
  rendreVisible();

  return {
    // re-rend une cellule (ex. apres depliage/etat) et recale si sa hauteur change.
    invalider(i) {
      const el = monte.get(i);
      if (el) { rendre(el, i); mesurer(el, i); planifier(); }
    },
    estMonte(i) { return monte.has(i); },
    element(i) { return monte.get(i) || null; },
    rafraichir: planifier,
    detruire() {
      window.removeEventListener('scroll', planifier, optScroll);
      window.removeEventListener('resize', planifier);
      for (const [, el] of monte) el.remove();
      monte.clear(); pool.length = 0; sizer.remove();
    },
  };
}
