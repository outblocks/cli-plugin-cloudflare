package plugin

import (
	"context"

	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/env"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/log"
)

func (p *Plugin) Init(ctx context.Context, e env.Enver, l log.Logger, cli apiv1.HostServiceClient) error {
	p.env = e
	p.hostCli = cli
	p.log = l

	return nil
}

func (p *Plugin) ProjectInit(ctx context.Context, r *apiv1.ProjectInitRequest) (*apiv1.ProjectInitResponse, error) {
	return &apiv1.ProjectInitResponse{}, nil
}

func (p *Plugin) Start(ctx context.Context, r *apiv1.StartRequest) (*apiv1.StartResponse, error) {
	var err error

	apiKey, err := p.hostCli.HostGetSecret(ctx, &apiv1.HostGetSecretRequest{
		Key: "cloudflare_api_key",
	})
	if err != nil {
		return nil, err
	}

	apiEmail, err := p.hostCli.HostGetSecret(ctx, &apiv1.HostGetSecretRequest{
		Key: "cloudflare_api_email",
	})
	if err != nil {
		return nil, err
	}

	apiToken, err := p.hostCli.HostGetSecret(ctx, &apiv1.HostGetSecretRequest{
		Key: "cloudflare_api_token",
	})
	if err != nil {
		return nil, err
	}

	apiUserServiceKey, err := p.hostCli.HostGetSecret(ctx, &apiv1.HostGetSecretRequest{
		Key: "cloudflare_api_user_service_key",
	})
	if err != nil {
		return nil, err
	}

	// Init cloudflare API.
	p.cli, err = config.NewCloudflareClient(apiKey.Value, apiEmail.Value, apiToken.Value, apiUserServiceKey.Value)
	if err != nil {
		return nil, err
	}

	return &apiv1.StartResponse{}, nil
}
