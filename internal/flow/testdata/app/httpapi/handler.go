package httpapi

import (
	"strconv"

	"example.com/app/client"
	"github.com/gin-gonic/gin"
)

type ClientHandler struct{ svc *client.ClientService }

func NewClientHandler(s *client.ClientService) *ClientHandler { return &ClientHandler{svc: s} }

// GetClientByID is a homonym method with a different receiver.
func (h *ClientHandler) GetClientByID(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	cl, err := h.svc.GetClientByID(nil, id)
	if err != nil {
		c.JSON(400, err)
		return
	}
	c.JSON(200, cl)
}

// Auth returns a middleware closure.
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {}
}

func Logger(c *gin.Context) {}

// Register adds the client routes to a router group received as parameter.
func Register(r *gin.RouterGroup, h *ClientHandler) {
	r.GET("/clients/:id", Auth(), h.GetClientByID)
}

// chain builds a handler list in a helper, as real projects do:
// r.GET(path, chain(h)...) and append(chain(h), extra)...
func chain(h gin.HandlerFunc) []gin.HandlerFunc {
	return []gin.HandlerFunc{Auth(), h}
}

func RegisterMore(r *gin.RouterGroup, h *ClientHandler) {
	r.GET("/spread", chain(h.GetClientByID)...)
	r.POST("/appended", append(chain(h.GetClientByID), Logger)...)
}
