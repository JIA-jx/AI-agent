package router

import (
	"mcp-server/internal/tool"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type McpRouter struct {
}

func (u *McpRouter) Register(r *gin.Engine) {
	mcpServer := server.NewMCPServer(
		"mcp server",
		mcp.LATEST_PROTOCOL_VERSION,
		server.WithToolCapabilities(true),           // 启用工具能力
		server.WithResourceCapabilities(true, true), // 资源能力
		server.WithPromptCapabilities(true),         // 提示词能力
	)

	weather := tool.NewWeatherTool(tool.GdApiKey)
	mcpServer.AddTool(weather.Build(), weather.Invoke)
	sseServer := server.NewSSEServer(
		mcpServer,
		server.WithBaseURL("http://localhost:7777"),
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
		server.WithKeepAlive(true),
	)

	r.GET("/sse", gin.WrapH(sseServer.SSEHandler()))
	r.POST("/message", gin.WrapH(sseServer.MessageHandler()))
}
