package agents

import (
	"context"
	"net/http"
	"time"

	"basemodel/logs"
	"basemodel/req"
	"basemodel/res"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service *service
}

func (h *Handler) CreateAgent(c *gin.Context) {
	var createReq CreateAgentReq
	if err := req.JsonParam(c, &createReq); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}

	resp, err := h.service.createAgent(c.Request.Context(), userID, createReq)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) ListAgents(c *gin.Context) {
	var listReq SearchAgentReq
	if err := req.JsonParam(c, &listReq); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.listAgents(c.Request.Context(), userID, listReq)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) GetAgent(c *gin.Context) {
	var id uuid.UUID
	if err := req.Path(c, "id", &id); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.getAgent(c.Request.Context(), userID, id)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) UpdateAgent(c *gin.Context) {
	var updateReq UpdateAgentReq
	if err := req.JsonParam(c, &updateReq); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.updateAgent(c.Request.Context(), userID, updateReq)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) AgentMessage(c *gin.Context) {
	var messageReq AgentMessageReq
	if err := req.JsonParam(c, &messageReq); err != nil {
		return
	}
	userID, exist := req.GetUserIdUUID(c)
	if !exist {
		return
	}

	// AI回答时间较长，所以不能设置限制
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		logs.Warnf("SetWriteDeadline error: %v", err)
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	datachan, errchan := h.service.agentMessage(ctx, userID, messageReq)
	heartbeat := time.NewTicker(time.Second * 5)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			logs.Warnf("context done, 客户端断开连接")
			return
		case <-heartbeat.C:
			_, err := c.Writer.Write([]byte(": keep-alive\n\n"))
			if err != nil {
				logs.Warnf("write heartbeat error: %v", err)
				cancel()
				return
			}
			c.Writer.Flush()
		case data, ok := <-datachan:
			if !ok {
				_, err := c.Writer.Write([]byte("data: [DONE]\n\n"))
				if err != nil {
					logs.Warnf("write done error: %v", err)
				}
				c.Writer.Flush()
				return
			}
			_, err := c.Writer.Write([]byte("data: " + data + "\n\n"))
			if err != nil {
				logs.Errorf("write data error: %v", err)
				cancel()
				return
			}
			c.Writer.Flush()
		case err, ok := <-errchan:
			if !ok {
				errchan = nil
				continue
			}
			if err != nil {
				_, err := c.Writer.Write([]byte("data: [ERROR]" + err.Error() + "\n\n"))
				if err != nil {
					logs.Errorf("write error error: %v", err)
					cancel()
					return
				}
				c.Writer.Flush()
				return
			}
		}
	}
}

func (h *Handler) UpdateAgentTool(c *gin.Context) {
	var id uuid.UUID
	if err := req.Path(c, "id", &id); err != nil {
		return
	}

	var updateReq UpdateAgentToolReq
	if err := req.JsonParam(c, &updateReq); err != nil {
		return
	}

	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.updateAgentTool(c.Request.Context(), userID, id, updateReq)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) AddAgentKnowledgeBase(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var addReq addAgentKnowledgeBaseReq
	if err := req.JsonParam(c, &addReq); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.addAgentKnowledgeBase(c.Request.Context(), userID, agentId, addReq)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) DeleteAgentKnowledgeBase(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var kbId uuid.UUID
	if err := req.Path(c, "kbId", &kbId); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.deleteAgentKnowledgeBase(c.Request.Context(), userID, agentId, kbId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) AddAgentAgent(c *gin.Context) {
	var agentRequest AgentMarketRequest
	if err := req.JsonParam(c, &agentRequest); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.addAgentAgent(c.Request.Context(), userID, agentRequest)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) DeleteAgentAgent(c *gin.Context) {
	var agentRequest DeleteAgentMarketRequest
	if err := req.JsonParam(c, &agentRequest); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.deleteAgentAgent(c.Request.Context(), userID, agentRequest)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) AddWorkflowToAgent(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var reqs addWorkflowToAgentReq
	if err := req.JsonParam(c, &reqs); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.addWorkflowToAgent(c.Request.Context(), userID, agentId, reqs)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) DeleteWorkflowFromAgent(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var workflowId uuid.UUID
	if err := req.Path(c, "workflowId", &workflowId); err != nil {
		return
	}
	err := h.service.deleteWorkflowFromAgent(c.Request.Context(), agentId, workflowId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, nil)
}

func (h *Handler) DeleteAgent(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	err := h.service.deleteAgent(c.Request.Context(), agentId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, nil)
}

func (h *Handler) CreateSession(c *gin.Context) {
	var param createSessionRequest
	if err := req.JsonParam(c, &param); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.createSession(c.Request.Context(), userID, param)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) ListSessions(c *gin.Context) {
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	var param listSessionsRequest
	err := req.QueryParam(c, &param)
	if err != nil {
		return
	}
	agentId, err := uuid.Parse(param.AgentID)
	if err != nil {
		res.Error(c, err)
		return
	}
	resp, err := h.service.listSessions(c.Request.Context(), userID, agentId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) GetSessionMessages(c *gin.Context) {
	var sessionId uuid.UUID
	if err := req.Path(c, "sessionId", &sessionId); err != nil {
		return
	}
	resp, err := h.service.getSessionMessages(c.Request.Context(), sessionId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) DeleteSession(c *gin.Context) {
	var sessionId uuid.UUID
	if err := req.Path(c, "sessionId", &sessionId); err != nil {
		return
	}
	err := h.service.deleteSession(c.Request.Context(), sessionId)
	if err != nil {
		res.Error(c, err)
		return
	}
	res.Success(c, nil)
}

func (h *Handler) AddSkillToAgent(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var reqs AddAgentSkillReq
	if err := req.JsonParam(c, &reqs); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	resp, err := h.service.addSkillToAgent(c.Request.Context(), userID, agentId, reqs)
	if err != nil {
		logs.Errorf("add skill to agent error: %v", err)
		res.Error(c, err)
		return
	}
	res.Success(c, resp)
}

func (h *Handler) DeleteSkillFromAgent(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var skillId uuid.UUID
	if err := req.Path(c, "skillId", &skillId); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	err := h.service.deleteSkillFromAgent(c.Request.Context(), userID, agentId, skillId)
	if err != nil {
		logs.Errorf("delete skill from agent error: %v", err)
		res.Error(c, err)
		return
	}
	res.Success(c, nil)
}

func (h *Handler) DeleteAgentTool(c *gin.Context) {
	var agentId uuid.UUID
	if err := req.Path(c, "id", &agentId); err != nil {
		return
	}
	var toolId uuid.UUID
	if err := req.Path(c, "toolId", &toolId); err != nil {
		return
	}
	userID, ok := req.GetUserIdUUID(c)
	if !ok {
		return
	}
	err := h.service.deleteAgentTool(c.Request.Context(), userID, agentId, toolId)
	if err != nil {
		logs.Errorf("delete tool from agent error: %v", err)
		res.Error(c, err)
		return
	}
	res.Success(c, nil)
}

func NewHandler() *Handler {
	return &Handler{
		service: newService(),
	}
}
