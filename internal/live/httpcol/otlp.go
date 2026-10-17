package httpcol

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/pbwire"
)

// OTelSpan is an OpenTelemetry span received from the application, with
// only the attributes GitView uses.
type OTelSpan struct {
	TraceID  string            `json:"traceId"`
	SpanID   string            `json:"spanId"`
	ParentID string            `json:"parentId,omitempty"`
	Name     string            `json:"name"`
	Kind     int               `json:"kind"` // 1 internal, 2 server, 3 client, 4 producer, 5 consumer
	Start    time.Time         `json:"start"`
	End      time.Time         `json:"end"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Error    bool              `json:"error,omitempty"`
	PID      int               `json:"pid,omitempty"` // resource attribute process.pid
	Service  string            `json:"service,omitempty"`
}

// keptAttrs are the span attributes kept; everything else (headers, user
// identifiers, peer addresses…) is dropped on reception.
var keptAttrs = map[string]bool{
	"http.request.method": true, "http.method": true,
	"http.route": true, "url.path": true, "http.target": true,
	"http.response.status_code": true, "http.status_code": true,
	"db.statement": true, "db.query.text": true, "db.system": true, "db.system.name": true, "db.operation": true, "db.operation.name": true,
	"code.filepath": true, "code.file.path": true, "code.lineno": true, "code.line.number": true,
	"code.function": true, "code.function.name": true, "code.namespace": true,
}

func keepAttr(m map[string]string, k, v string) map[string]string {
	if !keptAttrs[k] {
		return m
	}
	if m == nil {
		m = map[string]string{}
	}
	switch k {
	case "db.statement", "db.query.text":
		v = RedactSQL(v)
	case "http.target", "url.path":
		if i := strings.IndexAny(v, "?#"); i >= 0 {
			v = v[:i]
		}
		v = MaskPath(v)
	}
	m[k] = v
	return m
}

// OTLPReceiver accepts OTLP/HTTP trace exports (protobuf or JSON) on a
// loopback address.
type OTLPReceiver struct {
	ln   net.Listener
	srv  *http.Server
	sink func([]OTelSpan)
}

// DefaultOTLPAddr is the standard OTLP/HTTP address.
const DefaultOTLPAddr = "127.0.0.1:4318"

// StartOTLP starts the receiver. Point the application's exporter at it
// (OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318).
func StartOTLP(listen string, sink func([]OTelSpan)) (*OTLPReceiver, error) {
	if listen == "" {
		listen = DefaultOTLPAddr
	}
	if err := checkLoopback("the OTLP receiver address", listen); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	o := &OTLPReceiver{ln: ln, sink: sink}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", o.handle)
	o.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go o.srv.Serve(ln)
	return o, nil
}

// Addr is the receiver's address.
func (o *OTLPReceiver) Addr() string { return o.ln.Addr().String() }

// Close stops the receiver.
func (o *OTLPReceiver) Close() error { return o.srv.Close() }

func (o *OTLPReceiver) handle(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !IsLoopbackHost(host) {
		http.Error(w, "loopback clients only", http.StatusForbidden)
		return
	}
	var body io.Reader = http.MaxBytesReader(w, r.Body, 16<<20)
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body = io.LimitReader(zr, 64<<20)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	isJSON := strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
	var spans []OTelSpan
	if isJSON {
		spans, err = DecodeOTLPJSON(data)
	} else {
		spans, err = DecodeOTLP(data)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(spans) > 0 && o.sink != nil {
		o.sink(spans)
	}
	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

// DecodeOTLP decodes an ExportTraceServiceRequest (protobuf).
func DecodeOTLP(b []byte) ([]OTelSpan, error) {
	var out []OTelSpan
	err := pbwire.Fields(b, func(num, wt int, _ uint64, rs []byte) error {
		if num != 1 || wt != 2 {
			return nil
		}
		var res resource
		var spans [][]byte
		err := pbwire.Fields(rs, func(num, wt int, _ uint64, data []byte) error {
			switch {
			case num == 1 && wt == 2:
				return pbwire.Fields(data, func(num, wt int, _ uint64, kv []byte) error {
					if num == 1 && wt == 2 {
						k, v, err := decodeKV(kv)
						if err != nil {
							return err
						}
						res.set(k, v)
					}
					return nil
				})
			case num == 2 && wt == 2:
				return pbwire.Fields(data, func(num, wt int, _ uint64, sp []byte) error {
					if num == 2 && wt == 2 {
						spans = append(spans, sp)
					}
					return nil
				})
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, sp := range spans {
			s, err := decodeSpan(sp)
			if err != nil {
				return err
			}
			s.PID, s.Service = res.pid, res.service
			out = append(out, s)
		}
		return nil
	})
	return out, err
}

type resource struct {
	pid     int
	service string
}

func (r *resource) set(k, v string) {
	switch k {
	case "process.pid":
		r.pid, _ = strconv.Atoi(v)
	case "service.name":
		r.service = v
	}
}

func decodeSpan(b []byte) (OTelSpan, error) {
	var s OTelSpan
	err := pbwire.Fields(b, func(num, wt int, v uint64, data []byte) error {
		switch num {
		case 1:
			s.TraceID = hex.EncodeToString(data)
		case 2:
			s.SpanID = hex.EncodeToString(data)
		case 4:
			if len(data) > 0 {
				s.ParentID = hex.EncodeToString(data)
			}
		case 5:
			s.Name = string(data)
		case 6:
			s.Kind = int(v)
		case 7:
			s.Start = time.Unix(0, int64(v))
		case 8:
			s.End = time.Unix(0, int64(v))
		case 9:
			if wt == 2 {
				k, val, err := decodeKV(data)
				if err != nil {
					return err
				}
				s.Attrs = keepAttr(s.Attrs, k, val)
			}
		case 15:
			return pbwire.Fields(data, func(num, _ int, v uint64, _ []byte) error {
				if num == 3 && v == 2 {
					s.Error = true
				}
				return nil
			})
		}
		return nil
	})
	return s, err
}

func decodeKV(b []byte) (string, string, error) {
	var k, v string
	err := pbwire.Fields(b, func(num, wt int, _ uint64, data []byte) error {
		switch {
		case num == 1 && wt == 2:
			k = string(data)
		case num == 2 && wt == 2:
			return pbwire.Fields(data, func(num, wt int, x uint64, d []byte) error {
				switch num {
				case 1:
					v = string(d)
				case 2:
					v = strconv.FormatBool(x != 0)
				case 3:
					v = strconv.FormatInt(int64(x), 10)
				case 4:
					v = strconv.FormatFloat(math.Float64frombits(x), 'g', -1, 64)
				}
				return nil
			})
		}
		return nil
	})
	return k, v, err
}

// DecodeOTLPJSON decodes the JSON encoding of an ExportTraceServiceRequest.
func DecodeOTLPJSON(b []byte) ([]OTelSpan, error) {
	type anyValue struct {
		StringValue *string         `json:"stringValue"`
		BoolValue   *bool           `json:"boolValue"`
		IntValue    json.RawMessage `json:"intValue"`
		DoubleValue *float64        `json:"doubleValue"`
	}
	type kv struct {
		Key   string   `json:"key"`
		Value anyValue `json:"value"`
	}
	type span struct {
		TraceID      string          `json:"traceId"`
		SpanID       string          `json:"spanId"`
		ParentSpanID string          `json:"parentSpanId"`
		Name         string          `json:"name"`
		Kind         int             `json:"kind"`
		Start        json.RawMessage `json:"startTimeUnixNano"`
		End          json.RawMessage `json:"endTimeUnixNano"`
		Attributes   []kv            `json:"attributes"`
		Status       struct {
			Code int `json:"code"`
		} `json:"status"`
	}
	var req struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []kv `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []span `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(b, &req); err != nil {
		return nil, err
	}
	str := func(v anyValue) string {
		switch {
		case v.StringValue != nil:
			return *v.StringValue
		case v.BoolValue != nil:
			return strconv.FormatBool(*v.BoolValue)
		case len(v.IntValue) > 0:
			return strings.Trim(string(v.IntValue), `"`)
		case v.DoubleValue != nil:
			return strconv.FormatFloat(*v.DoubleValue, 'g', -1, 64)
		}
		return ""
	}
	nanos := func(raw json.RawMessage) time.Time {
		n, _ := strconv.ParseInt(strings.Trim(string(raw), `"`), 10, 64)
		return time.Unix(0, n)
	}
	var out []OTelSpan
	for _, rs := range req.ResourceSpans {
		var res resource
		for _, a := range rs.Resource.Attributes {
			res.set(a.Key, str(a.Value))
		}
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				s := OTelSpan{
					TraceID: strings.ToLower(sp.TraceID), SpanID: strings.ToLower(sp.SpanID), ParentID: strings.ToLower(sp.ParentSpanID),
					Name: sp.Name, Kind: sp.Kind, Start: nanos(sp.Start), End: nanos(sp.End),
					Error: sp.Status.Code == 2, PID: res.pid, Service: res.service,
				}
				for _, a := range sp.Attributes {
					s.Attrs = keepAttr(s.Attrs, a.Key, str(a.Value))
				}
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// cleanPath drops the query string and masks the path (again: spans may
// come from elsewhere than DecodeOTLP).
func cleanPath(v string) string {
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	return MaskPath(v)
}

func attr(s OTelSpan, keys ...string) string {
	for _, k := range keys {
		if v := s.Attrs[k]; v != "" {
			return v
		}
	}
	return ""
}

// AssembleOTel turns spans into requests: every server span with an HTTP
// method whose ancestors include no other such span becomes a request (a
// trace may hold several), and the spans below it carrying a database
// statement become its SQL statements. Spans of another process than pid
// are ignored; so are spans that do not say which process sent them
// (resource attribute process.pid) unless acceptUnidentified is set. It
// returns how many spans were ignored for lack of a process ID.
func AssembleOTel(spans []OTelSpan, pid int, acceptUnidentified bool) (out []Request, unidentified int) {
	byTrace := map[string][]OTelSpan{}
	var order []string
	for _, s := range spans {
		if s.TraceID == "" || pid != 0 && s.PID != 0 && s.PID != pid {
			continue
		}
		if s.PID == 0 && pid != 0 && !acceptUnidentified {
			unidentified++
			continue
		}
		if _, ok := byTrace[s.TraceID]; !ok {
			order = append(order, s.TraceID)
		}
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}
	for _, trace := range order {
		list := byTrace[trace]
		bySpan := map[string]*OTelSpan{}
		for i := range list {
			bySpan[list[i].SpanID] = &list[i]
		}
		isServer := func(s *OTelSpan) bool {
			return s.Kind == 2 && attr(*s, "http.request.method", "http.method") != ""
		}
		// serverOf returns the outermost server span above s (s included).
		serverOf := func(s *OTelSpan) *OTelSpan {
			var top *OTelSpan
			for cur, n := s, 0; cur != nil && n < 64; cur, n = bySpan[cur.ParentID], n+1 {
				if isServer(cur) {
					top = cur
				}
			}
			return top
		}
		var servers []*OTelSpan
		for i := range list {
			if s := &list[i]; isServer(s) && serverOf(s) == s {
				servers = append(servers, s)
			}
		}
		sort.SliceStable(servers, func(i, j int) bool { return servers[i].Start.Before(servers[j].Start) })
		reqs := map[*OTelSpan]*Request{}
		for _, sv := range servers {
			id := trace
			if len(servers) > 1 {
				id = trace + "-" + sv.SpanID
			}
			req := &Request{
				ID:        id,
				W3C:       trace,
				Sources:   []Source{SourceOTel},
				Method:    attr(*sv, "http.request.method", "http.method"),
				Route:     attr(*sv, "http.route"),
				Path:      cleanPath(attr(*sv, "url.path", "http.target")),
				Start:     sv.Start,
				HasServer: true,
				ServerDur: sv.End.Sub(sv.Start),
			}
			req.Status, _ = strconv.Atoi(attr(*sv, "http.response.status_code", "http.status_code"))
			reqs[sv] = req
		}
		for i := range list {
			s := &list[i]
			q := attr(*s, "db.query.text", "db.statement")
			if q == "" {
				continue
			}
			owner := serverOf(s)
			if owner == nil {
				// Broken parent chain: the request whose window contains it.
				for _, sv := range servers {
					if !s.Start.Before(sv.Start) && !s.End.After(sv.End) {
						if owner != nil {
							owner = nil
							break
						}
						owner = sv
					}
				}
			}
			req := reqs[owner]
			if req == nil {
				continue
			}
			line, _ := strconv.Atoi(attr(*s, "code.line.number", "code.lineno"))
			fn := attr(*s, "code.function.name", "code.function")
			if ns := s.Attrs["code.namespace"]; ns != "" && fn != "" && !strings.Contains(fn, ".") {
				fn = ns + "." + fn
			}
			sp := SQLSpan{
				Query: RedactSQL(q), Start: s.Start.Sub(owner.Start), Dur: s.End.Sub(s.Start),
				File: attr(*s, "code.file.path", "code.filepath"), Line: line, Func: fn, Source: SourceOTel,
			}
			if s.Error {
				sp.Err = "error"
			}
			req.SQL = append(req.SQL, sp)
		}
		for _, sv := range servers {
			req := reqs[sv]
			sort.Slice(req.SQL, func(i, j int) bool { return req.SQL[i].Start < req.SQL[j].Start })
			req.SetKey()
			out = append(out, *req)
		}
	}
	return out, unidentified
}
