package app

import (
	"encoding/json"
	"fmt"
)

// ErreurIHM : erreur destinee a l'utilisateur, traduite par l'IHM (cle i18n +
// parametres) au lieu d'une phrase en dur. Transmise au front comme
// "ihm:{json}" (texte de l'erreur Wails) ; msgErreur (composants.js) la
// traduit. Les parametres dont le nom finit par "Octets" sont formates en
// taille lisible cote front.
type ErreurIHM struct {
	Cle    string         `json:"cle"`
	Params map[string]any `json:"params,omitempty"`
}

func (e *ErreurIHM) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		return e.Cle
	}
	return "ihm:" + string(b)
}

// errIHM construit une ErreurIHM ; kv alterne nom et valeur des parametres.
func errIHM(cle string, kv ...any) error {
	e := &ErreurIHM{Cle: cle}
	for i := 0; i+1 < len(kv); i += 2 {
		if e.Params == nil {
			e.Params = map[string]any{}
		}
		e.Params[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return e
}

// errDetail : ErreurIHM portant le detail technique d'une erreur sous-jacente
// (affiche tel quel, non traduit).
func errDetail(cle string, err error) error {
	return errIHM(cle, "detail", err.Error())
}
