package a2a

import (
	"common/biz"
	"context"
	"model"

	"basemodel/ai/a2a"
	"basemodel/database"
	"basemodel/errs"
	"basemodel/logs"
	models2 "github.com/cloudwego/eino-ext/a2a/models"
	"github.com/google/uuid"
)

type service struct {
	repo repository
}

func (s *service) getAgentCard(r GetAgentCardReq) (*models2.AgentCard, error) {
	ctx := context.TODO()
	card, err := a2a.GetAgentCard(ctx, r.AgentUrl, r.HandlerPath)
	if err != nil {
		logs.Errorf("get agent card err:%v", err)
		return nil, biz.ErrAgentCardGetFailed
	}
	return card, nil
}

func (s *service) saveAgentCard(r GetAgentCardReq) (any, error) {
	market, err := s.repo.getAgentCardByUrl(r.AgentUrl)
	if err != nil {
		logs.Errorf("get agent card by url err:%v", err)
		return nil, biz.ErrAgentCardGetFailed
	}
	if market != nil {
		return nil, biz.ErrAgentCardExisted
	}

	card, err := a2a.GetAgentCard(context.Background(), r.AgentUrl, r.HandlerPath)
	if err != nil {
		logs.Errorf("get agent card err:%v", err)
		return nil, biz.ErrAgentCardGetFailed
	}
	agentMarket := &model.AgentMarket{
		Id:          uuid.New(),
		URL:         r.AgentUrl,
		Name:        card.Name,
		Description: card.Description,
		HandlerPath: r.HandlerPath,
	}
	err = s.repo.save(agentMarket)
	if err != nil {
		logs.Errorf("save agent card err:%v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) listAgentMarkets() ([]*model.AgentMarket, error) {
	list, err := s.repo.list()
	if err != nil {
		logs.Errorf("list agent markets err:%v", err)
		return nil, errs.DBError
	}
	return list, nil
}

func (s *service) delete(id uuid.UUID) error {
	err := s.repo.delete(id)
	if err != nil {
		logs.Errorf("delete agent card err:%v", err)
		return errs.DBError
	}
	return nil
}

func newService() *service {
	return &service{
		repo: newModels(database.GetPostgresDB().GormDB),
	}
}
