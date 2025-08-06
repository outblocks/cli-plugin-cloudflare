package cf

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type DNSRecord struct {
	registry.ResourceBase

	ZoneID  fields.StringInputField `state:"force_new"`
	Name    fields.StringInputField
	Type    fields.StringInputField
	Value   fields.StringInputField
	Proxied fields.BoolInputField

	ID fields.StringOutputField
}

func (o *DNSRecord) ReferenceID() string {
	return fields.GenerateID("zones/%s/records/%s", o.ZoneID, o.Name)
}

func (o *DNSRecord) GetName() string {
	return fields.VerboseString(o.Name)
}

func (o *DNSRecord) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()
	zoneID := o.ZoneID.Any()

	if zoneID == "" {
		o.MarkAsNew()

		return nil
	}

	records, err := pctx.FuncCache(fmt.Sprintf("DNSRecords:list:%s", zoneID), func() (interface{}, error) {
		return cli.DNSRecords(ctx, zoneID, cloudflare.DNSRecord{})
	})
	if err != nil {
		return fmt.Errorf("error fetching dns records: %w", err)
	}

	var rec *cloudflare.DNSRecord

	for _, r := range records.([]cloudflare.DNSRecord) { //nolint: gocritic
		if o.ID.Current() == r.ID {
			rec = &r //nolint
			break
		}
	}

	if rec == nil {
		for _, r := range records.([]cloudflare.DNSRecord) { //nolint: gocritic
			if o.Name.Any() == r.Name {
				rec = &r //nolint
				break
			}
		}
	}

	if rec == nil {
		o.MarkAsNew()

		return nil
	}

	o.MarkAsExisting()
	o.ZoneID.SetCurrent(zoneID)
	o.ID.SetCurrent(rec.ID)
	o.Name.SetCurrent(rec.Name)
	o.Type.SetCurrent(rec.Type)
	o.Value.SetCurrent(rec.Content)

	proxied := false

	if rec.Proxied != nil && *rec.Proxied {
		proxied = true
	}

	o.Proxied.SetCurrent(proxied)

	return nil
}

func (o *DNSRecord) createDNSRecord() *cloudflare.DNSRecord {
	rec := cloudflare.DNSRecord{
		Name:    o.Name.Wanted(),
		Type:    o.Type.Wanted(),
		Content: o.Value.Wanted(),
	}

	if val, ok := o.Proxied.LookupWanted(); ok {
		rec.Proxied = &val
	}

	return &rec
}

func (o *DNSRecord) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	rec, err := cli.CreateDNSRecord(ctx, o.ZoneID.Wanted(), *o.createDNSRecord())
	if err != nil {
		return err
	}

	o.ID.SetCurrent(rec.Result.ID)

	return nil
}

func (o *DNSRecord) Update(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	return cli.UpdateDNSRecord(ctx, o.ZoneID.Current(), o.ID.Current(), *o.createDNSRecord())
}

func (o *DNSRecord) Delete(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	return cli.DeleteDNSRecord(ctx, o.ZoneID.Current(), o.ID.Current())
}
