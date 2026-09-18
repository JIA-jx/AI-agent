package tool

import (
	"context"
	"core/ai/tools"
	"encoding/json"

	"basemodel/ai/einos"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/mcp"
)

type WeatherTool struct {
	ApiKey string
	tool   einos.InvokeParamTool
}

func NewWeatherTool(apiKey string) *WeatherTool {
	return &WeatherTool{ApiKey: apiKey}
}

func ToMCPOptions(params map[string]*schema.ParameterInfo, desc string) []mcp.ToolOption {
	var options []mcp.ToolOption
	options = append(options, mcp.WithDescription(desc))

	for k, v := range params {
		var propertyOptions []mcp.PropertyOption
		if v.Required {
			propertyOptions = append(propertyOptions, mcp.Required())
		}
		propertyOptions = append(propertyOptions, mcp.Description(v.Desc))
		if v.Enum != nil && len(v.Enum) > 0 {
			propertyOptions = append(propertyOptions, mcp.Enum(v.Enum...))
		}

		switch v.Type {
		case schema.String:
			options = append(options, mcp.WithString(k, propertyOptions...))
		case schema.Number:
			options = append(options, mcp.WithNumber(k, propertyOptions...))
		case schema.Boolean:
			options = append(options, mcp.WithBoolean(k, propertyOptions...))
		case schema.Integer:
			options = append(options, mcp.WithNumber(k, propertyOptions...))
		case schema.Array:
			options = append(options, mcp.WithArray(k, propertyOptions...))
		case schema.Object:
			options = append(options, mcp.WithObject(k, propertyOptions...))
		}
	}
	return options
}

func (w *WeatherTool) Build() mcp.Tool {
	tool := tools.NewWeatherTool(&tools.WeatherConfig{ApiKey: w.ApiKey})
	w.tool = tool

	info, _ := tool.Info(context.Background())
	params := tool.Params()
	options := ToMCPOptions(params, info.Desc)

	return mcp.NewTool(info.Name, options...)
}

func (w *WeatherTool) Invoke(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params, err := json.Marshal(request.GetArguments())
	if err != nil {
		return nil, err
	}

	invokableRun, err := w.tool.InvokableRun(ctx, string(params))
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(invokableRun), nil
}
