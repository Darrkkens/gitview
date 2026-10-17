package export

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/athos/gitview/internal/gitdiff"
)

var sensitiveNames = []string{
	".env", ".env.*", "*.env", ".envrc",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore", "*.kdbx",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	".npmrc", ".pypirc", ".netrc", ".pgpass", ".htpasswd", ".git-credentials",
	"credentials", "credentials.*", "secrets.*", "*secret*.y*ml", "*.tfvars", "*.tfstate",
	"service-account*.json",
}

var sensitiveContent = regexp.MustCompile(
	`-----BEGIN [A-Z ]*PRIVATE KEY-----` +
		`|AKIA[0-9A-Z]{16}` +
		`|gh[pousr]_[A-Za-z0-9]{36,}` +
		`|xox[abprs]-[A-Za-z0-9-]{10,}` +
		`|sk-[A-Za-z0-9_-]{20,}` +
		`|(?i)(password|passwd|secret|api[_-]?key|access[_-]?token)["']?\s*[:=]\s*["'][^"'\s]{8,}["']`)

// SensitivePath reports whether the file name suggests secrets.
func SensitivePath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	for _, pat := range sensitiveNames {
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}

// SensitiveContent reports whether added or context lines look like secrets.
func SensitiveContent(f gitdiff.File) bool {
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if sensitiveContent.MatchString(l.Text) {
				return true
			}
		}
	}
	return false
}

// Redact withholds the content of sensitive files unless include is set.
// It always returns the sensitive paths so callers can warn about them.
func Redact(files []gitdiff.File, include bool) (out []gitdiff.File, sensitive []string) {
	out = make([]gitdiff.File, len(files))
	for i, f := range files {
		if SensitivePath(f.Path) || SensitiveContent(f) {
			sensitive = append(sensitive, f.Path)
			if !include {
				f.Hunks, f.Raw = nil, ""
			}
		}
		out[i] = f
	}
	return out, sensitive
}
