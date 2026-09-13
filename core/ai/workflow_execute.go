package ai

import (
	"context"
	"core/ai/nodes"
	"fmt"
	"model"
	"strings"
	"sync"

	"thunder/logs"

	emodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	aiSchema "github.com/cloudwego/eino/schema"
)

var Executor *WorkflowExecutor

type WorkflowExecutor struct {
	nodeRegistry map[nodes.NodeType]NodeFactory
	once         sync.Once
}

type NodeFactory func(data map[string]any) nodes.WorkflowNode

func NewWorkflowExecutor() *WorkflowExecutor {
	return &WorkflowExecutor{
		nodeRegistry: make(map[nodes.NodeType]NodeFactory),
	}
}

func (w *WorkflowExecutor) initRegistry() {
	w.once.Do(func() {
		w.nodeRegistry[nodes.TextDisplay] = func(data map[string]any) nodes.WorkflowNode {
			return nodes.NewTextDisplayNode(data)
		}
		w.nodeRegistry[nodes.TextCombine] = func(data map[string]any) nodes.WorkflowNode {
			return nodes.NewTextCombineNode(data)
		}
		w.nodeRegistry[nodes.HtmlDisplay] = func(data map[string]any) nodes.WorkflowNode {
			return nodes.NewHtmlDisplayNode(data)
		}
		w.nodeRegistry[nodes.QwenVL] = func(data map[string]any) nodes.WorkflowNode {
			return nodes.NewQwenVLNode(data)
		}
	})
}

func Init() {
	Executor = NewWorkflowExecutor()
	Executor.initRegistry()
}

type EdgeKey struct {
	Source string
	Target string
}
type EdgeValues struct {
	SourceHandles []string
	TargetHandles []string
}

func (w *WorkflowExecutor) Run(ctx context.Context, data *model.Graph) (map[string]any, error) {
	w.initRegistry()
	if w.hasAdvancedFeatures(data) {
		return w.ExecuteAdvanced(ctx, data, DefaultAdvancedOptions())
	}
	return w.Execute(ctx, data)
}

func DefaultAdvancedOptions() AdvancedOptions {
	return AdvancedOptions{
		MaxSteps: 100,
		MaxLoops: 10,
	}
}

// Agentic DAG 实现
// condition 节点输出判断分支走向
// loop_count 多次执行节点
// while_condition 满足条件才继续
func (w *WorkflowExecutor) hasAdvancedFeatures(data *model.Graph) bool {
	for _, e := range data.Edges {
		if e.Style != nil {
			if c, ok := e.Style["condition"]; ok && c != "" && c != "always" {
				return true
			}
		}
	}
	for _, n := range data.Nodes {
		if _, ok := n.Data["loop_count"]; ok {
			return true
		}
		if _, ok := n.Data["while_condition"]; ok {
			return true
		}
	}
	return false
}

func (w *WorkflowExecutor) Execute(ctx context.Context, data *model.Graph) (map[string]any, error) {
	wf := compose.NewWorkflow[map[string]any, map[string]any]()

	var startNode *model.Node
	var endNode *model.Node
	sourceMap := make(map[string][]*model.Edge)
	targetMap := make(map[string][]*model.Edge)
	for _, edge := range data.Edges {
		sourceMap[edge.Target] = append(sourceMap[edge.Target], edge)
		targetMap[edge.Source] = append(targetMap[edge.Source], edge)
	}

	nodeRefs := make(map[string]*compose.WorkflowNode)
	hasStartNode := false
	hasEndNode := false
	for _, node := range data.Nodes {
		if node.Type == string(nodes.Start) {
			startNode = node
			hasStartNode = true
		}
		if node.Type == string(nodes.End) {
			endNode = node
			hasEndNode = true
		}
		if _, exists := nodeRefs[node.ID]; !exists {
			nodeFactory, ok := w.nodeRegistry[nodes.NodeType(node.Type)]
			if !ok {
				logs.Error("Failed to find node factory for node type: %s", node.Type)
				return nil, fmt.Errorf("不支持的节点类型: %s", node.Type)
			}
			ref := wf.AddLambdaNode(node.ID, compose.InvokableLambda(nodeFactory(node.Data).Invoke))
			nodeRefs[node.ID] = ref
		}
	}
	if !hasStartNode || startNode == nil {
		logs.Error("Workflow must have a start node")
		return nil, fmt.Errorf("工作流必须包含开始节点")
	}
	if !hasEndNode || endNode == nil {
		logs.Error("Workflow must have an end node")
		return nil, fmt.Errorf("工作流必须包含结束节点")
	}

	edgesMap := make(map[EdgeKey]*EdgeValues)
	for _, edge := range data.Edges {
		if edge.Source == startNode.ID {
			nodeRefs[edge.Target].AddInput(compose.START)
		} else if edge.Target == endNode.ID {
			wf.End().AddInput(edge.Source, compose.MapFields(edge.SourceHandle, edge.TargetHandle))
		} else {
			ek := EdgeKey{
				Source: edge.Source,
				Target: edge.Target,
			}
			if edgesMap[ek] == nil {
				edgesMap[ek] = &EdgeValues{
					SourceHandles: []string{edge.SourceHandle},
					TargetHandles: []string{edge.TargetHandle},
				}
			} else {
				edgesMap[ek].SourceHandles = append(edgesMap[ek].SourceHandles, edge.SourceHandle)
				edgesMap[ek].TargetHandles = append(edgesMap[ek].TargetHandles, edge.TargetHandle)
			}
		}
	}

	for key, value := range edgesMap {
		mappings := make([]*compose.FieldMapping, 0)
		for i, sourceHandle := range value.SourceHandles {
			mappings = append(mappings, compose.MapFields(sourceHandle, value.TargetHandles[i]))
		}
		nodeRefs[key.Target].AddInputWithOptions(key.Source, mappings)
	}

	runner, err := wf.Compile(ctx)
	if err != nil {
		logs.Error("Failed to compile workflow: %v", err)
		return nil, err
	}

	params := make(map[string]any)
	result, err := runner.Invoke(ctx, params)
	if err != nil {
		logs.Error("Failed to execute workflow: %v", err)
		return nil, err
	}

	results := make(map[string]any)
	results["result"] = result
	return results, nil
}

// ===========================================================================
// ExecuteAdvanced: 有状态条件图执行器
//
// 增强能力：
//  1. 条件分支：Edge.Style["condition"] 控制是否走这条边
//     - "always"（默认）：无条件
//     - "if_result_contains:keyword"：上游 node 输出包含关键词才走
//     - "if_result_equals:value"：上游输出等于某值才走
//     - "if_llm_judge:prompt"：用 LLM 判断是否走
//  2. 循环：Node.Data["loop_count"] = N 执行 N 次；或 Node.Data["while_condition"] = "expr"

// AdvancedOptions 高级执行配置
type AdvancedOptions struct {
	JudgeModel emodel.BaseChatModel // 用于 LLM 条件判断（if_llm_judge: prompt）
	MaxSteps   int                  // 最大执行步数，防死循环，默认 100
	MaxLoops   int                  // 单个 node 最大循环次数，默认 10
}

type AdvancedState struct {
	ctx    context.Context
	data   *model.Graph
	node   map[string]*model.Node
	edges  map[string][]*model.Edge // source → outgoing edges
	edgeBy map[string]*model.Edge   // id → edge
	state  map[string]any           // nodeID → 输出 state
	opts   AdvancedOptions
}

// ExecuteAdvanced 从开始节点出发，在每一轮（step）中，执行当前所有待执行的节点
// 执行节点时，会处理节点自身的循环（loop_count, while_condition）
// 节点执行完后，会检查该节点的所有出边，根据边的 condition 判断哪些边应该走通，从而确定下一轮要执行的目标节点集
// 一直持续到没有更多节点可执行或达到最大步数限制
func (w *WorkflowExecutor) ExecuteAdvanced(ctx context.Context, data *model.Graph, opts AdvancedOptions) (map[string]any, error) {
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 100
	}
	if opts.MaxLoops <= 0 {
		opts.MaxLoops = 10
	}

	startNode, endNode, err := w.parseAdvancedGraph(data)
	if err != nil {
		return nil, err
	}

	state := &AdvancedState{
		ctx:    ctx,
		data:   data,
		node:   make(map[string]*model.Node),
		edges:  make(map[string][]*model.Edge),
		edgeBy: make(map[string]*model.Edge),
		state:  make(map[string]any),
		opts:   opts,
	}
	for _, n := range data.Nodes {
		state.node[n.ID] = n
	}
	for _, e := range data.Edges {
		state.edges[e.Source] = append(state.edges[e.Source], e)
		state.edgeBy[e.ID] = e
	}

	// 从 start 开始执行
	stepCount := 0
	currentNodes := []string{startNode.ID}
	// BFS 主循环
	for len(currentNodes) > 0 && stepCount < opts.MaxSteps {
		stepCount++
		nextNodes := make(map[string]bool)

		for _, nodeID := range currentNodes {
			n := state.node[nodeID]
			if n == nil {
				continue
			}
			if n.Type == string(nodes.End) {
				continue
			}

			// 执行 node（含循环）
			outputs := w.executeNodeWithLoop(ctx, state, n)
			state.state[nodeID] = outputs

			// 条件边选择
			outgoing := state.edges[nodeID]
			for _, edge := range outgoing {
				if w.shouldFollowEdge(ctx, state, edge, outputs) {
					nextNodes[edge.Target] = true
				}
			}
		}

		var nextList []string
		for id := range nextNodes {
			nextList = append(nextList, id)
		}
		currentNodes = nextList
	}

	// 返回 End node 的输出作为最终结果
	result := make(map[string]any)
	result["result"] = state.state[endNode.ID]
	result["node_states"] = state.state
	result["total_steps"] = stepCount

	return result, nil
}

func (w *WorkflowExecutor) parseAdvancedGraph(data *model.Graph) (*model.Node, *model.Node, error) {
	var startNode, endNode *model.Node
	for _, n := range data.Nodes {
		if n.Type == string(nodes.Start) {
			startNode = n
		}
		if n.Type == string(nodes.End) {
			endNode = n
		}
	}
	if startNode == nil {
		return nil, nil, fmt.Errorf("missing start node")
	}
	if endNode == nil {
		return nil, nil, fmt.Errorf("missing end node")
	}

	return startNode, endNode, nil
}

// 负责执行单个节点，并处理其循环逻辑
// 根据 loop_count 执行多次，并在每次执行后检查 while_condition，如果条件不满足则提前退出循环
func (w *WorkflowExecutor) executeNodeWithLoop(ctx context.Context, state *AdvancedState, n *model.Node) map[string]any {
	factory, ok := w.nodeRegistry[nodes.NodeType(n.Type)]
	if !ok {
		logs.Warnf("Advanced: no factory for node type %s, skipping", n.Type)
		return nil
	}

	invoker := factory(n.Data)

	// 检查循环配置
	loopCount := 1
	if lc, ok := n.Data["loop_count"]; ok {
		if v, ok := lc.(float64); ok && v > 1 {
			loopCount = int(v)
		} else if v, ok := lc.(int); ok && v > 1 {
			loopCount = v
		}
	}
	if loopCount > state.opts.MaxLoops {
		loopCount = state.opts.MaxLoops
	}

	var outputs map[string]any
	for i := 0; i < loopCount; i++ {
		outputs, err := invoker.Invoke(ctx, state.state)
		if err != nil {
			logs.Warnf("Advanced: node %s iter %d invoke failed: %v", n.ID, i, err)
			break
		}

		// while_condition: 每轮执行后检查
		if cond, ok := n.Data["while_condition"]; ok {
			if !w.evaluateSimpleCondition(state, outputs, fmt.Sprint(cond)) {
				logs.Infof("Advanced: while_condition false at iter %d, breaking", i)
				break
			}
		}
	}
	return outputs
}

func (w *WorkflowExecutor) evaluateSimpleCondition(state *AdvancedState, outputs map[string]any, expr string) bool {
	if len(expr) > len("if_result_contains:") && expr[:len("if_result_contains:")] == "if_result_contains:" {
		return w.outputContains(outputs, expr[len("if_result_contains:"):])
	}
	return true
}

// shouldFollowEdge 判断是否走这条边
func (w *WorkflowExecutor) shouldFollowEdge(ctx context.Context, state *AdvancedState, edge *model.Edge, outputs map[string]any) bool {
	condition := "always"
	if edge.Style != nil {
		if c, ok := edge.Style["condition"]; ok {
			condition = fmt.Sprint(c)
		}
	}

	switch {
	case condition == "always" || condition == "":
		return true

	case len(condition) > len("if_result_contains:") && condition[:len("if_result_contains:")] == "if_result_contains:":
		keyword := condition[len("if_result_contains:"):]
		return w.outputContains(outputs, keyword)

	case len(condition) > len("if_result_equals:") && condition[:len("if_result_equals:")] == "if_result_equals:":
		expected := condition[len("if_result_equals:"):]
		return w.outputEquals(outputs, expected)

	case len(condition) > len("if_llm_judge:") && condition[:len("if_llm_judge:")] == "if_llm_judge:":
		prompt := condition[len("if_llm_judge:"):]
		return w.llmJudge(ctx, state, prompt, outputs)

	default:
		return true // 不识别的 condition 默认走
	}
}

func (w *WorkflowExecutor) outputContains(outputs map[string]any, keyword string) bool {
	for _, v := range outputs {
		if s, ok := v.(string); ok {
			if strings.Contains(s, keyword) {
				return true
			}
		}
	}
	return false
}

func (w *WorkflowExecutor) outputEquals(outputs map[string]any, expected string) bool {
	for _, v := range outputs {
		if fmt.Sprint(v) == expected {
			return true
		}
	}
	return false
}

func (w *WorkflowExecutor) llmJudge(ctx context.Context, state *AdvancedState, prompt string, outputs map[string]any) bool {
	if state.opts.JudgeModel == nil {
		logs.Warnf("Advanced: LLM judge requested but no model, defaulting to true")
		return true
	}

	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\n节点输出：\n")
	for k, v := range outputs {
		sb.WriteString(fmt.Sprintf("%s: %v\n", k, v))
	}
	sb.WriteString("\n请只回答 true 或 false。")

	resp, err := state.opts.JudgeModel.Generate(ctx, []*aiSchema.Message{
		{Role: aiSchema.System, Content: "你是一个工作流条件判断器，根据给定上下文回答 true 或 false。"},
		{Role: aiSchema.User, Content: sb.String()},
	})
	if err != nil {
		logs.Warnf("Advanced: LLM judge failed, defaulting to true: %v", err)
		return true
	}

	result := strings.ToLower(strings.TrimSpace(resp.Content))
	return strings.Contains(result, "true")
}
