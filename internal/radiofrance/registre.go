package radiofrance

// Registre persistant des episodes Radio France (BUGS.md B3, B6, B8).
//
// Cle : l'id d'expression de __data.json (stable : verifie sur 10 mois
// d'archives). Valeur : l'uuid de l'episode, FIGE a la premiere vue, et la
// derniere URL audio connue. Deux effets :
//   - l'identite ne derive plus quand RF republie un media sous un autre nom
//     de fichier (avant : nouvel uuid, donc doublon sur la boite) ;
//   - /transistor/aod (une requete par episode) n'est appele que pour un
//     episode jamais vu, un episode indisponible a revoir, ou une revalidation
//     au compte-gouttes.
// L'uuid d'un episode nouveau reste calcule comme avant (cleEpisodeRF) : aucun
// uuid deja publie ne change. Un registre perdu ramene au comportement
// d'avant (risque de doublon, jamais de suppression).

import (
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	// revoirIndisponible : delai avant de redemander l'audio d'un episode
	// indisponible (aod 404).
	revoirIndisponible = 7 * 24 * time.Hour
	// revaliderApres : age d'une resolution au-dela duquel elle est refaite,
	// au plus revalidationsParSerie fois par lecture de serie.
	revaliderApres        = 30 * 24 * time.Hour
	revalidationsParSerie = 5
)

const (
	StatutOK           = "ok"
	StatutIndisponible = "indisponible"
)

// EntreeRF : un episode connu. UUID vide tant que l'episode n'a jamais eu
// d'audio (indisponible des la premiere vue).
type EntreeRF struct {
	UUID    string `json:"uuid,omitempty"`
	URL     string `json:"url,omitempty"`
	Statut  string `json:"statut"`
	Titre   string `json:"titre,omitempty"`
	Vu      int64  `json:"vu"`                // premiere vue (unix)
	Verifie int64  `json:"verifie"`           // derniere resolution aod (unix)
	Origine string `json:"origine,omitempty"` // "titre" : uuid rattache par titre
}

type fichierRegistre struct {
	Version    int                  `json:"version"`
	Episodes   map[string]*EntreeRF `json:"episodes"`
	Rapproches map[string]int64     `json:"rapproches,omitempty"` // serie -> date du rattachement par titre
}

// Registre : table id d'expression -> EntreeRF, chargee a la premiere
// utilisation et sauvegardee par Sauver. Sur pour un usage concurrent.
type Registre struct {
	charger func() ([]byte, error)
	sauver  func([]byte) error

	mu      sync.Mutex
	charge  bool
	modifie bool
	f       fichierRegistre
	parUUID map[string]string // uuid -> id
}

// NouveauRegistre : charger rend le contenu persiste (erreur = registre vide),
// sauver l'ecrit.
func NouveauRegistre(charger func() ([]byte, error), sauver func([]byte) error) *Registre {
	return &Registre{charger: charger, sauver: sauver}
}

// assurer charge le registre si besoin (mu tenu).
func (r *Registre) assurer() {
	if r.charge {
		return
	}
	r.charge = true
	if d, err := r.charger(); err == nil {
		if e := json.Unmarshal(d, &r.f); e != nil {
			log.Printf("radiofrance : registre illisible, repart vide (%v)", e)
			r.f = fichierRegistre{}
		}
	}
	if r.f.Episodes == nil {
		r.f.Episodes = map[string]*EntreeRF{}
	}
	if r.f.Rapproches == nil {
		r.f.Rapproches = map[string]int64{}
	}
	r.parUUID = map[string]string{}
	for id, e := range r.f.Episodes {
		if e.UUID != "" {
			r.parUUID[e.UUID] = id
		}
	}
}

// Entree renvoie une copie de l'entree de id.
func (r *Registre) Entree(id string) (EntreeRF, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	e, ok := r.f.Episodes[id]
	if !ok {
		return EntreeRF{}, false
	}
	return *e, true
}

// IDParUUID renvoie l'id d'expression d'un uuid d'episode connu.
func (r *Registre) IDParUUID(uuid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	return r.parUUID[uuid]
}

// resolu enregistre une resolution aod reussie et rend l'uuid de l'episode :
// celui deja connu (fige), sinon uuidNeuf.
func (r *Registre) resolu(id, titre, url, uuidNeuf string, t time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	e := r.f.Episodes[id]
	if e == nil {
		e = &EntreeRF{Vu: t.Unix()}
		r.f.Episodes[id] = e
	}
	if e.UUID == "" {
		e.UUID = uuidNeuf
		r.parUUID[uuidNeuf] = id
	}
	e.URL, e.Statut, e.Verifie = url, StatutOK, t.Unix()
	if titre != "" {
		e.Titre = titre
	}
	r.modifie = true
	return e.UUID
}

// indisponible note qu'un episode n'a pas d'audio (uuid deja connu garde).
func (r *Registre) indisponible(id, titre string, t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	e := r.f.Episodes[id]
	if e == nil {
		e = &EntreeRF{Vu: t.Unix()}
		r.f.Episodes[id] = e
	}
	e.Statut, e.Verifie = StatutIndisponible, t.Unix()
	if titre != "" {
		e.Titre = titre
	}
	r.modifie = true
}

// Rattacher donne a l'episode id l'uuid d'un contenu deja sur la boite
// (rattachement par titre). Refuse si id est inconnu ou si l'uuid appartient
// deja a un autre episode.
func (r *Registre) Rattacher(id, uuid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	e := r.f.Episodes[id]
	if e == nil || uuid == "" {
		return false
	}
	if autre, ok := r.parUUID[uuid]; ok && autre != id {
		return false
	}
	if e.UUID != "" {
		delete(r.parUUID, e.UUID)
	}
	e.UUID, e.Origine = uuid, "titre"
	r.parUUID[uuid] = id
	r.modifie = true
	return true
}

// Rapproche indique si le rattachement par titre a deja ete fait pour serie.
func (r *Registre) Rapproche(serie string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	_, ok := r.f.Rapproches[serie]
	return ok
}

// MarquerRapproche note que le rattachement par titre de serie est fait.
func (r *Registre) MarquerRapproche(serie string, t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	r.f.Rapproches[serie] = t.Unix()
	r.modifie = true
}

// Sauver ecrit le registre s'il a change.
func (r *Registre) Sauver() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.charge || !r.modifie {
		return nil
	}
	r.f.Version = 1
	d, err := json.MarshalIndent(r.f, "", " ")
	if err != nil {
		return err
	}
	if err := r.sauver(d); err != nil {
		return err
	}
	r.modifie = false
	return nil
}

// plan : ce que la lecture d'une serie doit faire de chaque episode.
type plan struct {
	connus      map[int]EntreeRF // index -> entree utilisable telle quelle
	indispo     map[int]bool     // index -> indisponible recent : ecarte
	aResoudre   []int            // index a resoudre par aod
	revalidions int
}

// planifier repartit les episodes (ids) entre connus, indisponibles recents et
// a resoudre. Sans registre, tout est a resoudre.
func (r *Registre) planifier(ids []string, t time.Time) plan {
	p := plan{connus: map[int]EntreeRF{}, indispo: map[int]bool{}}
	if r == nil {
		for i := range ids {
			p.aResoudre = append(p.aResoudre, i)
		}
		return p
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assurer()
	type vieux struct {
		i int
		v int64
	}
	var aRevalider []vieux
	for i, id := range ids {
		e := r.f.Episodes[id]
		switch {
		case e == nil:
			p.aResoudre = append(p.aResoudre, i)
		case e.Statut == StatutIndisponible:
			if t.Sub(time.Unix(e.Verifie, 0)) >= revoirIndisponible {
				p.aResoudre = append(p.aResoudre, i)
			} else {
				p.indispo[i] = true
			}
		case e.UUID == "" || e.URL == "":
			p.aResoudre = append(p.aResoudre, i)
		default:
			p.connus[i] = *e
			if t.Sub(time.Unix(e.Verifie, 0)) >= revaliderApres {
				aRevalider = append(aRevalider, vieux{i, e.Verifie})
			}
		}
	}
	// Revalidation au compte-gouttes : les plus anciennes d'abord.
	sort.Slice(aRevalider, func(a, b int) bool { return aRevalider[a].v < aRevalider[b].v })
	for k, v := range aRevalider {
		if k >= revalidationsParSerie {
			break
		}
		p.aResoudre = append(p.aResoudre, v.i)
		p.revalidions++
	}
	return p
}
