package server

import (
	"basemodel/config"
	"basemodel/midd"
	"github.com/gin-gonic/gin"
)

type IRouter interface {
	Register(engine *gin.Engine)
}

type CloseIRouter interface {
	IRouter
	Close() error
}

func UseMidd(conf *config.Config, engin *gin.Engine) {
	if conf.Server != nil {
		if len(conf.Server.GetCors()) > 0 {
			engin.Use(midd.Cors(conf.Server))
		}
	}
	if conf.Auth != nil {
		if conf.Auth.GetIsAuth() {
			engin.Use(midd.Auth(conf.Auth))
		}
	}
}
