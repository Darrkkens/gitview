// Package jwt is a minimal stand-in for github.com/golang-jwt/jwt/v5.
package jwt

type Token struct {
	Method SigningMethod
	Valid  bool
}

type SigningMethod interface{ Alg() string }

type signingMethodNone struct{}

func (signingMethodNone) Alg() string { return "none" }

var SigningMethodNone SigningMethod = signingMethodNone{}

const UnsafeAllowNoneSignatureType = "none signing method allowed"

type Keyfunc func(*Token) (any, error)

type Parser struct{}

func NewParser() *Parser { return &Parser{} }

func Parse(tokenString string, keyFunc Keyfunc) (*Token, error) { return &Token{}, nil }
func (p *Parser) ParseUnverified(tokenString string, claims any) (*Token, []string, error) {
	return &Token{}, nil, nil
}
