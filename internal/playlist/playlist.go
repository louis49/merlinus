// Package playlist decode l'index binaire de la playlist de l'enceinte et
// reconstruit l'arbre de contenu publie sur la boite. C'est la source de verite
// de la structure : le protocole local ne relit pas l'arborescence autrement, et
// ce chemin ne depend d'aucun backend.
//
// Firmware 2.1.23 : enregistrements de taille FIXE 96 octets (cf. DecodeV2),
// entiers little-endian. La playlist est soit en clair (pl.bin), soit chiffree
// (pl_enc.bin, dechiffree en amont par device.DechiffrerPlaylist) ; dans les deux
// cas le clair decode est au meme format 96 octets.
package playlist

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// RecordSizeV2 est la taille fixe d'un enregistrement de pl.bin (firmware 2.1.23+).
const RecordSizeV2 = 96

// Kind : nature d'un noeud (u16 @8), firmware 2.1.23 (docs/FOUILLE-B9-B17.md).
// Le type tient sur les 4 bits bas ; 0x10 marque le conteneur des webradios
// (cle JSON "webradio") ; 0x20/0x40/0x80 sont des types d'ACCES poses par le
// backend Bayard (free/paid/abonne), jamais lus par le firmware hors abonnement.
// Le drapeau "discover" des anciens firmwares n'existe plus.
const (
	KindRoot           uint16 = 0x0001
	KindFolder         uint16 = 0x0002
	KindAudio          uint16 = 0x0004
	KindFolderFavorite uint16 = 0x000a
	KindMask           uint16 = 0x000f
	KindWebradio       uint16 = 0x0010
	KindAccesGratuit   uint16 = 0x0020
	KindAccesPayant    uint16 = 0x0040
	KindAccesAbonne    uint16 = 0x0080
	KindAccesMask      uint16 = KindAccesGratuit | KindAccesPayant | KindAccesAbonne
)

// Item est un enregistrement decode de la playlist (pl.bin / pl_enc.bin).
type Item struct {
	ID            uint16
	ParentID      uint16
	Order         uint16
	ChildrenCount uint16
	Kind          uint16 // kind sans les bits d'acces (type + webradio), pour BuildTree/Summarize
	KindRaw       uint16 // kind BRUT du binaire (0 si l'Item n'a pas ete decode) ; pour SerialiserV2 fidele
	// LimitTimeUnix (@10) : date limite d'exploitation, 0 = aucune. Le firmware la
	// compare a son horloge : depassee, le titre est saute ("Ce titre n'est plus
	// disponible"). Ce n'est JAMAIS une date d'ajout (docs/FOUILLE-B9-B17.md).
	LimitTimeUnix uint32
	// SautJingle (@14) : secondes a sauter au debut du titre (lu par le firmware
	// si l'option set_skip_jingle est active). 0 partout en pratique ; conserve tel
	// quel a la republication (docs/FOUILLE-B9-B17.md, deduction forte).
	SautJingle uint16
	FileName   string // UUID du noeud
	Title      string
}

// Node est un noeud de l'arbre reconstruit, serialise au format attendu par
// l'enceinte (cle `child`, drapeaux a la presence de la cle).
type Node struct {
	UUID        string  `json:"uuid"`
	Title       string  `json:"title"`
	Children    []*Node `json:"child,omitempty"`
	AddTimeUnix uint32  `json:"add_time,omitempty"`   // date d'ajout LOCALE (IHM) ; absente du binaire
	LimitTime   *uint32 `json:"limit_time,omitempty"` // date limite (limit_time @10), 0 = aucune
	Favorite    int     `json:"favorite,omitempty"`
	Webradio    int     `json:"webradio,omitempty"`
}

// DecodeV2 lit pl.bin (firmware 2.1.23+) : enregistrements de 96 octets.
//
//	id(u16)@0 parentId(u16)@2 order(u16)@4 childrenCount(u16)@6 kind(u16)@8
//	limit_time(u32)@10 saut_jingle(u16)@14 UUID(16 octets)@16 titre(64 octets)@32
//
// L'UUID est stocke binaire, ses 5 segments octets-inverses (voir
// uuidDepuisBinaire). Le resultat est le MEME type Item que Decode : BuildTree
// et Summarize fonctionnent sans changement.
func DecodeV2(buf []byte) ([]Item, error) {
	if len(buf)%RecordSizeV2 != 0 {
		return nil, fmt.Errorf("taille de pl.bin non multiple de %d : %d octets", RecordSizeV2, len(buf))
	}
	items := make([]Item, 0, len(buf)/RecordSizeV2)
	for idx := 0; idx+RecordSizeV2 <= len(buf); idx += RecordSizeV2 {
		cur := buf[idx : idx+RecordSizeV2]
		var it Item
		it.ID = binary.LittleEndian.Uint16(cur[0:2])
		it.ParentID = binary.LittleEndian.Uint16(cur[2:4])
		it.Order = binary.LittleEndian.Uint16(cur[4:6])
		it.ChildrenCount = binary.LittleEndian.Uint16(cur[6:8])
		kind := binary.LittleEndian.Uint16(cur[8:10])
		it.KindRaw = kind               // valeur brute du binaire, pour un re-encodage fidele (SerialiserV2)
		it.Kind = kind &^ KindAccesMask // les bits d'acces (backend) ne disent rien de la nature du noeud
		it.LimitTimeUnix = binary.LittleEndian.Uint32(cur[10:14])
		it.SautJingle = binary.LittleEndian.Uint16(cur[14:16])
		it.FileName = uuidDepuisBinaire(cur[16:32])
		it.Title = string(bytes.TrimRight(cur[32:RecordSizeV2], "\x00"))
		items = append(items, it)
	}
	return items, nil
}

// SerialiserV2 est l'INVERSE exact de DecodeV2 : encode une liste d'Item en
// enregistrements de 96 octets (format pl.bin 2.1.23). Round-trip garanti :
// SerialiserV2(DecodeV2(buf)) == buf pour des titres null-paddes. Sert a
// reconstruire un pl.bin clair (a chiffrer ensuite en pl_enc.bin).
func SerialiserV2(items []Item) []byte {
	buf := make([]byte, len(items)*RecordSizeV2)
	for i, it := range items {
		r := buf[i*RecordSizeV2 : (i+1)*RecordSizeV2]
		binary.LittleEndian.PutUint16(r[0:2], it.ID)
		binary.LittleEndian.PutUint16(r[2:4], it.ParentID)
		binary.LittleEndian.PutUint16(r[4:6], it.Order)
		binary.LittleEndian.PutUint16(r[6:8], it.ChildrenCount)
		// Kind : valeur BRUTE (bits d'acces compris) si l'Item vient de DecodeV2
		// ou de SerialiserArbreV2 ; a defaut, le Kind (sans bits d'acces).
		kind := it.KindRaw
		if kind == 0 {
			kind = it.Kind
		}
		binary.LittleEndian.PutUint16(r[8:10], kind)
		binary.LittleEndian.PutUint32(r[10:14], it.LimitTimeUnix)
		binary.LittleEndian.PutUint16(r[14:16], it.SautJingle)
		copy(r[16:32], uuidVersBinaire(it.FileName))
		t := []byte(it.Title)
		if len(t) > RecordSizeV2-32 {
			t = t[:RecordSizeV2-32]
		}
		copy(r[32:RecordSizeV2], t) // reste deja a zero (null-pad)
	}
	return buf
}

// UUIDRacine : uuid du record racine (16 octets nuls) en forme canonique.
const UUIDRacine = "00000000-0000-0000-0000-000000000000"

// SerialiserArbreV2 reproduit le convertisseur JSON->pl.bin de la boite : a
// partir de l'arbre (noeuds de PREMIER niveau, SANS racine explicite), il produit
// la liste d'Item plate 2.1.23. Conventions observees sur arbre reel :
//   - record RACINE synthetise en tete (id 0, kind 0x01, uuid tout-zero, titre
//     "Root", childrenCount = nb de categories) ;
//   - id attribue en PRE-ORDRE DFS (parent avant enfants), a partir de 1 ;
//   - order = index du noeud parmi ses freres ; childrenCount = nb d'enfants
//     directs ; parentId = id du parent (0 pour les categories) ;
//   - kind BRUT : Folder 0x02 ; FolderFavorite 0x0a (drapeau favorite) ;
//     conteneur webradio 0x12 (0x02|0x10, drapeau webradio) ; feuille audio 0x04
//     (sans bit d'acces, comme le parseur JSON de la boite) ; station radio =
//     feuille SOUS un conteneur webradio -> 0x02 (dossier vide). limit_time @10 = date limite ; saut de jingle @14 (cf. Connu).
//
// Connu : ce que la boite porte deja pour un uuid (lu par DecodeV2 sur la
// playlist reelle) et que l'arbre Node ne sait pas representer :
//   - Kind   : kind BRUT, bits d'acces du backend compris (0x24 free, 0x44
//     paid, 0x84 abonne) : un contenu officiel garde exactement son kind ;
//   - Limite : limit_time @10, DATE LIMITE (0 = aucune), PAS une date d'ajout ;
//   - Jingle : saut de jingle @14, reecrit tel quel.
type Connu struct {
	Kind   uint16
	Limite uint32
	Jingle uint16
}

// ConnusDepuis construit la carte uuid -> Connu depuis les Items lus sur la boite.
func ConnusDepuis(items []Item) map[string]Connu {
	m := make(map[string]Connu, len(items))
	for _, it := range items {
		m[it.FileName] = Connu{Kind: it.KindRaw, Limite: it.LimitTimeUnix, Jingle: it.SautJingle}
	}
	return m
}

// connu (optionnel) : pour un re-encodage FIDELE de l'existant, on reprend le
// kind brut et la limite de la boite ; pour du contenu NOUVEAU (uuid absent), on
// retombe sur kindBrutV2 et sur LimitTime (0 si absent). On n'ecrit JAMAIS la
// date d'ajout dans ce champ : la boite la lirait comme une expiration passee.
// Passer nil sans playlist reelle (defauts seuls).
func SerialiserArbreV2(top []*Node, connu map[string]Connu) []Item {
	items := make([]Item, 0, 1+len(top))
	rootKind := KindRoot
	if c, ok := connu[UUIDRacine]; ok {
		rootKind = c.Kind
	}
	items = append(items, Item{
		ID: 0, ParentID: 0, Order: 0, ChildrenCount: uint16(len(top)),
		Kind: rootKind, KindRaw: rootKind, FileName: UUIDRacine, Title: "Root",
	})
	var nextID uint16 = 1
	var emit func(n *Node, parentID, order uint16, sousWebradio bool)
	emit = func(n *Node, parentID, order uint16, sousWebradio bool) {
		id := nextID
		nextID++
		raw := kindBrutV2(n, sousWebradio)
		var limite uint32
		var jingle uint16
		if n.LimitTime != nil {
			limite = *n.LimitTime
		}
		if c, ok := connu[n.UUID]; ok {
			raw, limite, jingle = c.Kind, c.Limite, c.Jingle
		}
		it := Item{
			ID: id, ParentID: parentID, Order: order,
			Kind: raw, KindRaw: raw, FileName: n.UUID,
			Title:         tronquerTitre(nettoyerTitreBoite(n.Title)), // emoji + 63 o, comme le chemin JSON
			LimitTimeUnix: limite,
			SautJingle:    jingle,
		}
		if n.Children != nil {
			it.ChildrenCount = uint16(len(n.Children))
		}
		it.Kind = raw &^ KindAccesMask // comme DecodeV2 ; KindRaw garde les bits d'acces
		items = append(items, it)
		wr := sousWebradio || n.Webradio != 0
		for i, c := range n.Children {
			emit(c, id, uint16(i), wr)
		}
	}
	for i, n := range top {
		emit(n, 0, uint16(i), false)
	}
	return items
}

// kindBrutV2 calcule le kind BRUT (octet du binaire) d'un noeud.
func kindBrutV2(n *Node, sousWebradio bool) uint16 {
	if n.Children == nil { // feuille
		if sousWebradio {
			return KindFolder // station radio : dossier vide (0x02)
		}
		return KindAudio // 0x04, comme le parseur JSON de la boite (sans bit d'acces)
	}
	// conteneur
	if n.Favorite != 0 {
		return KindFolderFavorite // 0x0a
	}
	k := KindFolder // 0x02
	if n.Webradio != 0 {
		k |= KindWebradio // 0x12 : conteneur des webradios
	}
	return k
}

// uuidVersBinaire est l'INVERSE de uuidDepuisBinaire : forme canonique
// "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" -> 16 octets, chaque segment (4-2-2-2-6)
// octet-inverse. UUID invalide -> 16 octets nuls.
func uuidVersBinaire(u string) []byte {
	out := make([]byte, 16)
	parts := strings.Split(u, "-")
	segLens := []int{4, 2, 2, 2, 6}
	if len(parts) != 5 {
		return out
	}
	off := 0
	for i, p := range parts {
		b, err := hex.DecodeString(p)
		if err != nil || len(b) != segLens[i] {
			return make([]byte, 16)
		}
		for j := range b {
			out[off+len(b)-1-j] = b[j] // octet-inverse dans le segment
		}
		off += len(b)
	}
	return out
}

// FavorisDepuisBin decode fav.bin (favoris poses par l'enfant sur la boite) :
// simple concatenation d'UUID binaires de 16 octets, MEME encodage que les UUID
// de pl.bin (cf. uuidDepuisBinaire), sans en-tete. Renvoie la liste des UUID en
// forme canonique ; un reliquat de moins de 16 octets est ignore.
func FavorisDepuisBin(buf []byte) []string {
	out := []string{}
	for i := 0; i+16 <= len(buf); i += 16 {
		if u := uuidDepuisBinaire(buf[i : i+16]); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// uuidDepuisBinaire convertit les 16 octets d'un UUID stocke en 2.1.23 vers sa
// forme canonique. Les 5 segments (4-2-2-2-6) sont octets-inverses
// (little-endian par segment). Confirme en reel contre un UUID connu.
func uuidDepuisBinaire(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	rev := func(s []byte) []byte {
		o := make([]byte, len(s))
		for i := range s {
			o[len(s)-1-i] = s[i]
		}
		return o
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", rev(b[0:4]), rev(b[4:6]), rev(b[6:8]), rev(b[8:10]), rev(b[10:16]))
}

// BuildTree reconstruit l'arbre a partir des enregistrements, en partant du
// noeud racine.
func BuildTree(items []Item) ([]*Node, error) {
	var rootID uint16
	found := false
	for _, it := range items {
		if it.Kind == KindRoot {
			rootID, found = it.ID, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("noeud racine introuvable dans la playlist")
	}
	return walk(items, rootID, map[uint16]bool{rootID: true}), nil
}

// walk collecte les enfants directs de parentID puis descend recursivement. vus
// contient les ids deja places : garde-fou contre un arbre cyclique ou un noeud
// a parents multiples (donnees corrompues/hostiles) qui ferait boucler.
func walk(items []Item, parentID uint16, vus map[uint16]bool) []*Node {
	var children []*Node
	for i := range items {
		it := items[i]
		if it.ID == parentID || it.ParentID != parentID {
			continue
		}
		if vus[it.ID] {
			continue // deja place ailleurs : on ne redescend pas (anti-cycle)
		}
		vus[it.ID] = true
		kind := it.Kind & KindMask

		node := &Node{UUID: it.FileName, Title: it.Title}
		if kind == KindAudio {
			// Seule date du binaire : la date LIMITE. La date d'ajout (add_time)
			// n'y existe pas : elle n'est connue que localement (IHM).
			lt := it.LimitTimeUnix
			node.LimitTime = &lt
		}
		node.Children = walk(items, it.ID, vus)
		if (kind == KindFolder || kind == KindFolderFavorite) && node.Children == nil {
			node.Children = make([]*Node, 0)
		}
		if kind == KindFolderFavorite {
			node.Favorite = 1
		}
		if it.Kind&KindWebradio != 0 {
			node.Webradio = 1 // noeud radio (conteneur ou station)
		}
		children = append(children, node)
	}
	return children
}

// Report resume l'arbre lu, pour le compte rendu affiche a l'utilisateur.
type Report struct {
	Categories    int    `json:"categories"`   // noeuds de premier niveau (sous la racine)
	Dossiers      int    `json:"dossiers"`     // dossiers/series (folder + favorite)
	Histoires     int    `json:"histoires"`    // fichiers audio
	Total         int    `json:"total"`        // total d'enregistrements
	FavoriTitre   string `json:"favori_titre"` // titre du dossier favori, s'il existe
	OctetsPlaylst int    `json:"octets"`       // taille de la playlist decodee
}

// Summarize calcule le compte rendu a partir des enregistrements decodes.
func Summarize(items []Item, tailleBin int) Report {
	r := Report{Total: len(items), OctetsPlaylst: tailleBin}
	var rootID uint16
	for _, it := range items {
		if it.Kind == KindRoot {
			rootID = it.ID
			break
		}
	}
	for _, it := range items {
		switch it.Kind & KindMask {
		case KindAudio:
			r.Histoires++
		case KindFolder, KindFolderFavorite:
			r.Dossiers++
		}
		if it.Kind&KindMask == KindFolderFavorite {
			r.FavoriTitre = it.Title
		}
		if it.ParentID == rootID && it.Kind != KindRoot {
			r.Categories++
		}
	}
	return r
}
