package cf

import (
	"context"
	"fmt"

	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type PagesDeployment struct {
	registry.ResourceBase

	Name        string                  `state:"-"`
	ProjectName fields.StringInputField `state:"force_new"`
	AccountID   fields.StringInputField `state:"force_new"`
	Manifest    fields.MapInputField    `state:"force_new"`
}

func (o *PagesDeployment) ReferenceID() string {
	return fields.GenerateID("accounts/%s/pages/%s/deployment", o.AccountID, o.ProjectName)
}

func (o *PagesDeployment) GetName() string {
	return o.Name
}

func (o *PagesDeployment) Read(ctx context.Context, meta interface{}) error {
	return nil
}

func (o *PagesDeployment) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	wranglerCli := pctx.WranglerCloudflareClient()

	manifest := o.Manifest.Wanted()
	manifestStr := make(map[string]string, len(manifest))

	for k, v := range manifest {
		manifestStr[k] = v.(string)
	}

	err := wranglerCli.CreatePagesDeployment(ctx, o.ProjectName.Wanted(), manifestStr)

	return err
}

func (o *PagesDeployment) Update(ctx context.Context, meta interface{}) error {
	return fmt.Errorf("unimplemented")
}

func (o *PagesDeployment) Delete(ctx context.Context, meta interface{}) error {
	return nil
}
