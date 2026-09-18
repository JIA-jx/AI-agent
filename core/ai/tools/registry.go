package tools

import (
	"context"

	"basemodel/ai/einos"
)

var _register *Registry

type Registry struct {
	tools []einos.InvokeParamTool
}

func RegisterSystemTools(inputs ...einos.InvokeParamTool) {
	var tools []einos.InvokeParamTool
	tools = append(tools, inputs...)
	_register = &Registry{tools: tools}
}

func FindTool(toolName string) einos.InvokeParamTool {
	for _, t := range _register.tools {
		info, _ := t.Info(context.Background())
		if info.Name == toolName {
			return t
		}
	}
	return nil
}
