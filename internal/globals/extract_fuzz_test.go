package globals

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// fuzzValue builds a cty value from raw bytes: nulls, scalars, lists, sets,
// tuples (including mixed element types), maps and objects. Unknown values
// are left out: Terramate fails global evaluation instead of producing them.
func fuzzValue(raw []byte, depth int) (cty.Value, []byte) {
	if len(raw) == 0 {
		return cty.StringVal(""), raw
	}
	kind, raw := raw[0]%14, raw[1:]
	elems := func() []cty.Value {
		n := 0
		if len(raw) > 0 {
			n, raw = int(raw[0]%4), raw[1:]
		}
		vals := make([]cty.Value, 0, n)
		for i := 0; i < n && depth < 3; i++ {
			var v cty.Value
			v, raw = fuzzValue(raw, depth+1)
			vals = append(vals, v)
		}
		return vals
	}
	switch kind {
	case 0:
		return cty.NullVal(cty.DynamicPseudoType), raw
	case 1:
		return cty.NullVal(cty.String), raw
	case 2:
		return cty.NullVal(cty.List(cty.String)), raw
	case 3:
		return cty.True, raw
	case 4:
		return cty.NumberIntVal(int64(len(raw))), raw
	case 5:
		return cty.StringVal(string(raw[:min(len(raw), 3)])), raw
	case 6:
		return cty.ListVal([]cty.Value{cty.StringVal("a"), cty.NullVal(cty.String)}), raw
	case 7:
		return cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.NumberIntVal(1)}), raw
	case 8:
		return cty.SetValEmpty(cty.String), raw
	case 9:
		return cty.SetVal([]cty.Value{cty.StringVal("b"), cty.StringVal("a")}), raw
	case 10:
		return cty.TupleVal(elems()), raw
	case 11:
		return cty.ListValEmpty(cty.String), raw
	case 12:
		return cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")}), raw
	default:
		return cty.ObjectVal(map[string]cty.Value{"k": cty.True}), raw
	}
}

// FuzzExtractAtlantisGlobals checks that no global value makes extraction
// panic: it either returns an error or valid overrides.
func FuzzExtractAtlantisGlobals(f *testing.F) {
	for i := byte(0); i < 14; i++ {
		f.Add([]byte{i, i, 2, 5, 3})
	}

	keys := append(append([]string{}, KnownGlobals...), "atlantis_typo", "other")
	f.Fuzz(func(t *testing.T, raw []byte) {
		globals := map[string]cty.Value{}
		for _, key := range keys {
			var v cty.Value
			v, raw = fuzzValue(raw, 0)
			globals[key] = v
		}
		_, _ = ExtractAtlantisGlobals(globals)
	})
}
