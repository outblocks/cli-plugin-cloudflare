package cf

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type PagesProject struct {
	registry.ResourceBase

	Name      fields.StringInputField `state:"force_new"`
	AccountID fields.StringInputField `state:"force_new"`
	Domains   fields.ArrayInputField

	InternalDomain fields.StringOutputField
}

func (o *PagesProject) ReferenceID() string {
	return fields.GenerateID("accounts/%s/pages/%s", o.AccountID, o.Name)
}

func (o *PagesProject) GetName() string {
	return fields.VerboseString(o.Name)
}

func (o *PagesProject) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	proj, _ := cli.PagesProject(ctx, o.AccountID.Wanted(), o.Name.Wanted())
	if proj.Name == "" {
		o.MarkAsNew()

		return nil
	}

	o.MarkAsExisting()

	o.InternalDomain.SetCurrent(proj.SubDomain)

	return nil
}

func (o *PagesProject) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	wranglerCli := pctx.WranglerCloudflareClient()
	cli := pctx.CloudflareClient()

	proj, err := wranglerCli.CreatePagesProject(ctx, o.Name.Wanted())

	o.InternalDomain.SetCurrent(proj.SubDomain)

	for _, d := range o.Domains.Wanted() {
		_, err = cli.PagesAddDomain(ctx, cloudflare.PagesDomainParameters{
			AccountID:   o.AccountID.Wanted(),
			ProjectName: o.Name.Wanted(),
			DomainName:  d.(string),
		})
		if err != nil {
			return err
		}
	}

	return err
}

func (o *PagesProject) Update(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()
	m := make(map[string]bool)

	for _, d := range o.Domains.Current() {
		m[d.(string)] = true
	}

	var toadd []string

	for _, d := range o.Domains.Wanted() {
		dstr := d.(string)
		if m[dstr] {
			delete(m, dstr)
		} else {
			toadd = append(toadd, dstr)
		}
	}

	for d := range m {
		err := cli.PagesDeleteDomain(ctx, cloudflare.PagesDomainParameters{
			AccountID:   o.AccountID.Wanted(),
			ProjectName: o.Name.Wanted(),
			DomainName:  d,
		})
		if err != nil {
			return err
		}
	}

	for _, d := range toadd {
		_, err := cli.PagesAddDomain(ctx, cloudflare.PagesDomainParameters{
			AccountID:   o.AccountID.Wanted(),
			ProjectName: o.Name.Wanted(),
			DomainName:  d,
		})
		if err != nil {
			return err
		}
	}

	return fmt.Errorf("unimplemented")
}

func (o *PagesProject) Delete(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	if cli.AccountID != o.AccountID.Current() {
		return nil
	}

	for _, d := range o.Domains.Current() {
		err := cli.PagesDeleteDomain(ctx, cloudflare.PagesDomainParameters{
			AccountID:   o.AccountID.Current(),
			ProjectName: o.Name.Current(),
			DomainName:  d.(string),
		})
		if err != nil {
			return err
		}
	}

	return cli.DeletePagesProject(ctx, o.AccountID.Current(), o.Name.Current())
}
