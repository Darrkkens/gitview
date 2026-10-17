package httpapi

import (
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
)

// shapes converts Go types into schemas following the encoding/json rules
// (field names from tags, "-", omitempty, ",string", promoted fields of
// embedded structs). Named structs are described once and referenced.
type shapes struct {
	p    *flow.Program
	defs map[string]*model.Shape
}

func newShapes(p *flow.Program, defs map[string]*model.Shape) *shapes {
	return &shapes{p: p, defs: defs}
}

// Rules the example generator and the readers of the contract understand.
var knownRules = map[string]bool{
	"required": true, "omitempty": true, "email": true, "url": true, "uri": true, "uuid": true, "uuid4": true,
	"min": true, "max": true, "len": true, "gte": true, "lte": true, "gt": true, "lt": true, "oneof": true,
	"numeric": true, "alpha": true, "alphanum": true, "datetime": true, "ip": true, "ipv4": true, "dive": true, "e164": true,
}

func typeKey(t types.Type, tag string) string {
	k := types.TypeString(t, nil)
	if tag != "json" {
		k += "#" + tag
	}
	return k
}

func shortType(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

// of describes t as encoded with the given tag key (json, form, uri,
// header, xml).
func (s *shapes) of(t types.Type, tag string) *model.Shape {
	return s.shape(t, tag, 0)
}

func hasMethod(t types.Type, name string) bool {
	for _, tt := range []types.Type{t, types.NewPointer(t)} {
		if _, ok := tt.Underlying().(*types.Interface); ok {
			continue
		}
		ms := types.NewMethodSet(tt)
		for i := 0; i < ms.Len(); i++ {
			if ms.At(i).Obj().Name() == name {
				return true
			}
		}
	}
	return false
}

func (s *shapes) shape(t types.Type, tag string, depth int) *model.Shape {
	if depth > 12 {
		return &model.Shape{Type: model.TypeUnknown, Partial: true, Note: "nested too deeply to describe"}
	}
	t = types.Unalias(t)
	goType := shortType(t)
	if n, ok := t.(*types.Named); ok {
		obj := n.Obj()
		path := ""
		if obj.Pkg() != nil {
			path = obj.Pkg().Path()
		}
		switch path + "." + obj.Name() {
		case "time.Time":
			return &model.Shape{Type: model.TypeString, Format: "date-time", GoType: goType}
		case "time.Duration":
			return &model.Shape{Type: model.TypeInteger, Format: "int64", GoType: goType, Note: "nanoseconds"}
		case "encoding/json.RawMessage":
			return &model.Shape{Type: model.TypeUnknown, GoType: goType, Note: "raw JSON, any value"}
		case "encoding/json.Number":
			return &model.Shape{Type: model.TypeNumber, GoType: goType}
		case "github.com/google/uuid.UUID", "github.com/gofrs/uuid.UUID", "github.com/satori/go.uuid.UUID":
			return &model.Shape{Type: model.TypeString, Format: "uuid", GoType: goType}
		}
		if tag == "json" && (hasMethod(t, "MarshalJSON") || hasMethod(t, "UnmarshalJSON")) {
			return &model.Shape{Type: model.TypeUnknown, GoType: goType, Partial: true,
				Note: "custom JSON encoding (" + goType + " implements MarshalJSON or UnmarshalJSON)"}
		}
		if hasMethod(t, "MarshalText") || hasMethod(t, "UnmarshalText") {
			return &model.Shape{Type: model.TypeString, GoType: goType, Note: "encoded as text by MarshalText"}
		}
		if _, isStruct := n.Underlying().(*types.Struct); isStruct {
			key := typeKey(t, tag)
			if _, ok := s.defs[key]; !ok {
				def := &model.Shape{Type: model.TypeObject, GoType: goType, Name: goType}
				def.File, def.Line, _ = s.p.Pos(obj.Pos())
				s.defs[key] = def // before the fields: recursive types refer to it
				st := s.shape(n.Underlying(), tag, depth+1)
				def.Fields, def.Partial, def.Note = st.Fields, st.Partial, st.Note
			}
			return &model.Shape{Type: model.TypeObject, Ref: key, Name: goType, GoType: goType}
		}
		sh := s.shape(n.Underlying(), tag, depth+1)
		c := *sh
		c.GoType = goType
		if consts := declaredConsts(n); len(consts) > 0 {
			c.Note = strings.TrimPrefix(c.Note+"; constants declared for "+goType+": "+strings.Join(consts, ", ")+" (decoding does not restrict to them)", "; ")
		}
		return &c
	}
	switch u := t.(type) {
	case *types.Basic:
		return basicShape(u, goType)
	case *types.Pointer:
		sh := *s.shape(u.Elem(), tag, depth+1)
		sh.Nullable = true
		return &sh
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte && tag == "json" {
			return &model.Shape{Type: model.TypeString, Format: "byte", GoType: goType, Note: "base64"}
		}
		return &model.Shape{Type: model.TypeArray, GoType: goType, Items: s.shape(u.Elem(), tag, depth+1), Nullable: true}
	case *types.Array:
		return &model.Shape{Type: model.TypeArray, GoType: goType, Items: s.shape(u.Elem(), tag, depth+1)}
	case *types.Map:
		return &model.Shape{Type: model.TypeObject, GoType: goType, Values: s.shape(u.Elem(), tag, depth+1), Nullable: true}
	case *types.Interface:
		return &model.Shape{Type: model.TypeUnknown, GoType: goType, Note: "interface value: any type"}
	case *types.Struct:
		return s.structShape(u, tag, depth)
	}
	return &model.Shape{Type: model.TypeUnknown, GoType: goType, Note: "not encodable as " + tag}
}

func basicShape(b *types.Basic, goType string) *model.Shape {
	switch {
	case b.Info()&types.IsBoolean != 0:
		return &model.Shape{Type: model.TypeBoolean, GoType: goType}
	case b.Info()&types.IsString != 0:
		return &model.Shape{Type: model.TypeString, GoType: goType}
	case b.Info()&types.IsInteger != 0:
		sh := &model.Shape{Type: model.TypeInteger, GoType: goType}
		switch b.Kind() {
		case types.Int32, types.Uint32, types.Int16, types.Uint16, types.Int8, types.Uint8:
			sh.Format = "int32"
		default:
			sh.Format = "int64"
		}
		return sh
	case b.Info()&types.IsFloat != 0:
		sh := &model.Shape{Type: model.TypeNumber, GoType: goType, Format: "double"}
		if b.Kind() == types.Float32 {
			sh.Format = "float"
		}
		return sh
	}
	return &model.Shape{Type: model.TypeUnknown, GoType: goType}
}

// declaredConsts lists the constants of a named type declared in its
// package, as written in the source (values, sorted).
func declaredConsts(n *types.Named) []string {
	pkg := n.Obj().Pkg()
	if pkg == nil {
		return nil
	}
	var out []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if ok && types.Identical(c.Type(), n) {
			out = append(out, c.Val().ExactString())
		}
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = append(out[:20], "…")
	}
	return out
}

func (s *shapes) structShape(st *types.Struct, tag string, depth int) *model.Shape {
	sh := &model.Shape{Type: model.TypeObject}
	seen := map[string]bool{}
	var walk func(st *types.Struct, embedded string, d int)
	walk = func(st *types.Struct, embedded string, d int) {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			tags := reflect.StructTag(st.Tag(i))
			val := tags.Get(tag)
			if val == "-" {
				continue
			}
			name, opts, _ := strings.Cut(val, ",")
			if f.Embedded() && name == "" {
				// Promoted fields of an embedded struct (encoding/json).
				et := f.Type()
				if p, ok := et.(*types.Pointer); ok {
					et = p.Elem()
				}
				if es, ok := et.Underlying().(*types.Struct); ok && d < 6 {
					walk(es, shortType(et), d+1)
					continue
				}
			}
			if !f.Exported() {
				continue
			}
			if name == "" {
				name = f.Name()
			}
			if seen[name] {
				continue // the shallower field wins
			}
			seen[name] = true
			fs := s.shape(f.Type(), tag, depth+1)
			if strings.Contains(","+opts+",", ",string,") && tag == "json" {
				fs = &model.Shape{Type: model.TypeString, GoType: fs.GoType, Note: "encoded as a string (,string option)"}
			}
			fl := model.Field{Name: name, GoName: f.Name(), Schema: fs, Embedded: embedded,
				OmitEmpty: strings.Contains(","+opts+",", ",omitempty,") || strings.Contains(","+opts+",", ",omitzero,")}
			fl.File, fl.Line, _ = s.p.Pos(f.Pos())
			for _, key := range []string{"binding", "validate"} {
				if v, ok := tags.Lookup(key); ok {
					fl.Rules = append(fl.Rules, parseRules(v, key)...)
				}
			}
			sh.Fields = append(sh.Fields, fl)
		}
	}
	walk(st, "", 0)
	return sh
}

// parseRules reads a go-playground/validator tag ("required,min=2,oneof=a b").
func parseRules(tag, source string) []model.Rule {
	var out []model.Rule
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, param, _ := strings.Cut(part, "=")
		out = append(out, model.Rule{Name: name, Param: param, Source: source, Known: knownRules[name]})
	}
	return out
}

// resolve returns the definition a shape refers to, or the shape itself.
func resolve(defs map[string]*model.Shape, sh *model.Shape) *model.Shape {
	if sh != nil && sh.Ref != "" {
		if d := defs[sh.Ref]; d != nil {
			return d
		}
	}
	return sh
}
