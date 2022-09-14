package cf

import (
	"context"
	"encoding/hex"
	"os"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"github.com/zeebo/blake3"
)

type WorkerScript struct {
	registry.ResourceBase

	ZoneID  fields.StringInputField `state:"force_new"`
	Name    fields.StringInputField `state:"force_new"`
	Hash    fields.StringInputField
	EnvVars fields.MapInputField

	Path string `state:"-"`
}

func (o *WorkerScript) ReferenceID() string {
	return fields.GenerateID("zones/%s/workers/scripts/%s", o.ZoneID, o.Name)
}

func (o *WorkerScript) GetName() string {
	return fields.VerboseString(o.Name)
}

func (o *WorkerScript) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	workerRes, _ := cli.DownloadWorker(ctx, &cloudflare.WorkerRequestParams{
		ZoneID:     o.ZoneID.Any(),
		ScriptName: o.Name.Any(),
	})
	if workerRes.WorkerScript.Script == "" {
		o.MarkAsNew()

		return nil
	}

	o.MarkAsExisting()

	sum := blake3.Sum256([]byte(workerRes.WorkerScript.Script))
	o.Hash.SetCurrent(hex.EncodeToString(sum[:])[:32])

	bindings, err := cli.ListWorkerBindings(ctx, &cloudflare.WorkerRequestParams{
		ZoneID:     o.ZoneID.Any(),
		ScriptName: o.Name.Any(),
	})
	if err != nil {
		return err
	}

	envVars := make(map[string]interface{})

	for _, b := range bindings.BindingList {
		if b.Binding.Type() != cloudflare.WorkerSecretTextBindingType {
			continue
		}

		envVars[b.Name] = b.Binding.(cloudflare.WorkerSecretTextBinding).Text
	}

	o.EnvVars.SetCurrent(envVars)

	return nil
}

func (o *WorkerScript) createOrUpdateWorkerScript(ctx context.Context, cli *cloudflare.API) error {
	scriptContent, err := os.ReadFile(o.Path)
	if err != nil {
		return err
	}

	bindings := make(map[string]cloudflare.WorkerBinding)
	for k, v := range o.EnvVars.Wanted() {
		bindings[k] = cloudflare.WorkerSecretTextBinding{
			Text: v.(string),
		}
	}

	_, err = cli.UploadWorkerWithBindings(ctx, &cloudflare.WorkerRequestParams{
		ScriptName: o.Name.Wanted(),
	}, &cloudflare.WorkerScriptParams{
		Script:   string(scriptContent),
		Bindings: bindings,
	})

	return err
}

func (o *WorkerScript) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	return o.createOrUpdateWorkerScript(ctx, cli)
}

func (o *WorkerScript) Update(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	return o.createOrUpdateWorkerScript(ctx, cli)
}

func (o *WorkerScript) Delete(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	_, err := cli.DeleteWorker(ctx, &cloudflare.WorkerRequestParams{
		ZoneID:     o.ZoneID.Current(),
		ScriptName: o.Name.Current(),
	})

	return err
}
