package midd

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"basemodel/config"
	"basemodel/tools/jwt"
	"github.com/gin-gonic/gin"
)

func Auth(authConf *config.Auth) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authConf.IsAuth == nil || !*authConf.IsAuth {
			return
		}
		for _, pattern := range authConf.GetIgnores() {
			if isMatch(c.Request.URL.Path, pattern) {
				c.Next()
				return
			}
		}

		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			reject(c, "Authorization header is missing", authConf.NeedLogins)
			return
		}

		if len(tokenString) > 7 && strings.ToLower(tokenString[:7]) == "bearer " {
			tokenString = tokenString[7:]
		}
		claims, err := jwt.ParseToken(tokenString)
		if err != nil {
			reject(c, "Invalid token", authConf.NeedLogins)
			return
		}

		c.Set("userId", claims.UserId)
		c.Set("claims", claims)
		c.Next()
	}
}

func reject(ctx *gin.Context, errMsg string, needLoginUrls []string) {
	for _, v := range needLoginUrls {
		if isMatch(ctx.Request.URL.Path, v) {
			ctx.Next()
			return
		}
	}
	ctx.JSON(http.StatusUnauthorized, gin.H{"error": errMsg})
	ctx.Abort()
}

func isMatch(path string, pattern string) bool {
	// 构建正则匹配
	pattern = strings.ReplaceAll(pattern, "**", ".*")
	regexPattern := fmt.Sprintf("^%s$", pattern)

	matched, _ := regexp.MatchString(regexPattern, path)
	return matched
}
