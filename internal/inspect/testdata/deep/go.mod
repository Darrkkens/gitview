module example.com/deep

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.0
	github.com/jackc/pgx/v5 v5.0.0
)

replace (
	github.com/go-chi/chi/v5 => ../stubs/chi
	github.com/jackc/pgx/v5 => ../stubs/pgx
)
