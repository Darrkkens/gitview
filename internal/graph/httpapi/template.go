package httpapi

import "strings"

// Template converts a registered pattern to an OpenAPI path template:
// gin/echo ":id" and "*path", chi "{id:[0-9]+}" and ServeMux "{path...}"
// all become "{id}"; ServeMux's "{$}" end anchor is dropped.
func Template(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		switch {
		case strings.HasPrefix(s, ":") && len(s) > 1:
			segs[i] = "{" + s[1:] + "}"
		case strings.HasPrefix(s, "*") && len(s) > 1:
			segs[i] = "{" + s[1:] + "}"
		case s == "{$}":
			segs[i] = ""
		case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
			name := s[1 : len(s)-1]
			if j := strings.IndexByte(name, ':'); j >= 0 {
				name = name[:j]
			}
			segs[i] = "{" + strings.TrimSuffix(name, "...") + "}"
		}
	}
	out := strings.Join(segs, "/")
	if len(out) > 1 {
		out = strings.TrimSuffix(out, "/")
		if strings.HasSuffix(path, "/") && !strings.HasSuffix(path, "{$}") {
			out += "/"
		}
	}
	return out
}

// PathParams lists the placeholders of a registered pattern, in order.
// "{dynamic}" marks a path computed at runtime and is not a parameter.
func PathParams(path string) []string {
	var out []string
	for _, s := range strings.Split(Template(path), "/") {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && s != "{dynamic}" && s != "{unknown}" {
			out = append(out, s[1:len(s)-1])
		}
	}
	return out
}
