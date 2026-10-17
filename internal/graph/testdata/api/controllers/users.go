package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"example.com/api/httpx"
	"example.com/api/services"
	"github.com/gin-gonic/gin"
)

type Address struct {
	Street string `json:"street" binding:"required"`
	City   string `json:"city"`
}

type Audit struct {
	CreatedBy string `json:"created_by"`
}

// CreateUserRequest is the body of POST /api/users.
type CreateUserRequest struct {
	Audit
	Name     string            `json:"name" binding:"required,min=2"`
	Email    string            `json:"email" binding:"required,email"`
	Age      int               `json:"age" binding:"gte=0,lte=150"`
	Nickname *string           `json:"nickname,omitempty"`
	Role     string            `json:"role" binding:"oneof=admin user"`
	Tags     []string          `json:"tags"`
	Address  Address           `json:"address"`
	Labels   map[string]string `json:"labels"`
	Birthday time.Time         `json:"birthday"`
	Secret   string            `json:"-"`
	internal string
}

type UserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type updateRequest struct {
	Name string `json:"name"`
}

type userURI struct {
	ID string `uri:"id" binding:"required"`
}

var errUserNotFound = httpx.NewError(http.StatusNotFound, "user_not_found", "user not found")

type UserController struct{ svc *services.UserService }

func NewUserController(s *services.UserService) *UserController { return &UserController{svc: s} }

func (ctl *UserController) List(c *gin.Context) {
	page := c.DefaultQuery("page", "1")
	limit, _ := strconv.Atoi(c.Query("limit"))
	users := ctl.svc.List(c.Request.Context(), page, limit)
	c.JSON(http.StatusOK, users)
}

func (ctl *UserController) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := ctl.svc.Create(c.Request.Context(), req.Name, req.Email)
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, UserResponse{ID: u.ID, Name: u.Name, Email: u.Email})
}

func (ctl *UserController) Get(c *gin.Context) {
	id := c.Param("id")
	u, err := ctl.svc.Get(c.Request.Context(), id)
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, UserResponse{ID: u.ID, Name: u.Name})
}

// Update reads the id with ShouldBindUri and the body through a helper.
func (ctl *UserController) Update(c *gin.Context) {
	var uri userURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	var req updateRequest
	if !httpx.DecodeJSON(c.Writer, c.Request, &req) {
		return
	}
	httpx.JSON(c.Writer, http.StatusOK, UserResponse{ID: uri.ID, Name: req.Name})
}

func (ctl *UserController) Delete(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func (ctl *UserController) Avatar(c *gin.Context) {
	if _, err := c.FormFile("file"); err != nil {
		c.String(http.StatusBadRequest, "missing file")
		return
	}
	c.Status(ctl.svc.AvatarStatus()) // decided at runtime
}

func (ctl *UserController) Kind(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"kind": c.Param("kind")})
}

// fail maps service errors to responses.
func (ctl *UserController) fail(c *gin.Context, err error) {
	if errors.Is(err, services.ErrNotFound) {
		httpx.WriteError(c.Writer, errUserNotFound)
		return
	}
	c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal"})
}

func Health(c *gin.Context) { c.String(http.StatusOK, "ok") }

// notRegistered looks like a handler but no route uses it: its responses
// must not be attributed to any endpoint.
func notRegistered(c *gin.Context) { c.JSON(http.StatusTeapot, gin.H{"never": true}) }
