package cf

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type WorkerRoute struct {
	registry.ResourceBase

	ZoneID     fields.StringInputField `state:"force_new"`
	ScriptName fields.StringInputField `state:"force_new"`
	Pattern    fields.StringInputField

	ID fields.StringOutputField
}

func (o *WorkerRoute) ReferenceID() string {
	return fields.GenerateID("zones/%s/workers/routes/%s/%s", o.ZoneID, o.ScriptName, o.Pattern)
}

func (o *WorkerRoute) GetName() string {
	return fields.VerboseString(o.Pattern)
}

func (o *WorkerRoute) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	zoneID := o.ZoneID.Any()

	routes, err := pctx.FuncCache(fmt.Sprintf("WorkerRoutes:list:%s", zoneID), func() (interface{}, error) {
		return cli.ListWorkerRoutes(ctx, zoneID)
	})
	if err != nil {
		return fmt.Errorf("error fetching worker routes: %w", err)
	}

	var route *cloudflare.WorkerRoute

	for _, r := range routes.(cloudflare.WorkerRoutesResponse).Routes {
		if o.ID.Current() == r.ID {
			route = &r //nolint
			break
		}
	}

	if route == nil {
		for _, r := range routes.(cloudflare.WorkerRoutesResponse).Routes {
			if o.Pattern.Any() == r.Pattern {
				route = &r //nolint
				break
			}
		}
	}

	if route == nil {
		o.MarkAsNew()

		return nil
	}

	o.MarkAsExisting()
	o.ID.SetCurrent(route.ID)
	o.Pattern.SetCurrent(route.Pattern)
	o.ScriptName.SetCurrent(route.Script)

	return nil
}

func (o *WorkerRoute) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	res, err := cli.CreateWorkerRoute(ctx, o.ZoneID.Wanted(), cloudflare.WorkerRoute{
		Pattern: o.Pattern.Wanted(),
		Script:  o.ScriptName.Wanted(),
	})
	if err != nil {
		return err
	}

	o.ID.SetCurrent(res.ID)

	return nil
}

func (o *WorkerRoute) Update(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	_, err := cli.UpdateWorkerRoute(ctx, o.ZoneID.Wanted(), o.ID.Current(), cloudflare.WorkerRoute{
		Pattern: o.Pattern.Wanted(),
		Script:  o.ScriptName.Wanted(),
	})

	return err
}

func (o *WorkerRoute) Delete(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	_, err := cli.DeleteWorkerRoute(ctx, o.ZoneID.Wanted(), o.ID.Current())

	return err
}
