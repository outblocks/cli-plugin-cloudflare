package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/outblocks/cli-plugin-cloudflare/cf"
	plugin_go "github.com/outblocks/outblocks-plugin-go"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	"github.com/outblocks/outblocks-plugin-go/types"
)

func (p *Plugin) getDomainZoneName(rec string) string {
	split := strings.Split(rec, ".")
	l := len(split)

	if l < 2 {
		return ""
	}

	return fmt.Sprintf("%s.%s", split[l-2], split[l-1])
}

func (p *Plugin) registerDNSRecords(reg *registry.Registry, domains []*apiv1.DomainInfo, records []*apiv1.DNSRecord) error {
	matcher := types.NewDomainInfoMatcher(domains)

	for _, rec := range records {
		di := matcher.Match(rec.Record)
		proxy := di != nil && di.Other.AsMap()["cloudflare_proxy"] == true && strings.Count(rec.Record, ".") <= 2

		zone := p.getDomainZoneName(rec.Record)
		if zone == "" {
			continue
		}

		zoneID := p.zoneMap[zone]

		o := cf.DNSRecord{
			ZoneID:  fields.String(zoneID),
			Name:    fields.String(rec.Record),
			Type:    fields.String(rec.Type.String()[len("TYPE_"):]),
			Value:   fields.String(rec.Value),
			Proxied: fields.Bool(proxy),
		}

		rec.Created = true

		_, err := reg.RegisterPluginResource(zone, fmt.Sprintf("%s::%s", rec.Record, rec.Type), &o)
		if err != nil {
			return err
		}
	}

	return nil
}

func prepareRegistry(reg *registry.Registry, data []byte) error {
	cf.RegisterTypes(reg)

	return reg.Load(data)
}

func (p *Plugin) PlanDNS(ctx context.Context, reg *registry.Registry, r *apiv1.PlanDNSRequest) (*apiv1.PlanDNSResponse, error) {
	if r.State.Other == nil {
		r.State.Other = make(map[string][]byte)
	}

	pctx := p.PluginContext()
	records := r.DnsRecords
	state := r.State

	err := prepareRegistry(reg, r.State.Registry)
	if err != nil {
		return nil, err
	}

	// Register DNS Records.
	err = p.registerDNSRecords(reg, r.Domains, records)
	if err != nil {
		return nil, err
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return nil, err
	}

	data, err := reg.Dump()
	if err != nil {
		return nil, err
	}

	r.State.Registry = data

	return &apiv1.PlanDNSResponse{
		Dns: &apiv1.Plan{
			Actions: registry.PlanActionFromDiff(diff),
		},
		State:      state,
		DnsRecords: records,
	}, nil
}

func (p *Plugin) ApplyDNS(r *apiv1.ApplyDNSRequest, reg *registry.Registry, stream apiv1.DNSPluginService_ApplyDNSServer) error {
	ctx := stream.Context()
	pctx := p.PluginContext()
	records := r.DnsRecords

	err := prepareRegistry(reg, r.State.Registry)
	if err != nil {
		return err
	}

	// Register DNS Records.
	err = p.registerDNSRecords(reg, r.Domains, records)
	if err != nil {
		return err
	}

	// Process registry.
	diff, err := reg.ProcessAndDiff(ctx, pctx)
	if err != nil {
		return err
	}

	err = reg.Apply(ctx, pctx, diff, plugin_go.DefaultRegistryApplyDNSCallback(stream))

	data, saveErr := reg.Dump()
	if err == nil {
		err = saveErr
	}

	r.State.Registry = data

	_ = stream.Send(&apiv1.ApplyDNSResponse{
		Response: &apiv1.ApplyDNSResponse_Done{
			Done: &apiv1.ApplyDNSDoneResponse{
				State:      r.State,
				DnsRecords: r.DnsRecords,
			},
		},
	})

	return err
}
