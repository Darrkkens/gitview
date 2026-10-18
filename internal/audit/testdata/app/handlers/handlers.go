// Package handlers are the HTTP handlers of the fixture.
package handlers

import (
	"crypto/tls"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"example.com/app/services"
	"github.com/gin-gonic/gin"
)

type loginRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func Health(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }

// Login sets the session cookie without Secure and HttpOnly (GO-AUTHN-002).
func Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	c.SetCookie("session", "opaque", 3600, "/", "", false, false)
	c.Status(http.StatusNoContent)
}

// CreateClientPublic writes to the database with no authentication.
func CreateClientPublic(c *gin.Context) {
	if err := services.Default.Repo.Insert(c.Request.Context(), c.PostForm("name")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}) // GO-DATA-002
		return
	}
	c.Status(http.StatusCreated)
}

// GetClient loads the client named in the path; nothing ties it to the caller.
func GetClient(c *gin.Context) {
	id := c.Param("id")
	cl, err := services.Default.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, cl)
}

// GetTenantClient restricts the lookup to the session's company.
func GetTenantClient(c *gin.Context) {
	company := c.GetString("company_id")
	cl, err := services.Default.GetForCompany(c.Request.Context(), company, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": cl.ID, "name": cl.Name})
}

func Search(c *gin.Context) {
	rows, err := services.Default.Search(c.Request.Context(), c.Query("q"))
	if err == nil {
		rows.Close()
	}
	c.Status(http.StatusOK)
}

func SafeSearch(c *gin.Context) {
	rows, err := services.Default.Repo.SafeSearch(c.Request.Context(), c.Query("q"))
	if err == nil {
		rows.Close()
	}
	c.Status(http.StatusOK)
}

func Report(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	rows, err := services.Default.Repo.Report(c.Request.Context(), id)
	if err == nil {
		rows.Close()
	}
	c.Status(http.StatusOK)
}

// Download serves a file named by the query string (GO-INJ-003).
func Download(c *gin.Context) {
	data, err := os.ReadFile(filepath.Join("/srv/files", c.Query("name")))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.String(http.StatusOK, "%s", data)
}

// SafeDownload keeps only the last element of the name.
func SafeDownload(c *gin.Context) {
	data, err := os.ReadFile(filepath.Join("/srv/files", filepath.Base(c.Query("name"))))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.String(http.StatusOK, "%s", data)
}

// Convert runs a shell command built from the form (GO-INJ-002).
func Convert(c *gin.Context) {
	out, err := exec.Command("sh", "-c", "convert "+c.PostForm("file")+" out.png").CombinedOutput()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.String(http.StatusOK, "%s", out)
}

// Go redirects to the URL in the query string (GO-INJ-004).
func Go(c *gin.Context) {
	c.Redirect(http.StatusFound, c.Query("to"))
}

var actions = map[string]func(*gin.Context){"health": Health}

// Dynamic calls a handler chosen at runtime: the call cannot be resolved.
func Dynamic(c *gin.Context) {
	if f, ok := actions[c.Query("a")]; ok {
		f(c)
	}
}

func DeleteClient(c *gin.Context) {
	if err := services.Default.Repo.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusNoContent)
}

// Insecure HTTP client used to call a partner (GO-CONF-002).
var partner = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

func Partner() (*http.Response, error) { return partner.Get("https://partner.example") }

// DownloadChecked joins the name and checks the result stays in the base
// directory before reading it.
func DownloadChecked(c *gin.Context) {
	p := filepath.Join("/srv/files", c.Query("name"))
	if !strings.HasPrefix(p, "/srv/files/") {
		c.Status(http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.String(http.StatusOK, "%s", data)
}
