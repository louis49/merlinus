package radiofrance

import (
	"encoding/json"
	"reflect"
	"testing"
)

// devalue aplatit une valeur Go au format "devalue" de SvelteKit tel que le lit
// resoudre : un tableau plat ou chaque objet/tableau reference ses enfants par
// leur index (nombre), la racine etant a l'index 0.
func devalue(racine any) []any {
	var arr []any
	var ajout func(v any) int
	ajout = func(v any) int {
		i := len(arr)
		arr = append(arr, nil)
		switch x := v.(type) {
		case map[string]any:
			m := make(map[string]any, len(x))
			for k, y := range x {
				m[k] = float64(ajout(y))
			}
			arr[i] = m
		case []any:
			s := make([]any, len(x))
			for k, y := range x {
				s[k] = float64(ajout(y))
			}
			arr[i] = s
		default:
			arr[i] = x
		}
		return i
	}
	ajout(racine)
	return arr
}

// pageData enveloppe des racines devalue dans un __data.json SvelteKit
// ({"type":"data","nodes":[...]}), avec un noeud null en tete comme sur le site.
func pageData(t *testing.T, racines ...any) string {
	t.Helper()
	nodes := []any{nil}
	for _, r := range racines {
		nodes = append(nodes, map[string]any{"type": "data", "data": devalue(r)})
	}
	b, err := json.Marshal(map[string]any{"type": "data", "nodes": nodes})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Format reel, ecrit a la main : les valeurs numeriques des champs sont des
// index dans le tableau plat ; les litteraux (chaines, nombres) sont des cases.
func TestResoudreFormatBrut(t *testing.T) {
	var arr []any
	brut := `[{"titre":1,"liste":2,"duree":5,"vide":-1},"Bestioles",[3,4],{"nom":1},"seul",754]`
	if err := json.Unmarshal([]byte(brut), &arr); err != nil {
		t.Fatal(err)
	}
	got := resoudre(arr, 0, map[int]any{}, map[int]bool{})
	attendu := map[string]any{
		"titre": "Bestioles",
		"liste": []any{map[string]any{"nom": "Bestioles"}, "seul"},
		"duree": 754.0,
		"vide":  nil, // index negatif (undefined devalue) : nil
	}
	if !reflect.DeepEqual(got, attendu) {
		t.Fatalf("resoudre = %#v\nattendu %#v", got, attendu)
	}
}

// Cycles et index hors limites : pas de boucle infinie ni de panique.
func TestResoudreCyclesEtBornes(t *testing.T) {
	var arr []any
	// 0 -> {self:0, autre:1}, 1 -> [0, 99]
	if err := json.Unmarshal([]byte(`[{"self":0,"autre":1},[0,99]]`), &arr); err != nil {
		t.Fatal(err)
	}
	got, ok := resoudre(arr, 0, map[int]any{}, map[int]bool{}).(map[string]any)
	if !ok {
		t.Fatalf("racine non objet")
	}
	if got["self"] != nil {
		t.Errorf("auto-reference : %v, attendu nil", got["self"])
	}
	autre, _ := got["autre"].([]any)
	if len(autre) != 2 || autre[0] != nil || autre[1] != nil {
		t.Errorf("autre = %#v, attendu [nil nil]", autre)
	}
	if resoudre(arr, 5, map[int]any{}, map[int]bool{}) != nil || resoudre(arr, -2, map[int]any{}, map[int]bool{}) != nil {
		t.Error("index hors limites : nil attendu")
	}
	if resoudre(nil, 0, map[int]any{}, map[int]bool{}) != nil {
		t.Error("tableau vide : nil attendu")
	}
}

// Une meme case referencee deux fois est resolue une seule fois (memo) et
// partagee : le parcours la visite autant de fois qu'elle est referencee.
func TestResoudreMemoEtParcourir(t *testing.T) {
	var arr []any
	if err := json.Unmarshal([]byte(`[[1,1,2],{"type":4},{"x":3},{"type":5},"a","b"]`), &arr); err != nil {
		t.Fatal(err)
	}
	memo := map[int]any{}
	racine := resoudre(arr, 0, memo, map[int]bool{})
	l := racine.([]any)
	if reflect.ValueOf(l[0]).Pointer() != reflect.ValueOf(l[1]).Pointer() {
		t.Error("case partagee non memorisee")
	}
	var types []string
	parcourir(racine, func(o map[string]any) {
		if s, ok := o["type"].(string); ok {
			types = append(types, s)
		}
	})
	// "a" deux fois (ref partagee), "b" une fois (imbrique sous x).
	n := map[string]int{}
	for _, s := range types {
		n[s]++
	}
	if n["a"] != 2 || n["b"] != 1 {
		t.Errorf("objets visites : %v", types)
	}
	parcourir(nil, func(map[string]any) { t.Error("nil ne doit rien visiter") })
	parcourir("chaine", func(map[string]any) { t.Error("scalaire ne doit rien visiter") })
}

// L'encodeur de test produit bien ce que resoudre sait relire.
func TestDevalueAllerRetour(t *testing.T) {
	v := map[string]any{"a": []any{"x", map[string]any{"b": 2.0}}, "c": "y"}
	b, _ := json.Marshal(devalue(v))
	var arr []any
	_ = json.Unmarshal(b, &arr)
	if got := resoudre(arr, 0, map[int]any{}, map[int]bool{}); !reflect.DeepEqual(got, v) {
		t.Fatalf("aller-retour : %#v", got)
	}
}
