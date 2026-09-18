package inits

import (
	"mcp-server/internal/router"

	"basemodel/server"
)

func Init(s *server.Server) {
	s.RegisterRouters(&router.Event{}, &router.McpRouter{})
}
