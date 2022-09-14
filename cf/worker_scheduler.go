package cf

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type WorkerSchedulers struct {
	registry.ResourceBase

	AccountID  fields.StringInputField `state:"force_new"`
	ScriptName fields.StringInputField `state:"force_new"`
	Crons      fields.ArrayInputField
}

func (o *WorkerSchedulers) ReferenceID() string {
	return fields.GenerateID("accounts/%s/workers/schedulers/%s", o.AccountID, o.ScriptName)
}

func (o *WorkerSchedulers) GetName() string {
	return fmt.Sprintf("%d cron(s)", len(o.Crons.Wanted()))
}

func (o *WorkerSchedulers) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	scriptName := o.ScriptName.Wanted()
	if scriptName == "" {
		o.MarkAsNew()

		return nil
	}

	crons, err := cli.ListWorkerCronTriggers(ctx, o.AccountID.Any(), o.ScriptName.Any())
	if err != nil {
		return err
	}

	if len(crons) == 0 {
		o.MarkAsNew()

		return nil
	}

	croni := make([]interface{}, len(crons))
	for i, c := range crons {
		croni[i] = c.Cron
	}

	return nil
}

func (o *WorkerSchedulers) updateOrCreate(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()
	crons := o.Crons.Wanted()
	cronobj := make([]cloudflare.WorkerCronTrigger, len(crons))

	for i, cron := range crons {
		cronobj[i].Cron = cron.(string)
	}

	_, err := cli.UpdateWorkerCronTriggers(ctx, o.AccountID.Wanted(), o.ScriptName.Wanted(), cronobj)

	return err
}

func (o *WorkerSchedulers) Create(ctx context.Context, meta interface{}) error {
	return o.updateOrCreate(ctx, meta)
}

func (o *WorkerSchedulers) Update(ctx context.Context, meta interface{}) error {
	return o.updateOrCreate(ctx, meta)
}

func (o *WorkerSchedulers) Delete(ctx context.Context, meta interface{}) error {
	return o.updateOrCreate(ctx, meta)
}
