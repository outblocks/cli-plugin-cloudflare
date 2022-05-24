package plugin

import (
	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/cf"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	plugin "github.com/outblocks/outblocks-plugin-go"
	"github.com/outblocks/outblocks-plugin-go/env"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/log"
)

type Plugin struct {
	log     log.Logger
	env     env.Enver
	hostCli apiv1.HostServiceClient

	cli         *cloudflare.API
	settings    config.Settings
	zoneMap     map[string]string
	originCerts map[*cf.OriginCertificate]*apiv1.DomainInfo
}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) PluginContext() *config.PluginContext {
	return config.NewPluginContext(p.env, p.cli, &p.settings)
}

var (
	_ plugin.DNSPluginHandler    = (*Plugin)(nil)
	_ plugin.DeployPluginHandler = (*Plugin)(nil)
)
