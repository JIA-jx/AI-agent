package nodes

import "context"

type NodeType string

const (
	Start       NodeType = "start"
	End         NodeType = "end"
	TextCombine NodeType = "textCombine"
	TextDisplay NodeType = "textDisplay"
	HtmlDisplay NodeType = "htmlDisplay"
	QwenVL      NodeType = "qwenVL"
)

type WorkflowNode interface {
	Invoke(ctx context.Context, input map[string]any) (map[string]any, error)
}
