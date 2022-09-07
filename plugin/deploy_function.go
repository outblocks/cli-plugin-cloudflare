package plugin

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/outblocks/cli-plugin-cloudflare/cf"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"github.com/outblocks/outblocks-plugin-go/types"
	plugin_util "github.com/outblocks/outblocks-plugin-go/util"
	"github.com/zeebo/blake3"
)

type FunctionApp struct {
	App        *apiv1.App
	Props      *types.FunctionAppProperties
	DeployOpts *types.FunctionAppDeployOptions
	ZoneID     string

	WorkerRoute  *cf.WorkerRoute
	WorkerScript *cf.WorkerScript
}

func NewFunctionApp(plan *apiv1.AppPlan, zoneID string) (*FunctionApp, error) {
	opts, err := types.NewFunctionAppProperties(plan.State.App.Properties.AsMap())
	if err != nil {
		return nil, err
	}

	deployOpts, err := types.NewFunctionAppDeployOptions(plan.State.App.Properties.AsMap())
	if err != nil {
		return nil, err
	}

	return &FunctionApp{
		App:        plan.State.App,
		Props:      opts,
		DeployOpts: deployOpts,
		ZoneID:     zoneID,
	}, nil
}

func (o *FunctionApp) process(ctx context.Context, pctx *config.PluginContext, r *registry.Registry, vars map[string]interface{}) error {
	buildDir := filepath.Join(pctx.Env().ProjectDir(), o.App.Dir, o.Props.Build.Dir)
	scriptName := cf.ID(pctx.Env(), o.App.Id)

	buildPath, ok := plugin_util.CheckDir(buildDir)
	if !ok {
		return fmt.Errorf("%s app '%s' build dir '%s' does not exist", o.App.Type, o.App.Name, buildDir)
	}

	scriptFile := filepath.Join(buildPath, "index.js")
	if !plugin_util.FileExists(scriptFile) {
		return fmt.Errorf("%s app '%s' is missing index.js file in '%s'", o.App.Type, o.App.Name, buildPath)
	}

	bytes, err := os.ReadFile(scriptFile)
	if err != nil {
		return err
	}

	sum := blake3.Sum256(bytes)
	hash := hex.EncodeToString(sum[:])[:32]

	envVars := make(map[string]fields.Field)
	eval := fields.NewFieldVarEvaluator(vars)

	for k, v := range o.App.Env {
		exp, err := eval.Expand(v)
		if err != nil {
			return err
		}

		envVars[k] = exp
	}

	o.WorkerScript = &cf.WorkerScript{
		ZoneID:  fields.String(o.ZoneID),
		Name:    fields.String(scriptName),
		Hash:    fields.String(hash),
		EnvVars: fields.Map(envVars),

		Path: scriptFile,
	}

	_, err = r.RegisterAppResource(o.App, "worker_script", o.WorkerScript)
	if err != nil {
		return err
	}

	if o.App.Url != "" {
		o.WorkerRoute = &cf.WorkerRoute{
			ZoneID:     fields.String(o.ZoneID),
			ScriptName: o.WorkerScript.Name,
			Pattern:    fields.String(cf.FixURL(o.App.Url)),
		}

		_, err = r.RegisterAppResource(o.App, "worker_route", o.WorkerRoute)
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *FunctionApp) DNSRecord() *apiv1.DNSRecord {
	if o.WorkerRoute == nil {
		return nil
	}

	return &apiv1.DNSRecord{
		Record: getHostname(o.App.Url),
		Type:   apiv1.DNSRecord_TYPE_AAAA,
		Value:  "100::",
	}
}

func (o *FunctionApp) AppState() *apiv1.AppState {
	return &apiv1.AppState{
		App: o.App,
		Deployment: &apiv1.DeploymentState{
			Ready: true,
		},
		Dns: &apiv1.DNSState{
			Url: o.App.Url,
		},
	}
}
