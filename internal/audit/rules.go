package audit

// RuleInfo describes a rule: what it looks for, why it matters and how a
// finding is fixed and tested. The texts are generic; a finding adds what
// was found where.
type RuleInfo struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Title       string   `json:"title"`
	Severity    string   `json:"severity"` // default; a finding may lower or raise it with a reason
	Basis       string   `json:"basis"`    // static | heuristic | database
	Detects     string   `json:"detects"`
	Why         string   `json:"why"`
	Remediation []string `json:"remediation"`
	Tests       []string `json:"tests"`
	Limits      string   `json:"limits"`
	Enabled     bool     `json:"enabled"`
	Findings    int      `json:"findings"`
}

type rule struct {
	RuleInfo
	run func(s *scanner, r *rule)
}

// catalog is the rule set, in report order. Each rule only reports what
// its evidence supports: a missing check is stated as "not established",
// never as proof that it does not exist.
var catalog = []*rule{
	{RuleInfo{ID: "GO-INJ-001", Category: CatInjection, Title: "SQL built from untrusted input", Severity: SevHigh, Basis: "static",
		Detects:     "SQL text built at runtime (concatenation, fmt.Sprintf, values passed in) whose runtime part comes from HTTP input according to Data Flow.",
		Why:         "Input that becomes part of the SQL text can change the statement (SQL injection): read or modify other rows, bypass conditions, or run other statements.",
		Remediation: []string{"Pass the value as a query parameter ($1, ?) instead of formatting it into the SQL.", "If an identifier (column, table, ORDER BY) must vary, map the input to a fixed allow-list of identifiers."},
		Tests:       []string{"Send a value containing a quote and SQL syntax (e.g. ' OR '1'='1) and check the query result and the logs.", "Add a test that runs the query with such a value against a test database and asserts it matches nothing unexpected."},
		Limits:      "Data paths through memory the analysis cannot follow are reported as incomplete, not as safe."}, ruleSQLInjection},
	{RuleInfo{ID: "GO-INJ-002", Category: CatInjection, Title: "OS command built from untrusted input", Severity: SevCritical, Basis: "static",
		Detects:     "os/exec commands whose program, shell script or arguments come from HTTP input.",
		Why:         "Input in a shell script runs arbitrary commands; input as the program name runs any binary; input in arguments can inject options.",
		Remediation: []string{"Do not run a shell: call the program directly with a fixed name.", "Validate the input against an allow-list and pass it after \"--\" so it cannot become an option."},
		Tests:       []string{"Call the endpoint with a value containing ; $(…) and a leading '-' and assert no extra process runs.", "Unit-test the argument builder with hostile values."}}, ruleCommandInjection},
	{RuleInfo{ID: "GO-INJ-003", Category: CatInjection, Title: "File path from untrusted input", Severity: SevHigh, Basis: "static",
		Detects:     "File operations (os.Open, os.ReadFile, os.WriteFile, os.Remove, http.ServeFile, gin's File…) whose path comes from HTTP input, without filepath.Base.",
		Why:         "Path elements such as ../ in the input reach files outside the intended directory (path traversal).",
		Remediation: []string{"Keep only the last element (filepath.Base) or map the input to known file names.", "After joining, resolve the path and check it is still inside the base directory (filepath.Rel, or os.Root on Go 1.24+)."},
		Tests:       []string{"Request ../../etc/passwd (and its URL-encoded forms) and assert the response is an error.", "Test the path builder with absolute paths and .. segments."},
		Limits:      "A condition on the value is listed as a validation, but its sufficiency is not established."}, rulePathTraversal},
	{RuleInfo{ID: "GO-INJ-004", Category: CatInjection, Title: "Redirect to a URL from untrusted input", Severity: SevMedium, Basis: "static",
		Detects:     "http.Redirect and the frameworks' Redirect called with a location that comes from HTTP input.",
		Why:         "An attacker can send links to the application that redirect to their own site (open redirect, used in phishing and token theft).",
		Remediation: []string{"Accept only relative paths, or compare the parsed host with an allow-list before redirecting."},
		Tests:       []string{"Request the endpoint with to=https://evil.example and //evil.example and assert it does not redirect there."}}, ruleOpenRedirect},
	{RuleInfo{ID: "GO-AUTHN-001", Category: CatAuthentication, Title: "Route without identified authentication changes data", Severity: SevMedium, Basis: "static",
		Detects:     "Routes whose handler chain has no recognised authentication (no element reads a credential and can answer 401) and that may write to the database or run a command.",
		Why:         "If the route should be restricted, anyone can change data through it. Public routes are not wrong by themselves: this asks for confirmation.",
		Remediation: []string{"If the route must be restricted, add the authentication middleware to its group.", "If it is public on purpose (sign-up, webhook with its own signature), record that decision."},
		Tests:       []string{"Call the route without credentials and assert the expected status (401, or success if public by design)."},
		Limits:      "Authentication done outside the chain (a gateway, a proxy, mTLS) is not visible to the analysis."}, ruleUnauthenticatedWrite},
	{RuleInfo{ID: "GO-AUTHN-002", Category: CatAuthentication, Title: "Cookie without security attributes", Severity: SevMedium, Basis: "static",
		Detects:     "Cookies set without Secure or HttpOnly (http.Cookie literals, gin's SetCookie with false arguments).",
		Why:         "Without Secure the cookie travels over plain HTTP; without HttpOnly scripts (XSS) can read it; session cookies are the usual target.",
		Remediation: []string{"Set Secure: true and HttpOnly: true, and SameSite (Lax or Strict) for session cookies."},
		Tests:       []string{"Assert the Set-Cookie header of the response contains Secure, HttpOnly and SameSite."}}, ruleCookies},
	{RuleInfo{ID: "GO-AUTHN-003", Category: CatAuthentication, Title: "JWT accepted without signature verification", Severity: SevHigh, Basis: "static",
		Detects:     "ParseUnverified, the \"none\" signing method or UnsafeAllowNoneSignatureType of golang-jwt.",
		Why:         "Claims of an unverified token are chosen by whoever sends it: user, roles, tenant.",
		Remediation: []string{"Verify tokens with jwt.Parse (or ParseWithClaims) and a key function that checks the expected algorithm.", "Use ParseUnverified only to read a header such as kid before verifying."},
		Tests:       []string{"Send a token signed with another key and one with alg=none; both must be rejected."}}, ruleJWT},
	{RuleInfo{ID: "GO-AUTHZ-001", Category: CatAuthorization, Title: "Resource authorization not established", Severity: SevMedium, Basis: "static",
		Detects:     "A resource identifier from the request (path, query, body) reaches a SELECT, UPDATE or DELETE whose conditions include no owner or tenant column filled from outside the request.",
		Why:         "If nothing else checks that the resource belongs to the caller, one user can read or change another user's data by changing the identifier (IDOR).",
		Remediation: []string{"Add the owner or tenant to the query (… AND company_id = $2) with the value from the authenticated session.", "Or check ownership after loading the row and answer 404/403 when it does not match."},
		Tests:       []string{"With two users of different tenants, request the other tenant's resource identifier and assert 404 or 403."},
		Limits:      "The check may exist where the analysis does not look: database policies (RLS), a gateway, or a helper reached through an unresolved call. This is a request to review, not a confirmed IDOR."}, ruleResourceAuthorization},
	{RuleInfo{ID: "GO-AUTHZ-002", Category: CatAuthorization, Title: "Privileged route without an authorization check", Severity: SevMedium, Basis: "heuristic",
		Detects:     "Routes under an administrative path (/admin, /internal, /manage…) whose chain has no element answering 403.",
		Why:         "Authentication tells who the caller is; administrative actions also need to check what the caller may do.",
		Remediation: []string{"Add a role or permission check (a middleware answering 403) to the administrative group."},
		Tests:       []string{"Call the route as an authenticated user without the role and assert 403."},
		Limits:      "The path is the heuristic part: other privileged routes are not recognised by name."}, rulePrivilegedRoutes},
	{RuleInfo{ID: "GO-DATA-001", Category: CatSensitiveData, Title: "Hardcoded secret", Severity: SevHigh, Basis: "static",
		Detects:     "String constants that match credential formats (private keys, cloud and API tokens) or are assigned to names such as password, secret, token or apiKey.",
		Why:         "Secrets in the code reach every copy of the repository and its history, and cannot be rotated without a release.",
		Remediation: []string{"Read the secret from the environment or a secret manager, and rotate the exposed value.", "Remove it from the history if the repository is shared."},
		Tests:       []string{"Add a secret scanner (for example gitleaks) to CI and assert this location no longer reports."},
		Limits:      "Name-based detection is heuristic; the secret itself is never shown in reports or sent to the AI."}, ruleHardcodedSecrets},
	{RuleInfo{ID: "GO-DATA-002", Category: CatSensitiveData, Title: "Internal error details in a response", Severity: SevLow, Basis: "static",
		Detects:     "Responses whose body includes err.Error() (gin JSON maps, http.Error) with a 5xx status.",
		Why:         "Database and library errors reveal table names, queries, paths and versions that help an attacker.",
		Remediation: []string{"Log the error with a request ID and answer a generic message."},
		Tests:       []string{"Force the error (for example with a failing repository mock) and assert the body has no internal message."}}, ruleErrorDetails},
	{RuleInfo{ID: "GO-DATA-003", Category: CatSensitiveData, Title: "Sensitive field in a response body", Severity: SevMedium, Basis: "heuristic",
		Detects:     "Response schemas (from the HTTP catalog) with fields named like passwords, hashes, secrets or tokens.",
		Why:         "Password hashes and secrets returned to clients can be cracked or reused.",
		Remediation: []string{"Return a dedicated response type, or mark the field json:\"-\"."},
		Tests:       []string{"Assert the JSON of the response has no such field."},
		Limits:      "Field names are the heuristic part; token fields of login responses are often intended."}, ruleSensitiveResponse},
	{RuleInfo{ID: "GO-DATA-004", Category: CatSensitiveData, Title: "Credential written to logs", Severity: SevMedium, Basis: "static",
		Detects:     "log, slog and fmt print calls whose arguments come from the Authorization header, cookies, or values named password, token or secret.",
		Why:         "Logs are read by more people and systems than the request was meant for, and are kept for long.",
		Remediation: []string{"Do not log credentials; log a hash prefix or a fixed marker instead."},
		Tests:       []string{"Capture the logger output in a test request with a token and assert the token does not appear."}}, ruleCredentialLogs},
	{RuleInfo{ID: "GO-CONF-001", Category: CatConfiguration, Title: "Permissive CORS", Severity: SevHigh, Basis: "static",
		Detects:     "Access-Control-Allow-Origin set to * or to the request's Origin, especially together with Allow-Credentials, and permissive gin-contrib/cors or rs/cors settings.",
		Why:         "Reflecting any origin with credentials lets any website read authenticated responses of the user's browser.",
		Remediation: []string{"Compare the Origin with an allow-list before echoing it, and never combine * or reflection with credentials."},
		Tests:       []string{"Send a request with Origin: https://evil.example and assert the response does not allow it."}}, ruleCORS},
	{RuleInfo{ID: "GO-CONF-002", Category: CatConfiguration, Title: "TLS verification disabled or weak TLS version", Severity: SevHigh, Basis: "static",
		Detects:     "tls.Config with InsecureSkipVerify: true, or MinVersion below TLS 1.2.",
		Why:         "Without certificate verification any network attacker can impersonate the server (man in the middle).",
		Remediation: []string{"Remove InsecureSkipVerify; for private CAs set RootCAs.", "Set MinVersion to tls.VersionTLS12 or later."},
		Tests:       []string{"Point the client at a server with a self-signed certificate and assert the connection fails."}}, ruleTLS},
	{RuleInfo{ID: "GO-CONF-003", Category: CatConfiguration, Title: "HTTP server or client without timeouts", Severity: SevLow, Basis: "static",
		Detects:     "http.ListenAndServe, gin's Run, http.Server without ReadHeaderTimeout/ReadTimeout, and http.Client without Timeout.",
		Why:         "Slow clients can hold connections open (Slowloris); outgoing calls without a timeout can block handlers forever.",
		Remediation: []string{"Use an http.Server with ReadHeaderTimeout (and ReadTimeout, WriteTimeout, IdleTimeout).", "Give http.Client a Timeout or use requests with a context deadline."},
		Tests:       []string{"Open a connection that sends headers slowly and assert the server closes it."}}, ruleTimeouts},
	{RuleInfo{ID: "GO-CONF-004", Category: CatConfiguration, Title: "Debug configuration", Severity: SevLow, Basis: "static",
		Detects:     "gin.SetMode(gin.DebugMode) and net/http/pprof imported in a program that serves http.DefaultServeMux.",
		Why:         "Debug modes and profiling endpoints expose internals (stack traces, memory, goroutines) if they reach production.",
		Remediation: []string{"Select the mode from configuration and serve pprof only on a separate, private listener."},
		Tests:       []string{"Assert /debug/pprof/ answers 404 on the public listener."},
		Limits:      "Whether this code runs in production cannot be determined statically."}, ruleDebug},
	{RuleInfo{ID: "GO-DEP-001", Category: CatDependencies, Title: "Dependency with a known vulnerability", Severity: SevMedium, Basis: "database",
		Detects:     "Modules required by go.mod at a version an advisory of the configured local OSV database (Go vulnerability database format) lists as affected.",
		Why:         "Known vulnerabilities have public details and often exploits.",
		Remediation: []string{"Upgrade to a fixed version (go get module@version) and run the tests."},
		Tests:       []string{"Re-run the audit after upgrading: the advisory must no longer match."},
		Limits:      "Only the configured database is used, as up to date as it is; severity is not part of the Go database. Reachability is checked only for the symbols an advisory lists."}, ruleDependencies},
}

// Rules returns the rule catalog.
func Rules() []RuleInfo {
	out := make([]RuleInfo, len(catalog))
	for i, r := range catalog {
		out[i] = r.RuleInfo
		out[i].Enabled = true
	}
	return out
}
