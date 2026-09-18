package midd

import (
	"net/http"
	"strings"

	"basemodel/config"
	"github.com/gin-gonic/gin"
)

func Cors(conf *config.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		allowedOrigin, ok := isOriginAllowed(origin, conf.Cors)

		if ok {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
			if allowedOrigin != "*" {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		method := c.Request.Method
		c.Header("Access-Control-Allow-Methods", "POST, GET, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Content-SessionType, Token")
		c.Header("Access-Control-Expose-Headers", "Access-Control-Allow-Headers, Token")
		c.Header("Access-Control-Max-Age", "172800")
		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func isOriginAllowed(origin string, allowedOrigins []string) (string, bool) {
	for _, o := range allowedOrigins {
		if o == "*" {
			return "*", true
		}
		if o == origin {
			return origin, true
		}
		if strings.HasPrefix(o, "*.") {
			domainSuffix := strings.TrimPrefix(o, "*.")
			if strings.HasSuffix(origin, "."+domainSuffix) {

				return origin, true
			}
		}
	}
	return "", false
}
