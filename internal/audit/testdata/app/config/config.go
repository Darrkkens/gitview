// Package config holds settings of the fixture.
package config

// JWTSecret is a hardcoded signing key (GO-SECRET-001). The literal is
// split so that this fixture does not look like a real credential to
// repository scanners; the compiler folds it into one constant.
const JWTSecret = "fixture-signing-" + "key-0123456789abcdef"

// DatabasePassword is another hardcoded credential.
var DatabasePassword = "pa55" + "word-for-the-fixture"

// PasswordField is a name, not a secret: no finding.
const PasswordField = "password"

// Placeholder values are not secrets either.
const APIKeyPlaceholder = "<api-key>"
