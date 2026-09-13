package main

import (
	"context"
	"core/ai/tools"
	"fmt"
	"thunder/config"

	"github.com/cloudwego/eino-ext/a2a/extension/eino"
	"github.com/cloudwego/eino-ext/a2a/transport/jsonrpc"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	hertzServer "github.com/cloudwego/hertz/pkg/app/server"
)

func main() {
	config.Init()
	conf := config.GetConfig()
	addr := fmt.Sprintf("%s:%d", conf.Server.GetHost(), conf.Server.GetPort())
	h := hertzServer.Default(
		hertzServer.WithHostPorts(addr),
		hertzServer.WithSenseClientDisconnection(true),
	)

	ctx := context.Background()
	r, err := jsonrpc.NewRegistrar(ctx, &jsonrpc.ServerConfig{
		Router:        h,
		HandlerPath:   "/a2a",
		AgentCardPath: nil,
	})
	if err != nil {
		panic(err)
	}

	chatModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: "http://127.0.0.1:11434",
		Model:   "modelscope.cn/Qwen/Qwen3-32B-GGUF:latest",
	})
	if err != nil {
		panic(err)
	}

	fileWriterTool := tools.NewFileWriteTool(&tools.FileWriteConfig{
		BaseDir: "",
	})
	toolsNodeConfig := compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{
			fileWriterTool,
		},
	}
	chatModelAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "文档生成智能体",
		Description: "一个可以生成对应文档的智能体",
		Instruction: "你是一个文档生成助手，请使用DeepSeek API实现项目文档生成",
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: toolsNodeConfig,
		},
	})
	if err != nil {
		panic(err)
	}

	err = eino.RegisterServerHandlers(ctx, chatModelAgent, &eino.ServerConfig{
		Registrar: r,
		URL:       "http://localhost:8777",
	})
	if err != nil {
		panic(err)
	}

	h.Run()
}
