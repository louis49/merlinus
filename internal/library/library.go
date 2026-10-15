// Package library telecharge et stocke en local les medias a pousser sur la
// boite (audio + vignettes), nommes par l'uuid stable du contenu. C'est la
// bibliotheque de travail : tout y est present avant de basculer en AP (les
// deux phases sont disjointes, pas d'internet en mode point d'acces).
//
// Contrainte boite : audio en MP3 (ou AAC ADTS) uniquement. Un enclosure .m4a
// (AAC en conteneur MP4, cas des podcasts Radio France via RSS) n'est PAS
// televersable tel quel : on le refuse et on le signale, jamais on ne pousse un
// media que la boite ne saurait lire.
package library

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // decodeur (l'encodeur est jpeg444)
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // images RF servies en WebP

	"github.com/louis49/merlinus/internal/library/jpeg444"
	"github.com/louis49/merlinus/internal/store"
)

// DirBibliotheque est le dossier (sous travail/ : source de verite, pas un
// cache ; seuls les medias que plus rien ne reference sont purges au demarrage,
// cf. app/menage.go) des medias a pousser sur la boite : audio + vignettes
// nommes par uuid stable, a plat ({uuid}.mp3|.aac|.jpg). Un media est identique
// quelle que soit la boite : la bibliotheque est globale.
const DirBibliotheque = "travail/bibliotheque"

// cheminMedia construit le chemin relatif d'un media de la bibliotheque.
func cheminMedia(uuid, ext string) string { return DirBibliotheque + "/" + uuid + "." + ext }

// CheminIllustration : chemin relatif de la vignette {uuid}.jpg en bibliotheque.
func CheminIllustration(uuid string) string { return cheminMedia(uuid, "jpg") }

// CheminAudio : chemin relatif de l'audio {uuid}.{ext} en bibliotheque.
func CheminAudio(uuid, ext string) string { return cheminMedia(uuid, ext) }

const uaNav = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120 Safari/537.36"

func telecharger(url string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", uaNav)
	cli := &http.Client{Timeout: 120 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20)) // plafond 200 Mo
	return data, resp.Header.Get("Content-Type"), err
}

// cheminSansQuery renvoie le chemin d'une URL en minuscules, sans query string.
func cheminSansQuery(url string) string {
	u := strings.ToLower(url)
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	return u
}

// AudioTelechargeable pre-filtre : la boite lit mp3 et aac, et on sait remuxer
// le m4a (AAC-LC) en aac. On accepte donc mp3/aac/m4a/mp4 ; le refus definitif
// (HE-AAC, format exotique) intervient au telechargement. Extension inconnue :
// on tente quand meme (verifie via le type MIME).
func AudioTelechargeable(url string) bool {
	u := cheminSansQuery(url)
	return strings.HasSuffix(u, ".mp3") || strings.HasSuffix(u, ".aac") ||
		strings.HasSuffix(u, ".m4a") || strings.HasSuffix(u, ".mp4") ||
		!strings.Contains(u[strings.LastIndexByte(u, '/')+1:], ".")
}

// PreparerEpisode telecharge l'audio (mp3 tel quel, ou m4a remuxe en aac) et la
// vignette 128x128 d'un episode, nommes par uuid. La boite exige les deux
// ({uuid}.mp3|.aac ET {uuid}.jpg) : si l'un manque, on echoue (rien a moitie).
// Renvoie l'extension audio retenue ("mp3"/"aac") et une erreur explicite si le
// format n'est pas exploitable (HE-AAC, exotique) -> a signaler.
func PreparerEpisode(uuid, audioURL, imageURL string) (string, error) {
	if uuid == "" || audioURL == "" {
		return "", fmt.Errorf("episode incomplet (uuid/audio manquant)")
	}
	// Deja prepare (audio ET vignette presents) : rien a retelecharger. Symetrie
	// avec PreparerImage, et evite un telechargement inutile sur re-ajout.
	if ext := ExtensionAudio(uuid); ext != "" && store.Has(CheminIllustration(uuid)) {
		return ext, nil
	}
	// Vignette d'abord (echec rapide si absente : la boite l'exige).
	jpg, err := ImageJPEG128(imageURL)
	if err != nil {
		return "", fmt.Errorf("illustration de l'episode : %w", err)
	}
	audio, ct, err := telecharger(audioURL)
	if err != nil {
		return "", fmt.Errorf("telechargement audio : %w", err)
	}
	if len(audio) < 1024 {
		return "", fmt.Errorf("audio vide ou tronque (%d octets)", len(audio))
	}
	ext, media, err := normaliserAudio(audioURL, ct, audio)
	if err != nil {
		return "", err
	}
	if err := store.Save(CheminAudio(uuid, ext), media); err != nil {
		return "", err
	}
	if err := store.Save(CheminIllustration(uuid), jpg); err != nil {
		return "", err
	}
	return ext, nil
}

// normaliserAudio ramene l'audio a un format lisible par la boite : mp3 tel
// quel, ou m4a (AAC-LC) remuxe en aac (ADTS). Refuse le reste avec un message
// clair (a signaler comme histoire non-telechargeable).
func normaliserAudio(url, ct string, data []byte) (ext string, out []byte, err error) {
	u := cheminSansQuery(url)
	ctl := strings.ToLower(ct)
	switch {
	case strings.HasSuffix(u, ".mp3") || strings.HasPrefix(ctl, "audio/mpeg"):
		return "mp3", data, nil
	case strings.HasSuffix(u, ".aac") || strings.HasPrefix(ctl, "audio/aac"):
		return "aac", data, nil
	case strings.HasSuffix(u, ".m4a") || strings.HasSuffix(u, ".mp4") ||
		strings.HasPrefix(ctl, "audio/mp4") || strings.HasPrefix(ctl, "audio/x-m4a") ||
		strings.HasPrefix(ctl, "video/mp4"):
		aac, err := RemuxerAACenADTS(data)
		if err != nil {
			return "", nil, fmt.Errorf("audio m4a non convertible : %w", err)
		}
		return "aac", aac, nil
	default:
		return "", nil, fmt.Errorf("format audio non pris en charge (type %q) : la boite ne lit que mp3/aac", ct)
	}
}

// PreparerImage telecharge et stocke une vignette 128 (illustration de serie),
// si elle n'est pas deja presente.
func PreparerImage(uuid, imageURL string) error {
	if uuid == "" {
		return fmt.Errorf("uuid vide")
	}
	if store.Has(CheminIllustration(uuid)) {
		return nil
	}
	jpg, err := ImageJPEG128(imageURL)
	if err != nil {
		return err
	}
	return store.Save(CheminIllustration(uuid), jpg)
}

// ExtensionAudio renvoie l'extension de l'audio en bibliotheque ("mp3"/"aac"),
// ou "" s'il est absent. Sert a la publication (nom du fichier a televerser).
func ExtensionAudio(uuid string) string {
	if store.Has(CheminAudio(uuid, "mp3")) {
		return "mp3"
	}
	if store.Has(CheminAudio(uuid, "aac")) {
		return "aac"
	}
	return ""
}

// UUIDDepuisNom extrait l'uuid d'un nom de fichier "<uuid>.<ext>". Sans
// extension (ex. "wifi.cfg" en a une ; un nom sans point n'en a pas), renvoie le
// nom entier et ok = false.
func UUIDDepuisNom(nom string) (uuid string, ok bool) {
	if i := strings.LastIndexByte(nom, '.'); i > 0 {
		return nom[:i], true
	}
	return nom, false
}

// TaillesLocales somme, par UUID, la taille des medias presents en bibliotheque
// (audio + jpg). Sert de repli quand l'inventaire de la boite est perime ou ne
// couvre pas un contenu (ex. ajoute apres le dernier inventaire).
func TaillesLocales() map[string]int64 {
	out := map[string]int64{}
	for _, nom := range store.ListerDir(DirBibliotheque) {
		uuid, ok := UUIDDepuisNom(nom)
		if !ok {
			continue
		}
		if n, e := store.Size(DirBibliotheque + "/" + nom); e == nil {
			out[uuid] += n
		}
	}
	return out
}

// ImageJPEG128 telecharge une image et la rend en JPEG baseline 128x128 (taille
// imposee par le firmware). Meme pipeline que les illustrations de categorie.
func ImageJPEG128(url string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("URL d'image vide")
	}
	data, _, err := telecharger(url)
	if err != nil {
		return nil, err
	}
	return JPEG128FromBytes(data)
}

// JPEG128FromBytes redimensionne une image deja en memoire (ex. cover extraite
// d'un mp3/m4a) en JPEG baseline 4:4:4 128x128, taille imposee par le firmware.
// Une image non carree (ex. vignettes d'episodes Radio France en 16:9) n'est
// ni rognee ni deformee : elle est posee ENTIERE au centre, sur un fond fait
// d'elle-meme agrandie et floutee (meme rendu que les cartes du catalogue).
func JPEG128FromBytes(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decodage image : %w", err)
	}
	var buf bytes.Buffer
	// 4:4:4 qualite 95, comme les illustrations officielles : le 4:2:0 de
	// image/jpeg borde de franges un texte fin sur un aplat sature (jpeg444).
	if err := jpeg444.Encode(&buf, carre128(src), &jpeg444.Options{Quality: 95}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecoderDataURL extrait les octets d'une data URL ("data:<type>;base64,...").
func DecoderDataURL(u string) ([]byte, error) {
	i := strings.IndexByte(u, ',')
	if !strings.HasPrefix(u, "data:") || i < 0 {
		return nil, fmt.Errorf("data URL invalide")
	}
	raw, err := base64.StdEncoding.DecodeString(u[i+1:])
	if err != nil {
		return nil, fmt.Errorf("base64 : %w", err)
	}
	return raw, nil
}

// JPEG128DepuisDataURL : illustration de boite (JPEG 128x128, cf.
// JPEG128FromBytes) depuis une image en data URL (import ou editeur de l'IHM).
func JPEG128DepuisDataURL(u string) ([]byte, error) {
	raw, err := DecoderDataURL(u)
	if err != nil {
		return nil, err
	}
	return JPEG128FromBytes(raw)
}

// surBlanc aplatit l'image sur un fond blanc : le JPEG n'a pas de canal alpha,
// une zone transparente (logo PNG detoure) deviendrait noire.
func surBlanc(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, image.White, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

// carre128 compose l'illustration carree 128x128 (cf. JPEG128FromBytes).
func carre128(src image.Image) *image.RGBA {
	const S = 128
	src = surBlanc(src)
	dst := image.NewRGBA(image.Rect(0, 0, S, S))
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return dst
	}
	// Quasi carree (ecart < 2 %) : simple redimensionnement, sans fond.
	if d := w - h; d*50 <= w && -d*50 <= w {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		return dst
	}
	// Fond : la partie centrale carree de l'image, reduite a 12x12 puis
	// re-agrandie en bilineaire (flou), legerement assombrie pour detacher
	// l'image du dessus.
	c := w
	if h < c {
		c = h
	}
	centre := image.Rect(b.Min.X+(w-c)/2, b.Min.Y+(h-c)/2, b.Min.X+(w-c)/2+c, b.Min.Y+(h-c)/2+c)
	petit := image.NewRGBA(image.Rect(0, 0, 12, 12))
	draw.ApproxBiLinear.Scale(petit, petit.Bounds(), src, centre, draw.Src, nil)
	draw.BiLinear.Scale(dst, dst.Bounds(), petit, petit.Bounds(), draw.Src, nil)
	voile := image.NewUniform(color.RGBA{0, 0, 0, 40})
	draw.Draw(dst, dst.Bounds(), voile, image.Point{}, draw.Over)
	// Dessus : l'image entiere, ratio conserve, centree.
	fw, fh := S, S
	if w > h {
		fh = S * h / w
	} else {
		fw = S * w / h
	}
	x0, y0 := (S-fw)/2, (S-fh)/2
	draw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+fw, y0+fh), src, b, draw.Over, nil)
	return dst
}
