package tools

import (
	"common/biz"
	"context"
	"core/ai/mcps"
	"core/ai/tools"
	"encoding/json"
	"model"
	"time"

	"basemodel/ai/einos"
	"basemodel/database"
	"basemodel/errs"
	"basemodel/logs"
	"basemodel/res"
	"github.com/google/uuid"
)

type service struct {
	repo repository
}

func (s *service) createTool(ctx context.Context, userId uuid.UUID, req CreateToolReq) (*model.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	toolInfo, err := s.repo.getToolByName(ctx, req.Name)
	if err != nil {
		logs.Errorf("get tool by name error: %v", err)
		return nil, errs.DBError
	}
	if toolInfo != nil {
		return nil, biz.ErrToolNameExisted
	}

	tool := model.Tool{
		BaseModel: model.BaseModel{
			ID: uuid.New(),
		},
		ToolType:  req.ToolType,
		IsEnable:  true,
		CreatorID: userId,
	}

	if req.ToolType == model.McpToolType {
		if req.McpConfig != nil {
			tool.McpConfig = req.McpConfig
		}
		tool.Name = req.Name
		tool.Description = req.Description
	} else {
		invokeParamTool := tools.FindTool(req.Name)
		if invokeParamTool == nil {
			return nil, biz.ErrToolNotExisted
		}
		info, err := invokeParamTool.Info(ctx)
		if err != nil {
			logs.Errorf("get tool info error: %v", err)
			return nil, errs.DBError
		}
		tool.Name = info.Name
		tool.Description = info.Desc
		tool.ParametersSchema = invokeParamTool.Params()
	}
	err = s.repo.createTool(ctx, &tool)
	if err != nil {
		logs.Errorf("create tool error: %v", err)
		return nil, errs.DBError
	}
	return &tool, nil
}

func (s *service) listTools(ctx context.Context, userID uuid.UUID, req ListToolsReq) (*res.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter := toolFilter{
		Name:     req.Name,
		Limit:    req.PageSize,
		Offset:   (req.Page - 1) * req.PageSize,
		ToolType: req.Type,
	}
	toolList, total, err := s.repo.listTools(ctx, userID, filter)
	if err != nil {
		logs.Errorf("list tools error: %v", err)
		return nil, errs.DBError
	}

	return &res.Page{
		List:        toolList,
		Total:       total,
		CurrentPage: int64(req.Page),
		PageSize:    int64(req.PageSize),
	}, nil
}

func (s *service) updateTool(ctx context.Context, userID uuid.UUID, id uuid.UUID, req UpdateToolReq) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	toolInfo, err := s.repo.getTool(ctx, userID, id)
	if err != nil {
		logs.Errorf("get tool error: %v", err)
		return nil, errs.DBError
	}
	if toolInfo == nil {
		return nil, biz.ErrToolNotExisted
	}

	if req.Name != toolInfo.Name {
		info, err := s.repo.getToolByName(ctx, req.Name)
		if err != nil {
			logs.Errorf("get tool by name error: %v", err)
			return nil, errs.DBError
		}
		if info != nil {
			return nil, biz.ErrToolNameExisted
		}
	}
	toolInfo.Name = req.Name
	toolInfo.Description = req.Description
	err = s.repo.updateTool(ctx, toolInfo)
	if err != nil {
		logs.Errorf("update tool error: %v", err)
		return nil, errs.DBError
	}
	return toolInfo, nil
}

func (s *service) deleteTool(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.repo.deleteTool(ctx, userID, id)
	if err != nil {
		logs.Errorf("delete tool error: %v", err)
		return errs.DBError
	}
	return nil
}

func (s *service) testTool(ctx context.Context, userId uuid.UUID, id uuid.UUID, req TestToolReq) (*TestToolResponse, error) {
	toolInfo, err := s.repo.getTool(ctx, userId, id)
	if err != nil {
		logs.Errorf("get tool error: %v", err)
		return nil, errs.DBError
	}

	invokeParamTool := tools.FindTool(toolInfo.Name)
	if invokeParamTool == nil {
		return nil, biz.ErrToolNotExisted
	}

	params, _ := json.Marshal(req.Params)
	result, err := invokeParamTool.InvokableRun(ctx, string(params))
	if err != nil {
		logs.Errorf("invoke tool error: %v", err)
		return &TestToolResponse{
			Message: err.Error(),
			Success: false,
			Data:    nil,
		}, nil
	}
	return &TestToolResponse{
		Message: "success",
		Success: true,
		Data:    result,
	}, nil
}

func (s *service) getMcpTools(ctx context.Context, userId uuid.UUID, toolId uuid.UUID) ([]*model.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	tool, err := s.repo.getTool(ctx, userId, toolId)
	if err != nil {
		logs.Errorf("get tool error: %v", err)
		return nil, errs.DBError
	}
	config := tool.McpConfig
	if config == nil {
		return nil, biz.ErrMcpConfigNotExisted
	}

	mcpConfig := einos.McpConfig{
		BaseUrl: config.Url,
		Token:   config.CredentialType,
		Name:    "aiDemo",
		Version: "1.0.0",
	}
	mcpTools, err := mcps.GetMCPTool(ctx, &mcpConfig)
	if err != nil {
		logs.Errorf("get mcp tool error: %v", err)
		return nil, biz.ErrGetMcpTools
	}

	var toolList []*model.Tool
	for _, mcpTool := range mcpTools {
		toolList = append(toolList, &model.Tool{
			BaseModel: model.BaseModel{
				ID: uuid.New(),
			},
			Name:             mcpTool.Name,
			Description:      mcpTool.Description,
			ToolType:         model.McpToolType,
			CreatorID:        userId,
			ParametersSchema: einos.ConvertSchema(mcpTool.InputSchema),
		})
	}
	return toolList, nil
}

func newService() *service {
	return &service{
		repo: newModels(database.GetPostgresDB().GormDB),
	}
}
