package model

import "github.com/athos/gitview/internal/evidence"

// HTTP is the HTTP catalog of the dataset: the routes found in the code and
// the contracts read from their handler chains by static analysis. Routes
// are the "route:" nodes of the graph; this adds what the handler expects
// and answers. Every element carries the evidence it was read from, and
// what could not be determined is marked partial or unknown instead of
// being filled in.
type HTTP struct {
	Routes  []*Route          `json:"routes"`
	Schemas map[string]*Shape `json:"schemas"` // named Go types referenced by Shape.Ref
	Servers []Server          `json:"servers,omitempty"`
	Notes   []string          `json:"notes,omitempty"`
}

// Server is a listen address found in the code (ListenAndServe, Run,
// http.Server{Addr: ...}).
type Server struct {
	Addr     string            `json:"addr"`          // as written, e.g. ":8080"
	URL      string            `json:"url,omitempty"` // http://localhost:8080 when the port is constant
	Dynamic  bool              `json:"dynamic,omitempty"`
	Evidence evidence.Evidence `json:"evidence"`
}

// Analysis status of a contract.
const (
	StatusComplete = "complete"
	StatusPartial  = "partial"
	StatusUnknown  = "unknown"
)

// Response bases.
const (
	BasisStatic  = "static"  // identified in the code
	BasisPartial = "partial" // status or body identified, the other not
	BasisUnknown = "unknown"
	// BasisObserved is reserved for responses recorded while the
	// application ran (Runtime Inspector); static analysis never sets it.
	BasisObserved = "observed"
)

type Route struct {
	ID        string `json:"id"`  // graph node ID (registration site included)
	Key       string `json:"key"` // "METHOD path", for humans and lookups
	Method    string `json:"method"`
	Path      string `json:"path"`     // as registered (gin :id, chi {id})
	Template  string `json:"template"` // OpenAPI path template (/users/{id})
	Framework string `json:"framework"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Group     string `json:"group"` // first constant path segments, for the catalog

	Handler     *ChainEntry  `json:"handler,omitempty"`
	Middlewares []ChainEntry `json:"middlewares,omitempty"`

	Params    []Param    `json:"params,omitempty"`
	Body      *Body      `json:"body,omitempty"`
	Responses []Response `json:"responses,omitempty"`
	Auth      *Auth      `json:"auth,omitempty"`
	Example   *Example   `json:"example,omitempty"`

	Status          string   `json:"status"` // complete | partial | unknown
	Reasons         []string `json:"reasons,omitempty"`
	UnresolvedCalls int      `json:"unresolvedCalls"` // in the handler chain, within the analysed depth

	Annotations []Annotation `json:"annotations,omitempty"`
}

// ChainEntry is a middleware or the handler of a route.
type ChainEntry struct {
	ID         string `json:"id,omitempty"` // graph node ID; empty when unresolved
	Name       string `json:"name"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Confidence string `json:"confidence"`
	Scope      string `json:"scope,omitempty"`     // global | group | router | route
	AddedFile  string `json:"addedFile,omitempty"` // the Use, Group or registration call
	AddedLine  int    `json:"addedLine,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Param is a path, query, header, cookie or form input the code reads.
type Param struct {
	Name     string `json:"name"`
	In       string `json:"in"`       // path | query | header | cookie | form
	Required string `json:"required"` // yes | no | unknown
	Schema   *Shape `json:"schema,omitempty"`
	Default  string `json:"default,omitempty"`
	Source   string `json:"source"`         // the call that reads it, e.g. "c.Query"
	From     string `json:"from,omitempty"` // the middleware or handler that reads it
	// InPattern tells, for path parameters, whether the route pattern has
	// the placeholder.
	InPattern  *bool               `json:"inPattern,omitempty"`
	Confidence string              `json:"confidence"`
	Evidence   []evidence.Evidence `json:"evidence"`
	Note       string              `json:"note,omitempty"`
}

// Body is the request body the code decodes.
type Body struct {
	ContentTypes []string `json:"contentTypes"`
	Schema       *Shape   `json:"schema,omitempty"`
	Source       string   `json:"source"` // ShouldBindJSON, json.Decoder.Decode...
	// Enforces lists the tag families the decoding applies (gin binding
	// tags with ShouldBind*, validate tags when a validator is called).
	Enforces        []string            `json:"enforces,omitempty"`
	DisallowUnknown bool                `json:"disallowUnknown,omitempty"`
	Confidence      string              `json:"confidence"`
	Evidence        []evidence.Evidence `json:"evidence"`
	Notes           []string            `json:"notes,omitempty"`
}

// Response is a status (and body) the handler chain may answer with.
type Response struct {
	Status      int    `json:"status"` // 0 when not determinable
	StatusText  string `json:"statusText,omitempty"`
	Implicit    bool   `json:"implicit,omitempty"` // net/http's 200 when nothing called WriteHeader
	ContentType string `json:"contentType,omitempty"`
	Schema      *Shape `json:"schema,omitempty"`
	Source      string `json:"source"`           // c.JSON, http.Error, WriteHeader + Encode...
	From        string `json:"from"`             // handler or middleware that answers
	FromID      string `json:"fromId,omitempty"` // its graph node
	// Via lists the helper functions between From and the call that writes
	// the response.
	Via   []string `json:"via,omitempty"`
	Basis string   `json:"basis"` // static | partial | unknown | observed
	// Example is a synthetic body built from Schema (never a recorded
	// response).
	Example    string              `json:"example,omitempty"`
	Confidence string              `json:"confidence"`
	Evidence   []evidence.Evidence `json:"evidence"`
	Note       string              `json:"note,omitempty"`
}

// Auth describes authentication recognised in the chain: a middleware (or
// the handler) that reads a credential and can answer 401.
type Auth struct {
	Recognized bool                `json:"recognized"`
	Scheme     string              `json:"scheme,omitempty"`     // bearer | basic | cookie | header
	Credential string              `json:"credential,omitempty"` // "Authorization header", "cookie session"
	By         string              `json:"by,omitempty"`         // the middleware
	Evidence   []evidence.Evidence `json:"evidence"`
	Note       string              `json:"note,omitempty"`
}

// Example is a synthetic request built from the contract. Values are
// placeholders chosen from the declared types and recognised validation
// rules, never data read from anywhere.
type Example struct {
	Body       string   `json:"body,omitempty"` // indented JSON
	Curl       string   `json:"curl"`
	BaseURL    string   `json:"baseUrl"`
	BaseURLSrc string   `json:"baseUrlSource"` // where the base URL comes from, or "placeholder"
	Valid      string   `json:"valid"`         // compatible | unverified
	Notes      []string `json:"notes,omitempty"`
}

// Shape types.
const (
	TypeObject  = "object"
	TypeArray   = "array"
	TypeString  = "string"
	TypeInteger = "integer"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeUnknown = "unknown"
)

// Shape is the schema of a value. A named struct is described once in
// HTTP.Schemas and referenced by Ref.
type Shape struct {
	Type     string   `json:"type"`
	Format   string   `json:"format,omitempty"`
	GoType   string   `json:"goType,omitempty"`
	Ref      string   `json:"ref,omitempty"`
	Name     string   `json:"name,omitempty"` // display name of a named schema
	Fields   []Field  `json:"fields,omitempty"`
	Items    *Shape   `json:"items,omitempty"`
	Values   *Shape   `json:"values,omitempty"` // map values
	Nullable bool     `json:"nullable,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	// Partial: more may be present than described (keys copied from
	// another map, a custom marshaller); Note says why.
	Partial bool   `json:"partial,omitempty"`
	Note    string `json:"note,omitempty"`
	File    string `json:"file,omitempty"` // declaration of a named type
	Line    int    `json:"line,omitempty"`
}

// Field is a member of an object schema.
type Field struct {
	Name      string `json:"name"` // as encoded (tag name)
	GoName    string `json:"goName,omitempty"`
	Schema    *Shape `json:"schema"`
	OmitEmpty bool   `json:"omitEmpty,omitempty"`
	Rules     []Rule `json:"rules,omitempty"`
	// Embedded names the embedded struct the field is promoted from.
	Embedded string `json:"embedded,omitempty"`
	// Conditional: the key is only written on some paths (map literals).
	Conditional bool   `json:"conditional,omitempty"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// Rule is a validation rule declared in a struct tag.
type Rule struct {
	Name   string `json:"name"` // required, email, min, oneof...
	Param  string `json:"param,omitempty"`
	Source string `json:"source"`     // binding | validate (the tag key)
	Known  bool   `json:"recognized"` // the example generator understands it
}

// Required reports whether a field is required by a rule of one of the
// given tag families (the ones the decoding applies).
func (f Field) Required(enforced []string) bool {
	for _, r := range f.Rules {
		if r.Name != "required" {
			continue
		}
		for _, e := range enforced {
			if r.Source == e {
				return true
			}
		}
	}
	return false
}
