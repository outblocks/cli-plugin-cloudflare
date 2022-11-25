package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/outblocks/cli-plugin-cloudflare/cf"
	plugin_go "github.com/outblocks/outblocks-plugin-go"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"github.com/outblocks/outblocks-plugin-go/types"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	certPendingValue = "pending"

	AppTypeStatic   = "static"
	AppTypeFunction = "function"
)

func (p *Plugin) computeZoneMap(state *apiv1.PluginState, domains []*apiv1.DomainInfo) error {
	var err error

	if state.Other == nil {
		state.Other = make(map[string][]byte)
	}

	_ = json.Unmarshal(state.Other["zone_id_map"], &p.zoneMap)

	for _, domainInfo := range domains {
		for _, d := range domainInfo.Domains {
			zone := getDomainZoneName(d)
			if zone == "" {
				continue
			}

			id := p.zoneMap[zone]

			if id == "" {
				id, err = p.cli.ZoneIDByName(zone)
				if err != nil {
					return fmt.Errorf("cannot lookup zone '%s': %w", zone, err)
				}
			}

			p.zoneMap[zone] = id
		}
	}

	data, _ := json.Marshal(p.zoneMap)
	state.Other["zone_id_map"] = data

	return nil
}

func (p *Plugin) isValidCloudflareDomain(domainInfo *apiv1.DomainInfo) bool {
	if len(domainInfo.Domains) == 0 {
		return false
	}

	zone := getDomainZoneName(domainInfo.Domains[0])

	for _, d := range domainInfo.Domains[1:] {
		if zone != getDomainZoneName(d) {
			return false
		}
	}

	return true
}

func (p *Plugin) validCloudflareDomains(domains []*apiv1.DomainInfo) []*apiv1.DomainInfo {
	var ret []*apiv1.DomainInfo

	for _, domainInfo := range domains {
		if domainInfo.DnsPlugin != "cloudflare" {
			continue
		}

		if !p.isValidCloudflareDomain(domainInfo) {
			continue
		}

		if domainInfo.Properties.AsMap()["cloudflare_proxy"] != true && domainInfo.Cert != "" && domainInfo.Key != "" {
			continue
		}

		ret = append(ret, domainInfo)
	}

	return ret
}

func containsNestedSubdomain(domain *apiv1.DomainInfo) bool {
	for _, rec := range domain.Domains {
		if strings.Count(rec, ".") > 2 {
			return true
		}
	}

	return false
}

func isStringArraySubset(arr []string, m map[string]struct{}) bool {
	for _, v := range arr {
		if _, ok := m[v]; !ok {
			return false
		}
	}

	return true
}

func (p *Plugin) registerOriginCertificates(reg *registry.Registry, appPlans []*apiv1.AppPlan, domains []*apiv1.DomainInfo, apply bool) error {
	appIDs := make(map[string]struct{})

	for _, a := range appPlans {
		appIDs[a.State.App.Id] = struct{}{}
	}

	dom := make([]*apiv1.DomainInfo, 0, len(domains))

	for _, d := range domains {
		if !isStringArraySubset(d.AppIds, appIDs) {
			dom = append(dom, d)
		}
	}

	if len(dom) == 0 {
		return nil
	}

	if !strings.HasPrefix(p.cli.APIUserServiceKey, "v1.0-") && !apply {
		p.log.Infoln("'CLOUDFLARE_API_USER_SERVICE_KEY' not set or invalid, skipping Origin CA creation...")

		return nil
	}

	for _, d := range dom {
		zone := getDomainZoneName(d.Domains[0])
		h := make([]fields.Field, len(d.Domains))

		if containsNestedSubdomain(d) {
			p.nonOriginDomains = append(p.nonOriginDomains, d)
			continue
		}

		for i, v := range d.Domains {
			h[i] = fields.String(v)
		}

		o := cf.OriginCertificate{
			Hostnames: fields.Array(h),
		}

		_, err := reg.RegisterPluginResource(zone, "origin_certificate", &o)
		if err != nil {
			return err
		}

		p.originCerts[&o] = d
		d.Cert = certPendingValue
		d.Key = certPendingValue

		if d.Properties.GetFields() == nil {
			d.Properties, _ = structpb.NewStruct(nil)
		}

		d.Properties.Fields["cloudflare_proxy"] = structpb.NewBoolValue(true)
		d.Properties.Fields["cloudflare_origin"] = structpb.NewBoolValue(true)
	}

	return nil
}

func (p *Plugin) processOriginCertificates(ctx context.Context, apply bool) error {
	for cert, domain := range p.originCerts {
		if !cert.IsExisting() {
			continue
		}

		domain.Cert = cert.Certificate.Current()
		domain.Key = cert.PrivateKey.Current()

		if !apply {
			continue
		}

		zone := getDomainZoneName(domain.Domains[0])
		zoneID := p.zoneMap[zone]
		settings, _ := p.cli.ZoneSSLSettings(ctx, zoneID)

		if settings.Value != "flexible" && settings.Value != "off" {
			continue
		}

		_, err := p.cli.UpdateZoneSSLSettings(ctx, zoneID, "full")
		if err != nil {
			return err
		}
	}

	for _, domain := range p.nonOriginDomains {
		if domain.Properties.AsMap()["cloudflare_origin"] != true {
			continue
		}

		delete(domain.Properties.GetFields(), "cloudflare_origin")

		domain.Cert = ""
		domain.Key = ""
	}

	return nil
}

func (p *Plugin) processApps(ctx context.Context, reg *registry.Registry, appPlans []*apiv1.AppPlan) error {
	if len(appPlans) == 0 {
		return nil
	}

	if p.cli.AccountID == "" {
		return fmt.Errorf("$CLOUDFLARE_ACCOUNT_ID or secrets.cloudflare_account_id is required for app deployment")
	}

	apps := make([]*apiv1.App, len(appPlans))
	for i, a := range appPlans {
		apps[i] = a.State.App
	}

	appVars := types.AppVarsFromApps(apps)

	for _, app := range appPlans {
		if app.Skip {
			continue
		}

		hostname := ""
		path := ""

		if app.State.App.Url != "" {
			u, _ := url.Parse(app.State.App.Url)
			hostname = u.Hostname()
			path = u.Path
		}

		switch {
		case app.State.App.Type == AppTypeStatic:
			a, err := NewStaticApp(app, []string{hostname})
			if err != nil {
				return err
			}

			if len(path) > 1 {
				return fmt.Errorf("cannot use url '%s' for cloudflare pages - url has to be a full domain without path", app.State.App.Url)
			}

			p.staticApps[app.State.App.Id] = a

			err = a.process(ctx, p.PluginContext(), reg)
			if err != nil {
				return err
			}

		case app.State.App.Type == AppTypeFunction:
			domain := getDomainZoneName(hostname)

			if strings.Count(hostname, ".") > 2 {
				return fmt.Errorf("cannot use domain '%s' for cloudflare worker deployment - current max subdomain level is 1", hostname)
			}

			zoneID := p.zoneMap[domain]
			if zoneID == "" {
				return fmt.Errorf("zone for domain could not be found: %s", domain)
			}

			a, err := NewFunctionApp(app, zoneID)
			if err != nil {
				return err
			}

			p.functionApps[app.State.App.Id] = a

			err = a.process(ctx, p.PluginContext(), reg, types.VarsForApp(appVars, app.State.App, nil))
			if err != nil {
				return err
			}
		}
	}

	return nil
}
func (p *Plugin) processDeployInit(ctx context.Context, reg *registry.Registry, appPlans []*apiv1.AppPlan, state *apiv1.PluginState, domains []*apiv1.DomainInfo, apply bool) ([]*registry.Diff, error) {
	pctx := p.PluginContext()
	reg = reg.Partition("init")

	err := prepareRegistry(reg, state.Registry)
	if err != nil {
		return nil, err
	}

	err = p.computeZoneMap(state, domains)
	if err != nil {
		return nil, err
	}

	domains = p.validCloudflareDomains(domains)

	err = p.registerOriginCertificates(reg, appPlans, domains, apply)
	if err != nil {
		return nil, err
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return nil, err
	}

	err = p.processOriginCertificates(ctx, false)

	return diff, err
}

func (p *Plugin) processDeploy(ctx context.Context, reg *registry.Registry, appPlans []*apiv1.AppPlan, state *apiv1.PluginState) (map[string]*apiv1.AppState, []*apiv1.DNSRecord, []*registry.Diff, error) {
	pctx := p.PluginContext()
	reg = reg.Partition("deploy")

	err := prepareRegistry(reg, state.Registry)
	if err != nil {
		return nil, nil, nil, err
	}

	err = p.processApps(ctx, reg, appPlans)
	if err != nil {
		return nil, nil, nil, err
	}

	appStates := make(map[string]*apiv1.AppState)

	// Process DNS records and appstates.
	recs := make(map[string]*apiv1.DNSRecord)

	for _, app := range p.staticApps {
		rec := app.DNSRecord()
		if rec != nil {
			recs[rec.Record] = rec
		}

		state := app.AppState()
		if state != nil {
			appStates[app.App.Id] = state
		}
	}

	for _, app := range p.functionApps {
		rec := app.DNSRecord()
		if rec != nil {
			if _, ok := recs[rec.Record]; !ok {
				recs[rec.Record] = rec
			}
		}

		state := app.AppState()
		if state != nil {
			appStates[app.App.Id] = state
		}
	}

	dnsRecords := make([]*apiv1.DNSRecord, 0, len(recs))

	for _, rec := range recs {
		dnsRecords = append(dnsRecords, rec)
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return nil, nil, nil, err
	}

	return appStates, dnsRecords, diff, err
}

func (p *Plugin) Plan(ctx context.Context, reg *registry.Registry, r *apiv1.PlanRequest) (*apiv1.PlanResponse, error) {
	var (
		diff       []*registry.Diff
		err        error
		dnsRecords []*apiv1.DNSRecord
		appStates  map[string]*apiv1.AppState
	)

	if r.Priority == 500 {
		diff, err = p.processDeployInit(ctx, reg, r.Apps, r.State, r.Domains, false)
	} else {
		appStates, dnsRecords, diff, err = p.processDeploy(ctx, reg, r.Apps, r.State)
	}

	if err != nil {
		return nil, err
	}

	data, err := reg.Dump()
	if err != nil {
		return nil, err
	}

	r.State.Registry = data

	return &apiv1.PlanResponse{
		Plan: &apiv1.Plan{
			Actions: registry.PlanActionFromDiff(diff),
		},
		State:      r.State,
		AppStates:  appStates,
		Domains:    r.Domains,
		DnsRecords: dnsRecords,
	}, nil
}

func (p *Plugin) Apply(r *apiv1.ApplyRequest, reg *registry.Registry, stream apiv1.DeployPluginService_ApplyServer) error {
	ctx := stream.Context()
	pctx := p.PluginContext()

	var (
		diff       []*registry.Diff
		err        error
		dnsRecords []*apiv1.DNSRecord
		appStates  map[string]*apiv1.AppState
	)

	if r.Priority == 500 {
		diff, err = p.processDeployInit(ctx, reg, r.Apps, r.State, r.Domains, true)
	} else {
		appStates, dnsRecords, diff, err = p.processDeploy(ctx, reg, r.Apps, r.State)
	}

	if err != nil {
		return err
	}

	err = reg.Apply(ctx, pctx, diff, plugin_go.DefaultRegistryApplyCallback(stream))

	_ = p.processOriginCertificates(ctx, true)

	data, saveErr := reg.Dump()
	if err == nil {
		err = saveErr
	}

	r.State.Registry = data

	_ = stream.Send(&apiv1.ApplyResponse{
		Response: &apiv1.ApplyResponse_Done{
			Done: &apiv1.ApplyDoneResponse{
				State:      r.State,
				AppStates:  appStates,
				Domains:    r.Domains,
				DnsRecords: dnsRecords,
			},
		},
	})

	return err
}
