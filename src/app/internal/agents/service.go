package agents

import (
	"agentmem"
	"app/shared"
	"common/biz"
	"context"
	"core/ai"
	"core/ai/deepagent"
	"core/ai/mcps"
	"core/ai/multiagent"
	"core/ai/reflection"
	"core/ai/store"
	"core/ai/tools"
	"encoding/json"
	"errors"
	"fmt"
	"model"
	"strings"
	"sync"
	"time"

	"basemodel/ai/einos"
	"basemodel/database"
	"basemodel/errs"
	"basemodel/event"
	"basemodel/logs"

	"github.com/cloudwego/eino-ext/a2a/client"
	"github.com/cloudwego/eino-ext/a2a/extension/eino"
	"github.com/cloudwego/eino-ext/a2a/transport/jsonrpc"
	"github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	aiModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/ollama/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type service struct {
	repo             repository
	stateMutex       sync.RWMutex
	pendingAnswer    map[string]string
	waitingStates    map[string]bool
	checkPointStore  compose.CheckPointStore
	deepAgentFactory *deepagent.Factory
	memoryMgr        *agentmem.MemoryManager
}

func (s *service) LoadAgent(ctx context.Context, agentId uuid.UUID) (*model.Agent, error) {
	return s.repo.getAgentById(ctx, agentId)
}

func (s *service) GetProviderConfig(ctx context.Context, provider string, modelName string) (*model.ProviderConfig, error) {
	return s.getProviderConfig(ctx, model.LLMTypeChat, provider, modelName)
}

func (s *service) SearchKnowledgeBase(ctx context.Context, userId uuid.UUID, query string, kbId uuid.UUID) ([]*shared.SearchKnowledgeBaseResult, error) {
	return s.searchKnowledgeBase(ctx, userId, query, kbId)
}

func (s *service) createAgent(ctx context.Context, userId uuid.UUID, req CreateAgentReq) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	agent := model.DefaultAgent(userId, req.Name, req.Description, req.Status)
	if agent.Mode == "" {
		agent.Mode = req.Mode
	}

	if req.Mode == model.DeepAgentMode && req.DeepConfig != nil {
		agent.DeepConfig = req.DeepConfig
	}
	err := s.repo.createAgent(ctx, agent)
	if err != nil {
		logs.Errorf("创建智能代理失败: %v", err)
		return nil, errs.DBError
	}
	return agent, nil
}

func (s *service) listAgents(ctx context.Context, userID uuid.UUID, req SearchAgentReq) (*ListAgentResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter := AgentFilter{
		Name:   req.Params.Name,
		Status: req.Params.Status,
		Limit:  req.Params.PageSize,
		Offset: (req.Params.Page - 1) * req.Params.PageSize,
	}
	list, total, err := s.repo.listAgents(ctx, userID, filter)
	if err != nil {
		logs.Errorf("查询智能代理列表失败: %v", err)
		return nil, errs.DBError
	}
	return &ListAgentResponse{
		Agents: list,
		Total:  total,
	}, nil
}

func (s *service) getAgent(ctx context.Context, userID uuid.UUID, id uuid.UUID) (*model.Agent, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	agent, err := s.repo.getAgent(ctx, userID, id)
	if err != nil {
		logs.Errorf("查询智能代理失败: %v", err)
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}
	return agent, nil
}

func (s *service) updateAgent(ctx context.Context, userId uuid.UUID, req UpdateAgentReq) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	agent, err := s.repo.getAgent(ctx, userId, req.ID)
	if err != nil {
		logs.Errorf("查询智能代理失败: %v", err)
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}

	if req.Name != "" {
		agent.Name = req.Name
	}
	if req.Description != "" {
		agent.Description = req.Description
	}
	if req.Status != "" {
		agent.Status = req.Status
	}
	if req.SystemPrompt != "" {
		agent.SystemPrompt = req.SystemPrompt
	}
	if req.ModelProvider != "" {
		agent.ModelProvider = req.ModelProvider
	}
	if req.ModelName != "" {
		agent.ModelName = req.ModelName
	}
	if req.ModelParameters != nil {
		agent.ModelParameters = req.ModelParameters
	}
	if req.OpeningDialogue != "" {
		agent.OpeningDialogue = req.OpeningDialogue
	}
	if req.Mode != "" {
		agent.Mode = req.Mode
	}
	if req.DeepConfig != nil {
		agent.DeepConfig = req.DeepConfig
	}
	err = s.repo.updateAgent(ctx, agent)
	if err != nil {
		logs.Errorf("更新智能代理失败: %v", err)
		return nil, errs.DBError
	}
	return agent, nil
}

func (s *service) agentMessage(ctx context.Context, userID uuid.UUID, req AgentMessageReq) (<-chan string, <-chan error) {
	dataChan := make(chan string)
	errChan := make(chan error)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logs.Errorf("处理智能代理消息失败: %v", err)
				select {
				case errChan <- errors.New("internal server error"):
				case <-ctx.Done():
					logs.Warnf("发送取消 context Done")
				}
			}
			close(dataChan)
			close(errChan)
		}()

		agent, err := s.repo.getAgent(ctx, userID, req.AgentID)
		if err != nil {
			logs.Errorf("查询智能代理失败: %v", err)
			s.sendError(ctx, errChan, err)
			return
		}
		switch agent.Mode {
		case model.DeepAgentMode:
			s.handleDeepAgent(ctx, userID, req, agent, dataChan, errChan)
		case model.GeneralAgentMode:
			s.handleNormalAgent(ctx, userID, req, agent, dataChan, errChan)
		}
	}()
	return dataChan, errChan
}

func (s *service) sendError(ctx context.Context, errChan chan error, err error) {
	select {
	case errChan <- err:
	case <-ctx.Done():
		logs.Warnf("发送取消 context Done")
	}
}

func (s *service) buildMainAgent(ctx context.Context, agent *model.Agent, history []*schema.Message, message string, dataChan chan string, sessionID string, userID string) (adk.Agent, error) {
	providerConfig, err := s.getProviderConfig(ctx, model.LLMTypeChat, agent.ModelProvider, agent.ModelName)
	if err != nil {
		return nil, errs.DBError
	}
	if providerConfig == nil {
		return nil, biz.ErrProviderConfigNotFound
	}

	chatModel, err := s.buildToolCallingChatModel(ctx, agent, providerConfig)
	if err != nil {
		logs.Errorf("构建chatmodel失败: %v", err)
		return nil, err
	}

	var allTools []tool.BaseTool
	allTools = append(allTools, s.buildTools(agent)...)
	for _, v := range agent.Workflows {
		workflowTool := ai.NewWorkflowTool(v)
		allTools = append(allTools, workflowTool)
	}

	skills, err := s.buildSkills(agent)
	if err != nil {
		logs.Errorf("构建skills失败: %v", err)
		return nil, err
	}
	systemPrompt := ai.SystemPrompt
	if agent.Name == "AI运维" || agent.Name == "OpsMaster" {
		systemPrompt = ai.DevOpsSystemPrompt
	}

	ragContext := s.buildRagContext(ctx, dataChan, message, agent)

	var memory string
	if s.memoryMgr != nil {
		if err := s.memoryMgr.AddMessage(ctx, sessionID, userID, "user", message); err != nil {
			logs.Warnf("memory add message failed: %v", err)
		}
		memory = s.memoryMgr.RecallContext(ctx, sessionID, userID, message)
		go func() {
			if err := s.memoryMgr.Reflect(context.Background(), sessionID, userID); err != nil {
				logs.Warnf("memory reflect failed: %v", err)
			}
		}()
	}

	modelAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Model:       chatModel,
		Name:        agent.Name,
		Description: agent.Description,
		Instruction: systemPrompt,
		GenModelInput: func(ctx context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
			optional := false
			if len(history) == 0 {
				optional = true
			}

			// 模板引擎构建消息模板
			template := prompt.FromMessages(schema.FString,
				schema.SystemMessage(systemPrompt),
				schema.MessagesPlaceholder("history_key", optional),
			)
			messages, _err := template.Format(ctx, map[string]any{
				"role":        agent.SystemPrompt,
				"ragContext":  ragContext,
				"memory":      memory,
				"toolsInfo":   s.formatToolsInfo(allTools),
				"agentsInfo":  s.formatAgentsDescription(agent.Agents),
				"history_key": history,
			})
			if _err != nil {
				logs.Errorf("格式化模板失败: %v", _err)
				return nil, _err
			}
			messages = append(messages, input.Messages...)
			return messages, nil
		},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: allTools,
			},
		},
		Handlers: skills,
	})
	if err != nil {
		logs.Errorf("构建ChatModelAgent失败: %v", err)
		return nil, err
	}
	return modelAgent, nil
}

func (s *service) getProviderConfig(ctx context.Context, chat model.LLMType, provider string, name string) (*model.ProviderConfig, error) {
	trigger, err := event.Trigger("getProviderConfig", &shared.GetProviderConfigsRequest{
		Provider:  provider,
		ModelName: name,
		LLMType:   chat,
	})
	if err != nil {
		logs.Errorf("触发getProviderConfig事件失败: %v", err)
		return nil, errs.DBError
	}
	return trigger.(*model.ProviderConfig), nil
}

func (s *service) buildToolCallingChatModel(ctx context.Context, agent *model.Agent, config *model.ProviderConfig) (aiModel.ToolCallingChatModel, error) {
	var chatModel aiModel.ToolCallingChatModel
	var err error
	modelParams := agent.ModelParameters.ToModelParams()
	temperature := float32(modelParams.Temperature)
	topP := float32(modelParams.TopP)
	maxTokens := modelParams.MaxTokens

	if config.Provider == model.OllamaProvider {
		chatModel, err = ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
			Model:   agent.ModelName,
			BaseURL: config.APIBase,
			Options: &api.Options{
				Temperature: temperature,
				TopP:        topP,
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
			TopP:                &topP,
		})
	} else if config.Provider == model.QwenProvider {
		chatModel, err = qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
			Model:       agent.ModelName,
			BaseURL:     config.APIBase,
			APIKey:      config.APIKey,
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
			TopP:        &topP,
		})
	} else {
		chatModel, err = openai.NewChatModel(ctx, &openai.ChatModelConfig{
			Model:               agent.ModelName,
			BaseURL:             config.APIBase,
			APIKey:              config.APIKey,
			MaxCompletionTokens: &maxTokens,
			Temperature:         &temperature,
			TopP:                &topP,
		})
	}
	return chatModel, err
}

func (s *service) sendData(ctx context.Context, dataChan chan string, data string) {
	select {
	case dataChan <- data:
	case <-ctx.Done():
		logs.Warnf("sendData 发送取消 context Done")
	}
}

func (s *service) updateAgentTool(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, req UpdateAgentToolReq) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	agent, err := s.repo.getAgent(ctx, userID, agentId)
	if err != nil {
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}
	if len(req.Tools) <= 0 {
		return nil, biz.ErrToolNotExisted
	}

	// 删除agent现有关联的工具
	err = s.repo.deleteAgentTools(ctx, agentId)
	if err != nil {
		return nil, errs.DBError
	}

	var agentTools []*model.AgentTool
	var toolIds []uuid.UUID
	for _, v := range req.Tools {
		toolIds = append(toolIds, v.ID)
	}

	toolsList, err := s.getToolsByIds(toolIds)
	for _, t := range toolsList {
		agentTools = append(agentTools, &model.AgentTool{
			AgentID:   agentId,
			ToolID:    t.ID,
			Status:    model.Enabled,
			CreatedAt: time.Now(),
		})
	}
	err = s.repo.createAgentTools(ctx, agentTools)
	if err != nil {
		logs.Errorf("批量插入agent_tools失败: %v", err)
		return nil, errs.DBError
	}
	return agentTools, nil
}

func (s *service) getToolsByIds(ids []uuid.UUID) ([]*model.Tool, error) {
	trigger, err := event.Trigger("getToolsByIds", &shared.GetToolsByIdsRequest{
		Ids: ids,
	})
	return trigger.([]*model.Tool), err
}

func (s *service) buildTools(agent *model.Agent) []tool.BaseTool {
	var agentTools []tool.BaseTool
	for _, v := range agent.Tools {
		switch v.ToolType {
		case model.SystemToolType:
			systemTool := s.loadSystemTool(v.Name)
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

func (s *service) loadSystemTool(name string) tool.BaseTool {
	return tools.FindTool(name)
}

func (s *service) formatToolsInfo(allTools []tool.BaseTool) string {
	var builder strings.Builder
	builder.WriteString("【可用工具列表】\n")
	for _, t := range allTools {
		info, _ := t.Info(context.Background())
		builder.WriteString(fmt.Sprintf("- name: `%s` \n", info.Name))
		builder.WriteString(fmt.Sprintf("  description: `%s` \n", info.Desc))

		marshal, _ := json.Marshal(info.ParamsOneOf)
		builder.WriteString(fmt.Sprintf("  params: `%s` \n", string(marshal)))
	}
	return builder.String()
}

func (s *service) addAgentKnowledgeBase(ctx context.Context, userId uuid.UUID, agentId uuid.UUID, addReq addAgentKnowledgeBaseReq) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	agent, err := s.repo.getAgent(ctx, userId, agentId)
	if err != nil {
		logs.Errorf("addAgentKnowledgeBase 获取agent失败: %v", err)
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}

	kb, err := s.getKnowledgeBase(ctx, userId, addReq.KnowledgeBaseID)
	if err != nil {
		logs.Errorf("addAgentKnowledgeBase 获取知识库失败: %v", err)
		return nil, errs.DBError
	}
	if kb == nil {
		return nil, biz.ErrKnowledgeBaseNotFound
	}

	exist, err := s.repo.isAgentKnowledgeBaseExist(ctx, agentId, addReq.KnowledgeBaseID)
	if err != nil {
		logs.Errorf("addAgentKnowledgeBase 查询关联关系是否存在失败: %v", err)
		return nil, errs.DBError
	}

	if exist {
		return nil, nil
	}
	err = s.repo.createAgentKnowledgeBase(ctx, &model.AgentKnowledgeBase{
		AgentID:         agentId,
		KnowledgeBaseId: addReq.KnowledgeBaseID,
		Status:          model.AgentKnowledgeStatusEnabled,
	})
	if err != nil {
		logs.Errorf("addAgentKnowledgeBase 创建关联关系失败: %v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) getKnowledgeBase(ctx context.Context, userId uuid.UUID, kbId uuid.UUID) (*model.KnowledgeBase, error) {
	trigger, err := event.Trigger("getKnowledgeBase", &shared.GetKnowledgeBaseRequest{
		UserId:          userId,
		KnowledgeBaseId: kbId,
	})
	return trigger.(*model.KnowledgeBase), err
}

func (s *service) deleteAgentKnowledgeBase(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, kbId uuid.UUID) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	err := s.repo.deleteAgentKnowledgeBase(ctx, agentId, kbId)
	if err != nil {
		logs.Errorf("deleteAgentKnowledgeBase 删除关联关系失败: %v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) buildRagContext(ctx context.Context, dataChan chan string, message string, agent *model.Agent) string {
	var ragContext string
	if len(agent.KnowledgeBases) > 0 {
		var allResult []*shared.SearchKnowledgeBaseResult
		for _, v := range agent.KnowledgeBases {
			results, err := s.searchKnowledgeBase(ctx, agent.CreatorID, message, v.ID)
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
				if i >= 1 {
					break
				}
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

func (s *service) searchKnowledgeBase(ctx context.Context, userId uuid.UUID, message string, id uuid.UUID) ([]*shared.SearchKnowledgeBaseResult, error) {
	trigger, err := event.Trigger("searchKnowledgeBase", &shared.SearchKnowledgeBaseRequest{
		UserId:          userId,
		KnowledgeBaseId: id,
		Query:           message,
	})
	if err != nil {
		logs.Errorf("searchKnowledgeBase 搜索知识库失败: %v", err)
		return nil, err
	}
	response := trigger.(*shared.SearchKnowledgeBaseResponse)
	return response.Results, nil
}

func (s *service) addAgentAgent(ctx context.Context, userId uuid.UUID, request AgentMarketRequest) (any, error) {
	agent, err := s.repo.getAgent(ctx, userId, request.AgentId)
	if err != nil {
		logs.Errorf("addAgentAgent 获取agent失败: %v", err)
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}

	for _, v := range request.AgentMarketIds {
		aa, err := s.repo.getAgentAgent(ctx, request.AgentId, v)
		if err != nil {
			logs.Errorf("addAgentAgent 获取agent失败: %v", err)
			return nil, errs.DBError
		}
		if aa != nil {
			continue
		}
		aa = &model.AgentAgent{
			AgentId:       request.AgentId,
			AgentMarketId: v,
		}
		err = s.repo.createAgentAgent(ctx, aa)
		if err != nil {
			logs.Errorf("addAgentAgent 创建关联关系失败: %v", err)
			return nil, errs.DBError
		}
	}
	return nil, nil
}

func (s *service) deleteAgentAgent(ctx context.Context, userID uuid.UUID, request DeleteAgentMarketRequest) (any, error) {
	err := s.repo.deleteAgentAgent(ctx, request.AgentId, request.AgentMarketId)
	if err != nil {
		logs.Errorf("deleteAgentAgent 删除关联关系失败: %v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) formatAgentsDescription(agents []*model.AgentMarket) string {
	var builder strings.Builder
	builder.WriteString("【 可调用的智能体列表 】\n")
	for _, v := range agents {
		builder.WriteString(fmt.Sprintf("- name: %s \n", v.Name))
		builder.WriteString(fmt.Sprintf("- desc: %s \n", v.Description))
	}
	return builder.String()
}

func (s *service) addWorkflowToAgent(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, reqs addWorkflowToAgentReq) (any, error) {
	agent, err := s.repo.getAgent(ctx, userID, agentId)
	if err != nil {
		logs.Errorf("addWorkflowToAgent 获取agent失败: %v", err)
		return nil, errs.DBError
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}
	agentWorkflow, err := s.repo.getAgentWorkflow(ctx, agentId, reqs.WorkflowID)
	if err != nil {
		logs.Errorf("addWorkflowToAgent 获取agent_workflow失败: %v", err)
		return nil, errs.DBError
	}
	if agentWorkflow != nil {
		return nil, nil
	}

	agentWorkflow = &model.AgentWorkflow{
		AgentID:    agentId,
		WorkflowID: reqs.WorkflowID,
		IsDefault:  reqs.IsDefault,
		Priority:   reqs.Priority,
		Status:     reqs.Status,
		CreatedAt:  time.Now(),
	}
	err = s.repo.createAgentWorkflow(ctx, agentWorkflow)
	if err != nil {
		logs.Errorf("addWorkflowToAgent 创建关联关系失败: %v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) deleteWorkflowFromAgent(ctx context.Context, agentId uuid.UUID, workflowId uuid.UUID) error {
	err := s.repo.deleteAgentWorkflow(ctx, agentId, workflowId)
	if err != nil {
		logs.Errorf("deleteWorkflowFromAgent 删除关联关系失败: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) deleteAgent(ctx context.Context, id uuid.UUID) error {
	err := s.repo.transaction(ctx, func(tx *gorm.DB) error {
		err := s.repo.deleteAgent(ctx, id)
		if err != nil {
			return err
		}
		err = s.repo.deleteAgentTools(ctx, id)
		if err != nil {
			return err
		}
		err = s.repo.deleteAgentKnowledgeBaseByAgentId(ctx, id)
		if err != nil {
			return err
		}
		err = s.repo.deleteAgentAgentByAgentId(ctx, id)
		if err != nil {
			return err
		}
		err = s.repo.deleteAgentWorkflowByAgentId(ctx, id)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logs.Errorf("deleteAgent 删除agent失败: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) createSession(ctx context.Context, userId uuid.UUID, param createSessionRequest) (*chatSessionResponse, error) {
	session := &model.ChatSession{
		BaseModel: model.BaseModel{
			ID: uuid.New(),
		},
		AgentID: param.AgentID,
		Title:   param.Title,
		UserID:  userId,
	}
	err := s.repo.createSession(ctx, session)
	if err != nil {
		logs.Errorf("createSession 创建session失败: %v", err)
		return nil, errs.DBError
	}
	return toChatSessionResponse(session), nil
}

func (s *service) listSessions(ctx context.Context, userID uuid.UUID, agentId uuid.UUID) ([]*model.ChatSession, error) {
	list, err := s.repo.listSessions(ctx, userID, agentId)
	if err != nil {
		logs.Errorf("listSessions 获取session列表失败: %v", err)
		return nil, errs.DBError
	}
	return list, nil
}

func (s *service) getSessionMessages(ctx context.Context, sessionId uuid.UUID) ([]*chatMessageResponse, error) {
	list, err := s.repo.getSessionMessages(ctx, sessionId)
	if err != nil {
		logs.Errorf("getSessionMessages 获取session消息列表失败: %v", err)
		return nil, errs.DBError
	}
	return toChatMessageResponses(list), nil
}

func (s *service) deleteSession(ctx context.Context, sessionId uuid.UUID) error {
	err := s.repo.transaction(ctx, func(tx *gorm.DB) error {
		err := s.repo.deleteSession(ctx, sessionId)
		if err != nil {
			return err
		}
		err = s.repo.deleteSessionMessages(ctx, sessionId)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logs.Errorf("deleteSession 删除session失败: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) saveChatMessage(sessionId uuid.UUID, message string, roleType schema.RoleType) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	chatMessage := &model.ChatMessage{
		BaseModel: model.BaseModel{
			ID: uuid.New(),
		},
		SessionID: sessionId,
		Role:      string(roleType),
		Content:   message,
	}
	err := s.repo.saveChatMessage(ctx, chatMessage)
	if err != nil {
		logs.Errorf("saveChatMessage 保存session消息失败: %v", err)
	}
}

func (s *service) buildSkills(agent *model.Agent) ([]adk.ChatModelAgentMiddleware, error) {
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

	// 为每个baseDir创建一个backend并加载skill
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

func (s *service) addSkillToAgent(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, reqs AddAgentSkillReq) (any, error) {
	agent, err := s.repo.getAgent(ctx, userID, agentId)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, biz.AgentNotFound
	}

	err = s.repo.transaction(ctx, func(tx *gorm.DB) error {
		for _, skillID := range reqs.SkillIDs {
			existed, err := s.repo.getAgentSkill(ctx, agentId, skillID)
			if err != nil {
				return err
			}
			if existed == nil {
				agentSkill := &model.AgentSkill{
					AgentID:   agentId,
					SkillID:   skillID,
					Status:    "active",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				err := s.repo.saveAgentSkill(ctx, agentSkill)
				if err != nil {
					logs.Errorf("保存技能关联失败：%v", err)
					return err
				}
			} else {
				existed.Status = "active"
				existed.UpdatedAt = time.Now()
				err := s.repo.updateAgentSkill(ctx, existed)
				if err != nil {
					logs.Errorf("更新技能关联失败：%v", err)
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		logs.Errorf("添加技能失败：%v", err)
		return nil, err
	}
	return nil, nil
}

func (s *service) deleteSkillFromAgent(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, skillId uuid.UUID) error {
	agent, err := s.repo.getAgent(ctx, userID, agentId)
	if err != nil {
		return err
	}
	if agent == nil {
		return biz.AgentNotFound
	}
	err = s.repo.deleteAgentSkill(ctx, agentId, skillId)
	if err != nil {
		logs.Errorf("删除技能失败：%v", err)
		return err
	}
	return nil
}

func (s *service) deleteAgentTool(ctx context.Context, userID uuid.UUID, agentId uuid.UUID, toolId uuid.UUID) error {
	err := s.repo.deleteAgentTool(ctx, agentId, toolId)
	if err != nil {
		logs.Errorf("删除工具失败：%v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) handleNormalAgent(ctx context.Context, userID uuid.UUID, req AgentMessageReq, agent *model.Agent, dataChan chan string, errChan chan error) {
	var session *model.ChatSession
	var err error
	if req.SessionId != nil {
		session, err = s.repo.getSession(ctx, req.SessionId)
		if err != nil {
			logs.Errorf("查询会话失败: %v", err)
			s.sendError(ctx, errChan, err)
			return
		}
	} else {
		session = &model.ChatSession{
			BaseModel: model.BaseModel{
				ID: uuid.New(),
			},
			AgentID: agent.ID,
			UserID:  userID,
			Title:   req.Message,
		}
		err = s.repo.createSession(ctx, session)
		if err != nil {
			logs.Errorf("创建会话失败: %v", err)
		} else {
			sessionInfo, _ := json.Marshal(map[string]any{
				"action":    "session_created",
				"sessionId": session.ID,
				"title":     session.Title,
			})
			s.sendData(ctx, dataChan, string(sessionInfo))
		}
	}

	var history []*schema.Message
	messages, err := s.repo.getSessionMessages(ctx, session.ID)
	if err != nil {
		logs.Errorf("查询会话历史消息失败: %v", err)
	} else {
		for _, v := range messages {
			switch v.Role {
			case string(schema.User):
				history = append(history, schema.UserMessage(v.Content))
			case string(schema.Assistant):
				history = append(history, schema.AssistantMessage(v.Content, nil))
			case string(schema.System):
				history = append(history, schema.SystemMessage(v.Content))
			}
		}
	}
	go s.saveChatMessage(session.ID, req.Message, schema.User)

	// 构建一个主agent
	mainAgent, err := s.buildMainAgent(ctx, agent, history, req.Message, dataChan, session.ID.String(), userID.String())
	if err != nil {
		logs.Errorf("构建主智能体失败: %v", err)
		s.sendError(ctx, errChan, err)
		return
	}
	// 构建子Agent
	var subAgents []adk.Agent
	for _, v := range agent.Agents {
		t, err := jsonrpc.NewTransport(ctx, &jsonrpc.ClientConfig{
			BaseURL:     v.URL,
			HandlerPath: v.HandlerPath,
		})
		if err != nil {
			logs.Errorf("构建子智能体失败: %v", err)
			continue
		}

		aClient, err := client.NewA2AClient(ctx, &client.Config{
			Transport: t,
		})
		if err != nil {
			logs.Errorf("构建子智能体失败: %v", err)
			continue
		}

		newAgent, err := eino.NewAgent(ctx, eino.AgentConfig{
			Client: aClient,
		})
		if err != nil {
			logs.Errorf("构建子智能体失败: %v", err)
			continue
		}
		subAgents = append(subAgents, newAgent)
	}

	// ========================================================================
	// 多 Agent 辩论模式：当有 >=2 个子 Agent 时自动触发
	// 每个 agent 独立 runner → 并行回答 → 互评修订 → LLM 投票选最佳
	// ========================================================================
	if len(subAgents) >= 2 {
		logs.Infof("handleNormalAgent: %d sub-agents detected, entering Debate mode", len(subAgents))

		// 把 mainAgent + 所有 subAgents 各自包装成独立 RunnerAgent
		runnerAgents := make([]*multiagent.RunnerAgent, 0, 1+len(subAgents))
		mainRunner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: mainAgent})
		runnerAgents = append(runnerAgents, multiagent.NewRunnerAgent(agent.Name, mainRunner))
		for i, sa := range subAgents {
			r := adk.NewRunner(ctx, adk.RunnerConfig{Agent: sa})
			runnerAgents = append(runnerAgents, multiagent.NewRunnerAgent(fmt.Sprintf("sub_%d", i), r))
		}

		// 构建 Debate，可选：用 Ollama 3B 做 LLM 投票
		debateCfg := multiagent.DefaultDebateConfig()
		if voteModel := s.buildCritiqueChatModel(ctx); voteModel != nil {
			debateCfg.VoteModel = voteModel
			logs.Infof("handleNormalAgent: Debate vote model enabled (Ollama)")
		}

		debate := multiagent.NewDebate(runnerAgents, debateCfg)
		debateIter := debate.Stream(ctx, req.Message)

		// 消费 Debate 流式事件 → 推给前端
		var finalWinnerAns string
		for {
			select {
			case <-ctx.Done():
				logs.Warnf("客户端取消了请求")
				return
			default:
			}

			ev, ok := debateIter.Next()
			if !ok {
				break
			}
			if ev.Err != nil {
				s.sendData(ctx, dataChan, ai.BuildErrMessage("", ev.Err.Error()))
				return
			}

			// 转成 JSON 推前端
			payload, _ := json.Marshal(ev)
			logs.Infof("Debate event: type=%s round=%d agent=%s", ev.Type, ev.Round, ev.AgentName)
			s.sendData(ctx, dataChan, string(payload))

			if ev.Type == multiagent.EventVote {
				finalWinnerAns = ev.WinnerAns
			}
		}

		// 保存获胜答案
		if finalWinnerAns != "" {
			go s.saveChatMessage(session.ID, finalWinnerAns, schema.Assistant)
		}
		return
	}

	// 构建supervisoragent
	supervisorAgent, err := supervisor.New(ctx, &supervisor.Config{
		Supervisor: mainAgent,
		SubAgents:  subAgents,
	})
	if err != nil {
		logs.Errorf("构建supervisorAgent失败: %v", err)
		s.sendError(ctx, errChan, err)
		return
	}

	// 构建Runner
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           supervisorAgent,
		EnableStreaming: true,
	})

	// 可选：SelfCritique 自检迭代（用本地 Ollama 小模型做评审）
	type eventIter interface {
		Next() (*adk.AgentEvent, bool)
	}
	var iter eventIter = runner.Query(ctx, req.Message)
	if critiqueModel := s.buildCritiqueChatModel(ctx); critiqueModel != nil {
		critique := reflection.NewSelfCritique(critiqueModel, reflection.DefaultCritiqueConfig())
		if critique.Enabled() {
			loop := reflection.NewSelfCritiqueLoop(runner, critique)
			iter = loop.Query(ctx, req.Message)
			logs.Infof("handleNormalAgent: SelfCritiqueLoop enabled (max_rounds=%d)", reflection.DefaultCritiqueConfig().MaxRounds)
		}
	}

	for {
		events, ok := iter.Next()
		if !ok {
			break
		}

		select {
		case <-ctx.Done():
			logs.Warnf("客户端取消了请求")
			return
		default:
		}
		if events.Err != nil {
			s.sendData(ctx, dataChan, ai.BuildErrMessage(events.AgentName, events.Err.Error()))
			return
		}

		if events.Output != nil && events.Output.MessageOutput != nil {
			msg, err := events.Output.MessageOutput.GetMessage()
			if err != nil {
				logs.Errorf("获取模型返回内容失败: %v", err)
				s.sendError(ctx, errChan, err)
				return
			}
			if msg.Content == "" && msg.ReasoningContent == "" {
				continue
			}
			if msg.ReasoningContent != "" {
				s.sendData(ctx, dataChan, ai.BuildReasoningMessage(events.AgentName, msg.ToolName, msg.ReasoningContent))
			}
			logs.Infof("Agent名称[%s], 工具名称:[%s], 模型返回内容: %s", events.AgentName, msg.ToolName, msg.Content)
			if msg.Content != "" {
				go s.saveChatMessage(session.ID, msg.Content, schema.Assistant)
				s.sendData(ctx, dataChan, ai.BuildMessage(events.AgentName, msg.ToolName, msg.Content))
			}
		}
	}
}

// buildCritiqueChatModel 构建轻量评审模型（Ollama 3B 小模型）
// 返回 eino 原生 BaseChatModel —— ollama.ChatModel 实现此接口
func (s *service) buildCritiqueChatModel(ctx context.Context) aiModel.BaseChatModel {
	cfg, err := s.getProviderConfig(ctx, model.LLMTypeChat, model.OllamaProvider, "qwen2.5:3b")
	if err != nil || cfg == nil {
		return nil
	}
	chatModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		Model:   cfg.Name,
		BaseURL: cfg.APIBase,
		Options: &api.Options{
			Temperature: 0.2,
			Runner:      api.Runner{NumCtx: 8192},
		},
	})
	if err != nil {
		logs.Warnf("buildCritiqueChatModel: ollama.NewChatModel failed: %v", err)
		return nil
	}
	return chatModel
}

func (s *service) handleDeepAgent(ctx context.Context, userID uuid.UUID, req AgentMessageReq, agent *model.Agent, dataChan chan string, errChan chan error) {
	var executor func(context.Context, *model.ChatSession, chan string, chan error) error
	executor = func(ctx context.Context, session *model.ChatSession, dataChan chan string, errChan chan error) error {
		factory := s.deepAgentFactory
		deepAgent, err := factory.Create(ctx, &deepagent.UniversalDeepAgentConfig{
			Name:           agent.Name,
			Description:    agent.Description,
			SubAgentLoader: s,
			Agent:          agent,
			SystemPrompt:   agent.SystemPrompt,
			MemoryManager:  s.memoryMgr,
			UserID:         userID.String(),
		})
		if err != nil {
			logs.Errorf("创建深度代理失败：%v", err)
			return fmt.Errorf("创建深度代理失败")
		}

		// Memory AddMessage + Recall + Reflect 统一在 ChatStream 里做
		eventChan, err := deepAgent.ChatStream(ctx, session.ID.String(), req.Message)
		if err != nil {
			logs.Errorf("深度代理聊天失败：%v", err)
			return fmt.Errorf("深度代理聊天失败")
		}

		subAgentName := make(map[string]string)
		for eve := range eventChan {
			if eve.Err != nil {
				logs.Errorf("深度代理聊天失败：%v", eve.Err)
				s.sendData(ctx, dataChan, ai.BuildErrMessage(agent.Name, eve.Err.Error()))
				continue
			}
			if eve.Output != nil {
				if eve.Output.MessageOutput != nil {
					msg, err := eve.Output.MessageOutput.GetMessage()
					if err == nil && msg != nil {
						if msg.Role == schema.Tool {
							toolName, ok := subAgentName[msg.ToolCallID]
							if !ok {
								toolName = msg.ToolName
							}
							responseMsg := ai.BuildMessage(agent.Name, toolName, msg.Content)
							s.sendData(ctx, dataChan, responseMsg)
							if msg.Content != "" {
								go s.saveChatMessage(session.ID, responseMsg, schema.Assistant)
							}
						} else if len(msg.ToolCalls) > 0 {
							for _, tc := range msg.ToolCalls {
								name := tc.Function.Name
								if name == "task" {
									args := tc.Function.Arguments
									if args != "" {
										var subAgent deepagent.SubAgent
										err = json.Unmarshal([]byte(args), &subAgent)
										if err != nil {
											logs.Errorf("解析子代理参数失败：%v", err)
											continue
										}
										if subAgent.SubagentType != "" {
											tn := "子Agent- " + subAgent.SubagentType
											subAgentName[tc.ID] = tn
										}
									}
								}
							}
							s.handleDeepAgentMessage(ctx, agent, msg, dataChan)
							if msg.Content != "" {
								toolCallMsg := ai.BuildMessage(agent.Name, msg.ToolName, msg.Content)
								go s.saveChatMessage(session.ID, toolCallMsg, schema.Assistant)
							}
						} else if msg.Content != "" {
							normalMsg := ai.BuildMessage(agent.Name, msg.ToolName, msg.Content)
							s.sendData(ctx, dataChan, normalMsg)
							if msg.Content != "" {
								go s.saveChatMessage(session.ID, normalMsg, schema.Assistant)
							}
						}
					}
				}
				if eve.Action != nil {
					if eve.Action.TransferToAgent != nil {
						transferContent := fmt.Sprintf("正在将任务转交给%s",
							eve.Action.TransferToAgent.DestAgentName)
						transferMsg := ai.BuildMessage(agent.Name, "transfer_to_agent", transferContent)
						s.sendData(ctx, dataChan, transferMsg)
						go s.saveChatMessage(session.ID, transferMsg, schema.Assistant)
					}
					if eve.Action.Interrupted != nil {
						if len(eve.Action.Interrupted.InterruptContexts) > 0 {
							for _, interruptCtx := range eve.Action.Interrupted.InterruptContexts {
								if interruptCtx.Info != nil {
									info := interruptCtx.Info.(map[string]any)
									if question, ok := info["question"].(string); ok {
										msg := ai.BuildMessage(agent.Name, "interrupt", question)
										s.sendData(ctx, dataChan, msg)
										break
									}
								}
							}
						}
						return nil
					}
					if eve.Action.Exit {
						exitMsg := ai.BuildMessage(agent.Name, "exit", "任务执行完成")
						s.sendData(ctx, dataChan, exitMsg)
						go s.saveChatMessage(session.ID, exitMsg, schema.Assistant)
					}
				}
				if eve.Output != nil && eve.Output.MessageOutput != nil {
					msg, err := eve.Output.MessageOutput.GetMessage()
					if err == nil && msg != nil && msg.ReasoningContent != "" {
						reasonMsg := ai.BuildMessage(agent.Name, msg.ToolName, msg.ReasoningContent)
						s.sendData(ctx, dataChan, reasonMsg)
					}
				}
			}
		}
		return nil
	}
	s.handleAgentMessage(ctx, userID, req, agent, dataChan, errChan, executor)
}

type agentExecutionHandler func(ctx context.Context, session *model.ChatSession, dataChan chan string, errChan chan error) error

func (s *service) handleAgentMessage(ctx context.Context, userID uuid.UUID, req AgentMessageReq, agent *model.Agent,
	dataChan chan string, errChan chan error, executor agentExecutionHandler) {
	//////////////////////
	var session *model.ChatSession
	var err error
	if req.SessionId != nil {
		session, err = s.repo.getSession(ctx, req.SessionId)
		if err != nil {
			logs.Errorf("查询会话失败: %v", err)
			s.sendError(ctx, errChan, err)
			return
		}
	} else {
		session = &model.ChatSession{
			BaseModel: model.BaseModel{
				ID: uuid.New(),
			},
			AgentID: agent.ID,
			UserID:  userID,
			Title:   req.Message,
		}
		err = s.repo.createSession(ctx, session)
		if err != nil {
			logs.Errorf("创建会话失败: %v", err)
		} else {
			sessionInfo, _ := json.Marshal(map[string]any{
				"action":    "session_created",
				"sessionId": session.ID,
				"title":     session.Title,
			})
			s.sendData(ctx, dataChan, string(sessionInfo))
		}
	}

	var history []*schema.Message
	messages, err := s.repo.getSessionMessages(ctx, session.ID)
	if err != nil {
		logs.Errorf("查询会话历史消息失败: %v", err)
	} else {
		for _, v := range messages {
			switch v.Role {
			case string(schema.User):
				history = append(history, schema.UserMessage(v.Content))
			case string(schema.Assistant):
				history = append(history, schema.AssistantMessage(v.Content, nil))
			case string(schema.System):
				history = append(history, schema.SystemMessage(v.Content))
			}
		}
	}
	go s.saveChatMessage(session.ID, req.Message, schema.User)
	err = executor(ctx, session, dataChan, errChan)
	if err != nil {
		logs.Errorf("执行任务失败: %v", err)
		s.sendError(ctx, errChan, err)
		return
	}
}

func (s *service) handleDeepAgentMessage(ctx context.Context, agent *model.Agent, msg adk.Message, dataChan chan string) {
	if msg.Content == "" {
		return
	}
	s.sendData(ctx, dataChan, ai.BuildMessage(agent.Name, msg.ToolName, msg.Content))
}

func newService() *service {
	factory := deepagent.NewFactory()
	mgr, _ := agentmem.NewMemoryManager()
	mgr.Start() // 启动抽取器池 worker
	return &service{
		repo:             newModels(database.GetPostgresDB().GormDB),
		checkPointStore:  store.NewInMemoryStore(),
		pendingAnswer:    make(map[string]string),
		waitingStates:    make(map[string]bool),
		deepAgentFactory: factory,
		memoryMgr:        mgr,
	}
}
