package router

import (
	"basemodel/res"
	"github.com/gin-gonic/gin"
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
