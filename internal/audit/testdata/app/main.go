// Command app is the CodeAudit fixture: a small API with secure and
// insecure variants of the patterns the security rules look for.
package main

import (
	"net/http"
	_ "net/http/pprof" // GO-CONF-004: debug endpoints on the default mux
	"time"

	"example.com/app/handlers"
	"example.com/app/legacy"
	"example.com/app/middlewares"
	"example.com/vulnlib"
	"github.com/gin-gonic/gin"
)

func main() {
	_ = vulnlib.Version()
	gin.SetMode(gin.DebugMode)

	r := gin.New()
	r.Use(middlewares.CORS)
	r.GET("/health", handlers.Health)                      // public on purpose, touches nothing
	r.POST("/login", handlers.Login)                       // public, sets the session cookie
	r.POST("/public/clients", handlers.CreateClientPublic) // public, writes to the database

	api := r.Group("/api", middlewares.Auth)
	api.GET("/clients/:id", handlers.GetClient)              // authenticated, no ownership filter
	api.GET("/tenant/clients/:id", handlers.GetTenantClient) // filtered by the session's company
	api.GET("/search", handlers.Search)                      // SQL built from the query string
	api.GET("/safe-search", handlers.SafeSearch)             // parameterised
	api.GET("/report/:id", handlers.Report)                  // number converted before formatting
	api.GET("/files", handlers.Download)                     // path from the query string
	api.GET("/files/safe", handlers.SafeDownload)            // filepath.Base
	api.POST("/convert", handlers.Convert)                   // command built from input
	api.GET("/go", handlers.Go)                              // redirect to a user URL
	api.GET("/dynamic", handlers.Dynamic)                    // call through a function value

	api.GET("/files/checked", handlers.DownloadChecked) // path checked against the base directory

	// A 403 before the authentication filters origins: it is not an
	// authorization check.
	v2 := r.Group("/v2", middlewares.CORSAllowList, middlewares.Auth)
	v2.GET("/clients/:id", handlers.GetClient)

	admin := r.Group("/admin", middlewares.Auth, middlewares.RequireAdmin)
	admin.DELETE("/clients/:id", handlers.DeleteClient)

	go legacy.Serve()
	srv := &http.Server{Addr: ":9090", Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
	go srv.ListenAndServe()
	_ = r.Run(":8080")
}
