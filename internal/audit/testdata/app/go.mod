module example.com/app

go 1.22

require (
	example.com/vulnlib v1.0.0
	github.com/gin-gonic/gin v0.0.0
	github.com/golang-jwt/jwt/v5 v5.0.0
)

replace (
	example.com/vulnlib => ../stubs/vulnlib
	github.com/gin-gonic/gin => ../stubs/gin
	github.com/golang-jwt/jwt/v5 => ../stubs/jwt
)
