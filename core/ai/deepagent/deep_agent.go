package deepagent

import (
	"agentmem"
	"app/shared"
	"context"
	"core/ai/agentbuilder"
	"fmt"
	"model"
	"strings"

	"basemodel/logs"

	"github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	aiModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
)

type SubAgentLoader interface {
	LoadAgent(ctx context.Context, agentId uuid.UUID) (*model.Agent, error)
	GetProviderConfig(ctx context.Context, provider string, modelName string) (*model.ProviderConfig, error)
	SearchKnowledgeBase(ctx context.Context, userId uuid.UUID, query string, kbId uuid.UUID) ([]*shared.SearchKnowledgeBaseResult, error)
}

// UniversalDeepAgent 通用深度代理
type UniversalDeepAgent struct {
	agent adk.ResumableAgent // 可恢复的 ADK 代理（支持断点续传）

	config       *model.DeepAgentConfig       // 深度代理配置
	chatModel    aiModel.ToolCallingChatModel // 支持工具调用的聊天模型
	subAgents    []adk.Agent                  // 子代理列表
	systemPrompt string

	builder    *agentbuilder.Builder   // 代理构建器
	loader     SubAgentLoader          // 子代理加载器
	memoryMgr  *agentmem.MemoryManager // 共享的记忆管理器
	agentModel *model.Agent            // 原始 Agent 模型
	userID     string
}

type UniversalDeepAgentConfig struct {
	Name           string
	Description    string
	APIKey         string
	BaseURL        string
	SubAgentLoader SubAgentLoader // 子代理加载器
	Agent          *model.Agent   // 代理模型
	SystemPrompt   string
	MemoryManager  *agentmem.MemoryManager // 共享记忆管理器
	UserID         string
}

func NewUniversalDeepAgent(ctx context.Context, cfg *UniversalDeepAgentConfig, deepConfig *model.DeepAgentConfig) (*UniversalDeepAgent, error) {
	mgr := cfg.MemoryManager
	if mgr == nil {
		mgr, _ = agentmem.NewMemoryManager()
		mgr.Start() // 启动抽取器池 worker
		logs.Warnf("NewUniversalDeepAgent MemoryManager 未注入，fallback 到新建实例（memory 不会与 GeneralAgent 共享）")
	}
	builder := agentbuilder.NewBuilderWithMemory(cfg.SubAgentLoader, mgr)
	agentContext, err := builder.BuildDeepAgentContext(ctx, cfg.Agent)
	if err != nil {
		logs.Errorf("NewUniversalDeepAgent 构建上下文失败: %v", err)
		return nil, err
	}

	agent := &UniversalDeepAgent{
		config:       deepConfig,
		chatModel:    agentContext.ChatModel,
		loader:       cfg.SubAgentLoader,
		builder:      builder,
		memoryMgr:    mgr,
		agentModel:   cfg.Agent,
		userID:       cfg.UserID,
		systemPrompt: cfg.SystemPrompt,
	}
	subAgents, err := agent.createSubAgents(ctx, cfg)
	if err != nil {
		return nil, err
	}
	agent.subAgents = subAgents

	// 创建本地后端，持久化代理状态
	backend, err := local.NewBackend(context.Background(), &local.Config{})
	if err != nil {
		logs.Errorf("NewUniversalDeepAgent 创建backend失败: %v", err)
		return nil, err
	}

	deepCfg := &deep.Config{
		Name:        cfg.Agent.Name,
		Description: cfg.Agent.Description,
		ChatModel:   agent.chatModel,
		Instruction: cfg.Agent.SystemPrompt,
		SubAgents:   subAgents,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentContext.Tools,
			},
		},
		Handlers:          agentContext.Skills,
		MaxIteration:      deepConfig.MaxIterations,
		WithoutWriteTodos: deepConfig.EnableTodos,
		Backend:           backend,
	}

	resumableAgent, err := deep.New(ctx, deepCfg)
	if err != nil {
		logs.Errorf("NewUniversalDeepAgent 创建resumableAgent失败: %v", err)
		return nil, err
	}
	agent.agent = resumableAgent

	return agent, nil
}

func (a *UniversalDeepAgent) createSubAgents(ctx context.Context, cfg *UniversalDeepAgentConfig) ([]adk.Agent, error) {
	var subAgents []adk.Agent
	if len(a.config.SubAgentIDs) > 0 {
		return a.loadSubAgentsFromDB(ctx, cfg)
	}
	return subAgents, nil
}

func (a *UniversalDeepAgent) loadSubAgentsFromDB(ctx context.Context, cfg *UniversalDeepAgentConfig) ([]adk.Agent, error) {
	var subAgents []adk.Agent
	if a.loader == nil {
		return nil, fmt.Errorf("loader is nil")
	}

	for _, agentId := range a.config.SubAgentIDs {
		agent, err := cfg.SubAgentLoader.LoadAgent(ctx, agentId)
		if err != nil {
			logs.Errorf("loadSubAgentsFromDB 加载子代理失败: %v", err)
			return nil, err
		}
		if agent == nil {
			return nil, fmt.Errorf("agent %s not found", agentId)
		}

		agentContext, err := a.builder.BuildDeepAgentContext(ctx, agent)
		if err != nil {
			logs.Errorf("loadSubAgentsFromDB 构建skills失败: %v", err)
			return nil, err
		}

		chatModelAgentConfig, err := a.builder.CreateChatModelAgentConfig(ctx, agent, nil, agentContext)
		if err != nil {
			logs.Errorf("loadSubAgentsFromDB 创建ChatModelAgentConfig失败: %v", err)
			return nil, err
		}

		modelAgent, err := adk.NewChatModelAgent(ctx, chatModelAgentConfig)
		if err != nil {
			logs.Errorf("loadSubAgentsFromDB 创建ChatModelAgent失败: %v", err)
			return nil, err
		}

		subAgents = append(subAgents, modelAgent)
	}
	return subAgents, nil
}

// ChatStream 执行 Deep Agent 推理，自动注入 Memory + RAG context 到 query
func (a *UniversalDeepAgent) ChatStream(ctx context.Context, sessionID string, message string) (<-chan *adk.AgentEvent, error) {
	injectedQuery := a.buildEnrichedQuery(ctx, sessionID, message)

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           a.agent,
		EnableStreaming: true,
	})

	iter := runner.Query(ctx, injectedQuery)
	eventChan := make(chan *adk.AgentEvent)
	go func() {
		defer close(eventChan)
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}

			select {
			case <-ctx.Done():
				return
			case eventChan <- event:
			}

			if event.Err != nil {
				return
			}
		}
	}()
	return eventChan, nil
}

// buildEnrichedQuery 构建注入了 Memory + RAG 上下文的 query
func (a *UniversalDeepAgent) buildEnrichedQuery(ctx context.Context, sessionID string, userMessage string) string {
	var sb strings.Builder

	// 1. 记忆召回
	if a.memoryMgr != nil {
		if err := a.memoryMgr.AddMessage(ctx, sessionID, a.userID, "user", userMessage); err != nil {
			logs.Warnf("deep agent memory add message failed: %v", err)
		}
		go func() {
			if err := a.memoryMgr.Reflect(context.Background(), sessionID, a.userID); err != nil {
				logs.Warnf("deep agent memory reflect failed: %v", err)
			}
		}()

		memoryCtx := a.memoryMgr.RecallContext(ctx, sessionID, a.userID, userMessage)
		if memoryCtx != "" {
			sb.WriteString("【关于你和该用户的历史对话记忆，请优先参考这些上下文】\n")
			sb.WriteString(memoryCtx)
			sb.WriteString("\n\n")
		}
	}

	// 2. RAG 知识库检索
	if a.loader != nil && a.agentModel != nil && len(a.agentModel.KnowledgeBases) > 0 {
		var allResult []*shared.SearchKnowledgeBaseResult
		for _, kb := range a.agentModel.KnowledgeBases {
			results, err := a.loader.SearchKnowledgeBase(ctx, a.agentModel.CreatorID, userMessage, kb.ID)
			if err != nil {
				logs.Warnf("deep agent rag search kb %s failed: %v", kb.ID, err)
				continue
			}
			allResult = append(allResult, results...)
		}
		if len(allResult) > 0 {
			sb.WriteString("【参考以下知识库内容回答问题】\n")
			for i, v := range allResult {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, v.Content))
				if i >= 4 {
					break
				}
			}
			sb.WriteString("\n")
		}
	}

	// 3. 用户原始问题
	sb.WriteString("【用户当前问题】\n")
	sb.WriteString(userMessage)

	return sb.String()
}
