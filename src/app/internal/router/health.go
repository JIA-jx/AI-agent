package router

import (
	"github.com/gin-gonic/gin"
	"thunder/res"
)

type HealthRouter struct {
}

func (r *HealthRouter) Register(engine *gin.Engine) {
	engine.GET("/health", func(c *gin.Context) {
		res.Success(c, gin.H{
			"status": "ok",
		})
	})
}
