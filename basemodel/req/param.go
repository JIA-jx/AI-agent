package req

import (
	"strconv"

	"basemodel/errs"
	"basemodel/logs"
	"basemodel/res"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func JsonParam(c *gin.Context, obj any) error {
	err := c.ShouldBindJSON(obj)
	if err != nil {
		logs.Errorf("req parse json param err : %v", err)
		res.Error(c, errs.ErrParam)
		return errs.ErrParam
	}
	return nil
}

func QueryParam(c *gin.Context, obj any) error {
	err := c.ShouldBindQuery(obj)
	if err != nil {
		logs.Errorf("req parse query param err : %v", err)
		res.Error(c, errs.ErrParam)
		return errs.ErrParam
	}
	return nil
}

func PathParam(ctx *gin.Context, paramKey string) string {
	param := ctx.Param(paramKey)
	return param
}

func Path(ctx *gin.Context, paramKey string, value any) error {
	param := PathParam(ctx, paramKey)
	if param == "" {
		logs.Errorf("req path parse param err : %v", "param is nil")
		res.Error(ctx, errs.ErrParam)
		return errs.ErrParam
	}

	switch v := value.(type) {
	case *string:
		*v = param
	case *int:
		i, err := strconv.Atoi(param)
		if err != nil {
			logs.Errorf("req path parse param err : %v", err)
			res.Error(ctx, errs.ErrParam)
			return errs.ErrParam
		}
		*v = i
	case *int64:
		i, err := strconv.ParseInt(param, 10, 64)
		if err != nil {
			logs.Errorf("req path parse param err : %v", err)
			res.Error(ctx, errs.ErrParam)
			return errs.ErrParam
		}
		*v = i
	case *uuid.UUID:
		var err error
		*v, err = uuid.Parse(param)
		if err != nil {
			logs.Errorf("req path parse param err : %v", err)
			res.Error(ctx, errs.ErrParam)
			return errs.ErrParam
		}
	default:
		logs.Errorf("req path parse param err : %v", "no support param type")
		res.Error(ctx, errs.ErrParam)
		return errs.ErrParam
	}

	return nil
}

func PathInArray(ctx *gin.Context, method string, urls []string) bool {
	path := ctx.Request.URL.Path
	for _, url := range urls {
		if path == url && method == ctx.Request.Method {
			return true
		}
	}
	return false
}

func GetUserIdUUID(ctx *gin.Context) (uuid.UUID, bool) {
	value, exists := ctx.Get("userId")
	if !exists {
		res.Error(ctx, errs.ErrUnauthorized)
		return uuid.Nil, false
	}
	str := value.(string)
	parse, err := uuid.Parse(str)
	if err != nil {
		res.Error(ctx, errs.ErrUnauthorized)
		return uuid.Nil, false
	}
	return parse, true
}
