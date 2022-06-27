package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/outblocks/cli-plugin-cloudflare/cf"
	plugin_go "github.com/outblocks/outblocks-plugin-go"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"google.golang.org/protobuf/types/known/structpb"
)

const pendingValue = "pending"

func (p *Plugin) computeZoneMap(state *apiv1.PluginState, domains []*apiv1.DomainInfo) error {
	var err error

	p.zoneMap = map[string]string{}

	if state.Other == nil {
		state.Other = make(map[string][]byte)
	}

	_ = json.Unmarshal(state.Other["zone_id_map"], &p.zoneMap)

	for _, domainInfo := range domains {
		for _, d := range domainInfo.Domains {
			zone := p.getDomainZoneName(d)
			if zone == "" {
				continue
			}

			id := p.zoneMap[zone]

			if id == "" {
				id, err = p.cli.ZoneIDByName(zone)
				if err != nil {
					return err
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

	zone := p.getDomainZoneName(domainInfo.Domains[0])

	for _, d := range domainInfo.Domains[1:] {
		if zone != p.getDomainZoneName(d) {
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

		if domainInfo.Other.AsMap()["cloudflare_proxy"] != true && domainInfo.Cert != "" && domainInfo.Key != "" {
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

func (p *Plugin) registerOriginCertificates(reg *registry.Registry, domains []*apiv1.DomainInfo) error {
	p.originCerts = make(map[*cf.OriginCertificate]*apiv1.DomainInfo)

	for _, d := range domains {
		zone := p.getDomainZoneName(d.Domains[0])
		h := make([]fields.Field, len(d.Domains))

		if containsNestedSubdomain(d) {
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
		d.Cert = pendingValue
		d.Key = pendingValue

		if d.Other.GetFields() == nil {
			d.Other, _ = structpb.NewStruct(nil)
		}

		d.Other.GetFields()["cloudflare_proxy"] = structpb.NewBoolValue(true)
	}

	return nil
}

func (p *Plugin) processOriginCertificates() {
	for cert, domain := range p.originCerts {
		if cert.IsExisting() {
			domain.Cert = cert.Certificate.Current()
			domain.Key = cert.PrivateKey.Current()
		}
	}
}

func (p *Plugin) Plan(ctx context.Context, reg *registry.Registry, r *apiv1.PlanRequest) (*apiv1.PlanResponse, error) {
	pctx := p.PluginContext()
	reg = reg.Partition("init")

	err := p.computeZoneMap(r.State, r.Domains)
	if err != nil {
		return nil, err
	}

	domains := p.validCloudflareDomains(r.Domains)

	if len(domains) == 0 {
		return &apiv1.PlanResponse{}, nil
	}

	if !strings.HasPrefix(p.cli.APIUserServiceKey, "v1.0-") {
		p.log.Infoln("'CLOUDFLARE_API_USER_SERVICE_KEY' not set or invalid, skipping Origin CA creation...")

		return &apiv1.PlanResponse{}, nil
	}

	err = prepareRegistry(reg, r.State.Registry)
	if err != nil {
		return nil, err
	}

	err = p.registerOriginCertificates(reg, domains)
	if err != nil {
		return nil, err
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return nil, err
	}

	p.processOriginCertificates()

	data, err := reg.Dump()
	if err != nil {
		return nil, err
	}

	r.State.Registry = data

	return &apiv1.PlanResponse{
		Deploy: &apiv1.Plan{
			Actions: registry.PlanActionFromDiff(diff),
		},
		State:   r.State,
		Domains: r.Domains,
	}, nil
}

func (p *Plugin) Apply(r *apiv1.ApplyRequest, reg *registry.Registry, stream apiv1.DeployPluginService_ApplyServer) error {
	ctx := stream.Context()
	pctx := p.PluginContext()
	reg = reg.Partition("init")

	err := p.computeZoneMap(r.State, r.Domains)
	if err != nil {
		return err
	}

	domains := p.validCloudflareDomains(r.Domains)

	if len(domains) == 0 {
		return nil
	}

	if !strings.HasPrefix(p.cli.APIUserServiceKey, "v1.0-") {
		return nil
	}

	err = prepareRegistry(reg, r.State.Registry)
	if err != nil {
		return err
	}

	err = p.registerOriginCertificates(reg, domains)
	if err != nil {
		return err
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return err
	}

	err = reg.Apply(ctx, pctx, diff, plugin_go.DefaultRegistryApplyCallback(stream))

	p.processOriginCertificates()

	data, saveErr := reg.Dump()
	if err == nil {
		err = saveErr
	}

	r.State.Registry = data

	_ = stream.Send(&apiv1.ApplyResponse{
		Response: &apiv1.ApplyResponse_Done{
			Done: &apiv1.ApplyDoneResponse{
				State:   r.State,
				Domains: r.Domains,
			},
		},
	})

	return err
}
