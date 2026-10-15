// Formatage des tailles et des durees, commun a toute l'IHM (separateur
// decimal et unites de la langue).
import { lang } from './i18n.js';

// fmtOctets : volume lisible. Ko sous 1 Mo, une decimale sous 10 (3,4 Mo),
// entier au-dela (12 Mo) ; Go a partir de 1 Go (une decimale).
export function fmtOctets(n) {
  n = Number(n) || 0;
  const en = lang === 'en';
  const nombre = (x, decimale) => { const s = decimale ? x.toFixed(1) : String(Math.round(x)); return en ? s : s.replace('.', ','); };
  if (n >= 1073741824) return nombre(n / 1073741824, true) + (en ? ' GB' : ' Go');
  if (n >= 1048576) { const mo = n / 1048576; return nombre(mo, mo < 10) + (en ? ' MB' : ' Mo'); }
  if (n >= 1024) return Math.round(n / 1024) + (en ? ' KB' : ' Ko');
  return n + (en ? ' B' : ' o');
}

// fmtDuree : duree lisible (« 1 h 5 min », « 3 min 20 s », « 45 s »).
export function fmtDuree(s) {
  s = Math.max(0, Math.round(Number(s) || 0));
  const mn = Math.floor(s / 60), sec = s % 60;
  if (mn >= 60) return Math.floor(mn / 60) + ' h ' + (mn % 60) + ' min';
  if (mn > 0) return mn + ' min ' + sec + ' s';
  return sec + ' s';
}
