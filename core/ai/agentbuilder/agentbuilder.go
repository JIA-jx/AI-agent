package agentbuilder

import (
	"agentmem"
	"app/shared"
	"context"
	"core/ai"
	"core/ai/mcps"
	"core/ai/tools"
	"fmt"
	"model"
	"strings"

	"basemodel/ai/einos"
	"basemodel/logs"

	"github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	aiModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/ollama/api"
	"github.com/google/uuid"
)

type Context struct {
	ProviderConfig *model.ProviderConfig
	ChatModel      aiModel.ToolCallingChatModel
	Tools          []tool.BaseTool
	Skills         []adk.ChatModelAgentMiddleware
	RAGContext     string
	Memory         string
}

type Builder struct {
	Loader    Loader
	MemoryMgr *agentmem.MemoryManager
	SessionID string
}

type Loader interface {
	GetProviderConfig(ctx context.Context, provider string, modelName string) (*model.ProviderConfig, error)
	SearchKnowledgeBase(ctx context.Context, userId uuid.UUID, query string, kbId uuid.UUID) ([]*shared.SearchKnowledgeBaseResult, error)
}

func NewBuilderWithMemory(loader Loader, mgr *agentmem.MemoryManager) *Builder {
	return &Builder{
		Loader:    loader,
		MemoryMgr: mgr,
	}
}

func (b *Builder) BuildDeepAgentContext(ctx context.Context, agent *model.Agent, optMessage ...string) (*Context, error) {
	if b == nil {
		return nil, fmt.Errorf("builder is nil")
	}
	if agent == nil {
		return nil, fmt.Errorf("agent is nil")
	}

	providerConfig, err := b.GetProviderConfig(ctx, agent.ModelProvider, agent.ModelName)
	if err != nil {
		return nil, fmt.Errorf("get provider config error: %v", err)
	}
	if providerConfig == nil {
		return nil, fmt.Errorf("provider config is nil")
	}

	chatModel, err := b.BuildToolCallingChatModel(ctx, agent, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("build chat model error: %v", err)
	}

	allTools := b.BuildTools(agent)
	for _, v := range agent.Workflows {
		workflowTool := ai.NewWorkflowTool(v)
		allTools = append(allTools, workflowTool)
	}

	skills, err := b.BuildSkills(agent)
	if err != nil {
		logs.Errorf("构建skills失败: %v", err)
		return nil, err
	}

	var ragContext, memory string
	if len(optMessage) > 0 && optMessage[0] != "" {
		message := optMessage[0]
		ragContext = b.BuildRagContext(ctx, nil, message, agent)
		memory = b.BuildMemoryContext(ctx, agent, message)
	}

	return &Context{
		ProviderConfig: providerConfig,
		ChatModel:      chatModel,
		Tools:          allTools,
		Skills:         skills,
		RAGContext:     ragContext,
		Memory:         memory,
	}, nil
}

func (b *Builder) GetProviderConfig(ctx context.Context, provider string, modelName string) (*model.ProviderConfig, error) {
	if b.Loader != nil {
		return b.Loader.GetProviderConfig(ctx, provider, modelName)
	}
	return nil, fmt.Errorf("loader is nil")
}

func (b *Builder) BuildToolCallingChatModel(ctx context.Context, agent *model.Agent, config *model.ProviderConfig) (aiModel.ToolCallingChatModel, error) {
	var chatModel aiModel.ToolCallingChatModel
	var err error
	modelParams := agent.ModelParameters.ToModelParams()
	temperature := float32(modelParams.Temperature)
	topK := float32(modelParams.TopP)
	maxTokens := modelParams.MaxTokens

	if config.Provider == model.OllamaProvider {
		chatModel, err = ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
			Model:   agent.ModelName,
			BaseURL: config.APIBase,
			Options: &api.Options{
				Temperature: temperature,
				TopP:        topK,
				Runner: api.Runner{
					NumCtx: maxTokens,
				},
			},
		})
	} else if config.Provider == model.OpenAIProvider {
		chatModel, err = openai.NewChatModel(ctx, &openai.ChatModelConfig{
			Model:               agent.ModelName,
			BaseURL:             config.APIBase,
			APIKey:              config.APIKey,
			MaxCompletionTokens: &maxTokens,
			Temperature:         &temperature,
			TopP:                &topK,
		})
	} else if config.Provider == model.QwenProvider {
		chatModel, err = qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
			Model:       agent.ModelName,
			BaseURL:     config.APIBase,
			APIKey:      config.APIKey,
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
			TopP:        &topK,
		})
	} else {
		chatModel, err = openai.NewChatModel(ctx, &openai.ChatModelConfig{
			Model:               agent.ModelName,
			BaseURL:             config.APIBase,
			APIKey:              config.APIKey,
			MaxCompletionTokens: &maxTokens,
			Temperature:         &temperature,
			TopP:                &topK,
		})
	}

	return chatModel, err
}

func (b *Builder) BuildTools(agent *model.Agent) []tool.BaseTool {
	var agentTools []tool.BaseTool
	for _, v := range agent.Tools {
		switch v.ToolType {
		case model.SystemToolType:
			systemTool := tools.FindTool(v.Name)
			if systemTool == nil {
				logs.Warnf("加载系统工具时，找不到工具: %v", v.Name)
				continue
			}
			agentTools = append(agentTools, systemTool)
		case model.McpToolType:
			mcpConfig := einos.McpConfig{
				BaseUrl: v.McpConfig.Url,
				Token:   v.McpConfig.CredentialType,
				Name:    "aiDemo",
				Version: "1.0.0",
			}
			baseTools, err := mcps.GetEinoBaseTools(context.Background(), &mcpConfig)
			if err != nil {
				logs.Errorf("获取mcp tools失败: %v", err)
				continue
			}
			agentTools = append(agentTools, baseTools...)
		default:
			logs.Warnf("未知的工具类型: %v", v.ToolType)
		}
	}
	return agentTools
}

func (b *Builder) BuildSkills(agent *model.Agent) ([]adk.ChatModelAgentMiddleware, error) {
	skills := agent.Skills
	if len(skills) == 0 {
		return []adk.ChatModelAgentMiddleware{}, nil
	}
	var middlewares []adk.ChatModelAgentMiddleware

	dirToSkills := make(map[string][]*model.Skill)
	for _, sk := range skills {
		if sk.BaseDir != "" {
			dirToSkills[sk.BaseDir] = append(dirToSkills[sk.BaseDir], sk)
		}
	}

	for baseDir, sls := range dirToSkills {
		backend, _ := local.NewBackend(context.Background(), &local.Config{})
		bc, err := skill.NewBackendFromFilesystem(context.Background(), &skill.BackendFromFilesystemConfig{
			Backend: backend,
			BaseDir: baseDir,
		})
		if err != nil {
			logs.Errorf("创建技能后端失败：%v", err)
			continue
		}

		for _, sk := range sls {
			middleware, err := skill.NewMiddleware(context.Background(), &skill.Config{
				Backend:       bc,
				SkillToolName: &sk.Name,
			})
			if err != nil {
				logs.Errorf("创建技能失败：%v", err)
				continue
			}
			middlewares = append(middlewares, middleware)
		}
	}
	return middlewares, nil
}

func (b *Builder) BuildRagContext(ctx context.Context, dataChan chan string, message string, agent *model.Agent) string {
	var ragContext string
	if len(agent.KnowledgeBases) > 0 {
		var allResult []*shared.SearchKnowledgeBaseResult
		for _, v := range agent.KnowledgeBases {
			results, err := b.Loader.SearchKnowledgeBase(ctx, agent.CreatorID, message, v.ID)
			if err != nil {
				logs.Errorf("searchKnowledgeBase 搜索知识库失败: %v", err)
				continue
			}
			allResult = append(allResult, results...)
		}

		if len(allResult) > 0 {
			var contextBuilder strings.Builder
			contextBuilder.WriteString("【 参考以下知识库内容回答问题 】\n")
			for i, v := range allResult {
				contextBuilder.WriteString(fmt.Sprintf("%d.  %s \n", i+1, v.Content))
			}
			ragContext = contextBuilder.String()

			var names strings.Builder
			for _, v := range agent.KnowledgeBases {
				names.WriteString(v.Name + "\t")
			}
			buildMessage := ai.BuildMessage(agent.Name, names.String(), ragContext)
			dataChan <- buildMessage
		}
	}
	return ragContext
}

type sessionIDCtxKey struct{}

func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

func (b *Builder) BuildMemoryContext(ctx context.Context, agent *model.Agent, message string) string {
	if b == nil || b.MemoryMgr == nil {
		return ""
	}

	sessionID := b.SessionID
	if sessionID == "" {
		sessionID = SessionIDFromContext(ctx)
	}
	if sessionID == "" {
		sessionID = agent.ID.String()
	}

	// 核心逻辑点
	userID := agent.CreatorID.String()
	if err := b.MemoryMgr.AddMessage(ctx, sessionID, userID, "user", message); err != nil {
		logs.Warnf("memory add message failed: %v", err)
	}
	memory := b.MemoryMgr.RecallContext(ctx, sessionID, userID, message)
	go func() {
		if err := b.MemoryMgr.Reflect(context.Background(), sessionID, userID); err != nil {
			logs.Warnf("memory reflect failed: %v", err)
		}
	}()

	return memory
}

func (b *Builder) CreateChatModelAgentConfig(ctx context.Context, agent *model.Agent,
	history []*schema.Message, agentCtx *Context) (*adk.ChatModelAgentConfig, error) {
	////////////////////
	systemPrompt := b.selectSystemPrompt(agent)
	return &adk.ChatModelAgentConfig{
		Model:       agentCtx.ChatModel,
		Name:        agent.Name,
		Description: agent.Description,
		Instruction: systemPrompt,
		GenModelInput: func(ctx context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
			optional := false
			if len(history) == 0 {
				optional = true
			}
			template := prompt.FromMessages(schema.FString,
				schema.SystemMessage(systemPrompt),
				schema.MessagesPlaceholder("history_key", optional),
			)

			messages, err2 := template.Format(ctx, map[string]any{
				"history_key": history,
			})
			if err2 != nil {
				logs.Errorf("格式化模板失败: %v", err2)
				return nil, err2
			}
			messages = append(messages, input.Messages...)
			return messages, nil
		},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentCtx.Tools,
			},
		},
		Handlers: agentCtx.Skills,
	}, nil
}

// TODO 做更多的 Agent 身份拓展
func (b *Builder) selectSystemPrompt(agent *model.Agent) string {
	systemPrompt := ai.SystemPrompt
	if agent.Name == "AI运维" || agent.Name == "OpsMaster" {
		systemPrompt = ai.DevOpsSystemPrompt
	}
	return systemPrompt
}
