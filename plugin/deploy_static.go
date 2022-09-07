package plugin

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/outblocks/cli-plugin-cloudflare/cf"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"github.com/outblocks/outblocks-plugin-go/types"

	plugin_util "github.com/outblocks/outblocks-plugin-go/util"
)

type StaticApp struct {
	App        *apiv1.App
	Props      *types.StaticAppProperties
	DeployOpts *types.StaticAppDeployOptions

	domains []string

	PagesProject    *cf.PagesProject
	PagesFiles      *cf.PagesFiles
	PagesDeployment *cf.PagesDeployment
}

func NewStaticApp(plan *apiv1.AppPlan, domains []string) (*StaticApp, error) {
	opts, err := types.NewStaticAppProperties(plan.State.App.Properties.AsMap())
	if err != nil {
		return nil, err
	}

	deployOpts, err := types.NewStaticAppDeployOptions(plan.State.App.Properties.AsMap())
	if err != nil {
		return nil, err
	}

	return &StaticApp{
		App:        plan.State.App,
		Props:      opts,
		DeployOpts: deployOpts,
		domains:    domains,
	}, nil
}

func (o *StaticApp) process(ctx context.Context, pctx *config.PluginContext, r *registry.Registry) error {
	cli := pctx.CloudflareClient()
	pagesProject := cf.ID(pctx.Env(), o.App.Id)

	domains := make([]fields.Field, len(o.domains))

	for i, d := range o.domains {
		domains[i] = fields.String(d)
	}

	o.PagesProject = &cf.PagesProject{
		Name:      fields.String(pagesProject),
		AccountID: fields.String(cli.AccountID),
		Domains:   fields.Array(domains),
	}

	_, err := r.RegisterAppResource(o.App, "pages_project", o.PagesProject)
	if err != nil {
		return err
	}

	buildDir := filepath.Join(pctx.Env().ProjectDir(), o.App.Dir, o.Props.Build.Dir)

	buildPath, ok := plugin_util.CheckDir(buildDir)
	if !ok {
		return fmt.Errorf("%s app '%s' build dir '%s' does not exist", o.App.Type, o.App.Name, buildDir)
	}

	hashesInterface, err := pctx.FuncCache(fmt.Sprintf("PagesFiles:%s:hash", pagesProject), func() (interface{}, error) {
		return cf.PagesFindFiles(buildPath, o.DeployOpts.Patterns)
	})
	if err != nil {
		return err
	}

	hashes := hashesInterface.(map[string]*cf.PagesFileInfo)
	if len(hashes) > cf.CloudflarePagesMaxFileCount {
		return fmt.Errorf("CloudFlare Pages only supports files up %d files, ensure you have specified build output directory correctly", cf.CloudflarePagesMaxSize)
	}

	hashesList := make([]string, 0, len(hashes))
	for _, h := range hashes {
		hashesList = append(hashesList, h.Hash)
	}

	o.PagesFiles = &cf.PagesFiles{
		Name:        pagesProject,
		ProjectName: o.PagesProject.Name,
		Hashes:      hashes,
		HashesList:  hashesList,
	}

	_, err = r.RegisterAppResource(o.App, "pages_files", o.PagesFiles)
	if err != nil {
		return err
	}

	o.PagesDeployment = &cf.PagesDeployment{
		Name:        pagesProject,
		ProjectName: o.PagesProject.Name,
		AccountID:   o.PagesProject.AccountID,
		Manifest:    o.PagesFiles.Manifest.Input(),
	}

	_, err = r.RegisterAppResource(o.App, "pages_deployment", o.PagesDeployment)
	if err != nil {
		return err
	}

	return nil
}

func (o *StaticApp) DNSRecord() *apiv1.DNSRecord {
	if o.App.Url == "" {
		return nil
	}

	return &apiv1.DNSRecord{
		Record: getHostname(o.App.Url),
		Type:   apiv1.DNSRecord_TYPE_CNAME,
		Value:  o.PagesProject.InternalDomain.Current(),
	}
}

func (o *StaticApp) AppState() *apiv1.AppState {
	cloudURL := ""

	if o.PagesProject != nil {
		cloudURL = fmt.Sprintf("https://%s", o.PagesProject.InternalDomain.Current())
	}

	return &apiv1.AppState{
		App: o.App,
		Deployment: &apiv1.DeploymentState{
			Ready: true,
		},
		Dns: &apiv1.DNSState{
			Url:      o.App.Url,
			CloudUrl: cloudURL,
		},
	}
}
