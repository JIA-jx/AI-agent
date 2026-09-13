package ai

import (
	"context"
	"core/ai/nodes"
	"encoding/json"
	"fmt"
	"model"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type WorkflowTool struct {
	workflow *model.Workflow
	node     *model.Node
}

func NewWorkflowTool(workflow *model.Workflow) *WorkflowTool {
	return &WorkflowTool{
		workflow: workflow,
	}
}

func (w *WorkflowTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	configJSON, _ := json.Marshal(w.workflow.Data)
	inputParams := make(map[string]*schema.ParameterInfo)

	var startNode *model.Node
	for _, node := range w.workflow.Data.Nodes {
		if node.Type == string(nodes.Start) {
			startNode = node
			break
		}
	}
	if startNode != nil {
		for _, edge := range w.workflow.Data.Edges {
			if edge.Source == startNode.ID {
				var targetNode *model.Node
				for _, node := range w.workflow.Data.Nodes {
					if node.ID == edge.Target {
						targetNode = node
						break
					}
				}
				if targetNode != nil && targetNode.Data != nil {
					w.node = targetNode
					for key, fieldData := range targetNode.Data {
						if fieldMap, ok := fieldData.(map[string]any); ok {
							if targetNode.Type == string(nodes.QwenVL) {
								if key == "model" || key == "providers" || key == "promptType" {
									continue
								}
							}
							fieldName := "unknown"
							if name, ok := fieldMap["fieldName"].(string); ok {
								fieldName = name
							}
							desc := fieldName
							if d, ok := fieldMap["fieldDesc"].(string); ok {
								desc = d
							}
							required := false
							if r, ok := fieldMap["required"].(bool); ok {
								required = r
							}

							inputParams[key] = &schema.ParameterInfo{
								Desc:     desc,
								Required: required,
								Type:     schema.String,
							}
						}
					}
				}
			}
		}
	}
	if len(inputParams) == 0 {
		inputParams["input"] = &schema.ParameterInfo{
			Desc: "工作流执行的输入参数",
			Type: schema.String,
		}
	}
	return &schema.ToolInfo{
		Name: "execute_workflow_" + w.workflow.Name,
		Desc: fmt.Sprintf("执行名为:%s的工作流，工作流描述为:%s。工作流配置%s",
			w.workflow.Name,
			w.workflow.Description,
			string(configJSON)),
		ParamsOneOf: schema.NewParamsOneOfByParams(inputParams),
	}, nil
}

func (w *WorkflowTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	executor := Executor

	var inputParams map[string]any
	err := json.Unmarshal([]byte(argumentsInJSON), &inputParams)
	if err != nil {
		return "", err
	}

	params := w.ConvertParams(inputParams)
	if w.node != nil {
		for _, value := range w.workflow.Data.Nodes {
			if value.ID == w.node.ID {
				for k, v := range params {
					value.Data[k] = v
				}
			}
		}
	}
	result, err := executor.Run(ctx, w.workflow.Data)
	if err != nil {
		return "", err
	}
	resultJson, _ := json.Marshal(result)
	return string(resultJson), nil
}

func (w *WorkflowTool) ConvertParams(params map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range params {
		result[k] = map[string]any{
			"fieldValue": v,
		}
	}
	return result
}
