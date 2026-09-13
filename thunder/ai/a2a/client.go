package a2a

import (
	"context"

	"github.com/cloudwego/eino-ext/a2a/client"
	"github.com/cloudwego/eino-ext/a2a/models"
	"github.com/cloudwego/eino-ext/a2a/transport/jsonrpc"
)

func GetAgentCard(ctx context.Context, url string, path string) (*models.AgentCard, error) {
	t, err := jsonrpc.NewTransport(ctx, &jsonrpc.ClientConfig{
		BaseURL:     url,
		HandlerPath: path,
	})
	if err != nil {
		return nil, err
	}

	c, err := client.NewA2AClient(ctx, &client.Config{
		Transport: t,
	})
	if err != nil {
		return nil, err
	}

	agentCard, err := c.AgentCard(ctx)
	return agentCard, err
}
