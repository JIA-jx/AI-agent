package einos

import (
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/mcp"
)

type McpConfig struct {
	BaseUrl  string
	Token    string
	Name     string
	Version  string
	Endpoint string
	Type     string
	Stdio    *Stdio
}

type Stdio struct {
	Command string
	Args    []string
	env     []string
}

func ConvertSchema(inputSchema mcp.ToolInputSchema) map[string]*schema.ParameterInfo {
	params := make(map[string]*schema.ParameterInfo)

	for key, value := range inputSchema.Properties {
		paramInfo := &schema.ParameterInfo{}
		if propMap, ok1 := value.(map[string]any); ok1 {
			if typeVal, ok2 := propMap["type"]; ok2 {
				if typeStr, ok3 := typeVal.(string); ok3 {
					switch typeStr {
					case "string":
						paramInfo.Type = schema.String
					case "integer":
						paramInfo.Type = schema.Integer
					case "number":
						paramInfo.Type = schema.Number
					case "boolean":
						paramInfo.Type = schema.Boolean
					case "array":
						paramInfo.Type = schema.Array
					case "object":
						paramInfo.Type = schema.Object
					default:
						paramInfo.Type = schema.String
					}
				}
			}

			if desc, exists := propMap["description"]; exists {
				if descStr, ok := desc.(string); ok {
					paramInfo.Desc = descStr
				}
			}

			if enum, exists := propMap["enum"]; exists {
				if enumSlice, ok := enum.([]interface{}); ok {
					for _, enumItem := range enumSlice {
						if enumStr, ok := enumItem.(string); ok {
							paramInfo.Enum = append(paramInfo.Enum, enumStr)
						}
					}
				}
			}
		}

		for _, required := range inputSchema.Required {
			if required == key {
				paramInfo.Required = true
				break
			}
		}

		params[key] = paramInfo
	}

	return params
}
