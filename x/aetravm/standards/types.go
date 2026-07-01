package standards

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const CanonicalVersion = "avm-stdlib/v1"

type TypeDescriptor struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Kind        string `json:"kind"`
	Arity       int    `json:"arity"`
	Description string `json:"description"`
}

type Registry struct {
	Version string           `json:"version"`
	Types   []TypeDescriptor `json:"types"`
}

func DefaultRegistry() Registry {
	types := []TypeDescriptor{
		{Name: "Address", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "chain-bound account or contract address"},
		{Name: "Coins", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "non-negative coin amount in base units"},
		{Name: "Cell", Version: CanonicalVersion, Kind: "cell", Arity: 0, Description: "canonical chunk/cell payload root"},
		{Name: "Slice", Version: CanonicalVersion, Kind: "cell", Arity: 0, Description: "bounded view over a Cell payload"},
		{Name: "Ref", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "typed reference to an out-of-line payload"},
		{Name: "Result", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "success or error value"},
		{Name: "Option", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "optional value"},
		{Name: "List", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "bounded canonical list"},
		{Name: "Map", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "deterministically ordered key/value map"},
	}
	sort.Slice(types, func(i, j int) bool { return strings.ToLower(types[i].Name) < strings.ToLower(types[j].Name) })
	return Registry{Version: CanonicalVersion, Types: types}
}

func (r Registry) Canonical() Registry {
	out := Registry{
		Version: strings.TrimSpace(r.Version),
		Types:   append([]TypeDescriptor(nil), r.Types...),
	}
	sort.SliceStable(out.Types, func(i, j int) bool {
		left := out.Types[i]
		right := out.Types[j]
		if strings.ToLower(left.Name) != strings.ToLower(right.Name) {
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Arity != right.Arity {
			return left.Arity < right.Arity
		}
		return left.Description < right.Description
	})
	return out
}

func (r Registry) Find(name string) (TypeDescriptor, bool) {
	for _, typ := range r.Types {
		if strings.EqualFold(typ.Name, name) {
			return typ, true
		}
	}
	return TypeDescriptor{}, false
}

func (r Registry) Validate(name string, arity int) error {
	typ, ok := r.Find(name)
	if !ok {
		return fmt.Errorf("unknown standard type %q", name)
	}
	if typ.Arity != arity {
		return fmt.Errorf("standard type %q requires %d type arguments", typ.Name, typ.Arity)
	}
	return nil
}

func (r Registry) Hash() [32]byte {
	payload, _ := json.Marshal(r.Canonical())
	return sha256.Sum256(payload)
}
