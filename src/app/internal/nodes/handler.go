package nodes

import (
	"net/http"
	"time"

	"basemodel/logs"
	"basemodel/req"
	"basemodel/res"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *service
}

func (h *Handler) TestNode(c *gin.Context) {
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		logs.Warnf("SetWriteDeadline error: %v", err)
	}

	var reqs TestNodeReq
	if err := req.JsonParam(c, &reqs); err != nil {
		return
	}

	resp, err := h.service.testNode(c.Request.Context(), reqs)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func NewHandler() *Handler {
	return &Handler{
		service: newService(),
	}
}
