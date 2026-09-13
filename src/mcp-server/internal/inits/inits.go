package inits

import (
	"mcp-server/internal/router"

	"thunder/server"
)

func Init(s *server.Server) {
	s.RegisterRouters(&router.Event{}, &router.McpRouter{})
}
