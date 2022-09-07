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
	wranglerCli *config.WranglerCloudflareAPI

	settings         config.Settings
	zoneMap          map[string]string
	originCerts      map[*cf.OriginCertificate]*apiv1.DomainInfo
	nonOriginDomains []*apiv1.DomainInfo

	staticApps    map[string]*StaticApp
	functionApps  map[string]*FunctionApp
	pluginContext *config.PluginContext
}

func NewPlugin() *Plugin {
	return &Plugin{
		originCerts:  make(map[*cf.OriginCertificate]*apiv1.DomainInfo),
		zoneMap:      map[string]string{},
		staticApps:   make(map[string]*StaticApp),
		functionApps: make(map[string]*FunctionApp),
	}
}

func (p *Plugin) PluginContext() *config.PluginContext {
	if p.pluginContext == nil {
		p.pluginContext = config.NewPluginContext(p.env, p.cli, p.wranglerCli, &p.settings)
	}

	return p.pluginContext
}

var (
	_ plugin.DNSPluginHandler    = (*Plugin)(nil)
	_ plugin.DeployPluginHandler = (*Plugin)(nil)
	_ plugin.LogsPluginHandler   = (*Plugin)(nil)
)
