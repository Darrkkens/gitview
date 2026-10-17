module example.com/app

go 1.22

require (
	github.com/gin-gonic/gin v0.0.0
	github.com/go-chi/chi/v5 v5.0.0
	github.com/jackc/pgx/v5 v5.0.0
	github.com/labstack/echo/v4 v4.0.0
)

replace (
	github.com/gin-gonic/gin => ../stubs/gin
	github.com/go-chi/chi/v5 => ../stubs/chi
	github.com/jackc/pgx/v5 => ../stubs/pgx
	github.com/labstack/echo/v4 => ../stubs/echo
)
