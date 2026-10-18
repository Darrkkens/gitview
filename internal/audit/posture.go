package audit

import (
	"github.com/athos/gitview/internal/graph/model"
)

// posture summarises every route: authentication and authorization checks
// identified in its chain, what its database operations look like, the
// calls the analysis could not resolve, and the findings it may reach.
func (s *scanner) posture() {
	for _, p := range s.in.Programs {
		for _, e := range p.Errors {
			s.status.Limit("not analysed: " + e)
		}
	}
	byRoute := map[string][]string{}
	for _, f := range s.audit.Findings {
		for _, r := range f.Routes {
			byRoute[r.ID] = append(byRoute[r.ID], f.ID)
		}
	}
	for _, rt := range s.sortedRoutes() {
		ps := RoutePosture{ID: rt.ID, Key: rt.Key, Auth: authText(rt), Findings: dedupe(byRoute[rt.ID]), Unresolved: rt.UnresolvedCalls}
		if rt.Auth != nil && rt.Auth.Recognized {
			c := Check{Kind: "authentication", Label: "authentication " + authText(rt), Basis: "static"}
			if len(rt.Auth.Evidence) > 0 {
				c.File, c.Line = rt.Auth.Evidence[0].File, rt.Auth.Evidence[0].Line
			}
			ps.Checks = append(ps.Checks, c)
		}
		if from, ok := authorizationCheck(rt); ok {
			for _, x := range rt.Responses {
				if x.Status == 403 && x.From == from {
					c := Check{Kind: "authorization", Label: from + " answers 403 after the authentication", Symbol: x.FromID, Basis: "static"}
					if len(x.Evidence) > 0 {
						c.File, c.Line = x.Evidence[0].File, x.Evidence[0].Line
					}
					ps.Checks = append(ps.Checks, c)
					break
				}
			}
		}
		rc := s.reachFrom(rt.ID, 10)
		static, dynamic := 0, 0
		for id := range rc.prev {
			if n := s.nodes[id]; n != nil && n.Type == model.KindDB {
				if d, _ := n.Metadata["sqlDynamic"].(bool); d {
					dynamic++
				} else if sql, _ := n.Metadata["sql"].(string); sql != "" {
					static++
				}
			}
		}
		if static > 0 {
			ps.Checks = append(ps.Checks, Check{Kind: "parameterized", Label: plural(static, "database operation") + " with constant SQL (values passed as parameters)", Basis: "static"})
		}
		ps.DynamicSQL = dynamic
		s.audit.Routes = append(s.audit.Routes, ps)
	}
}
