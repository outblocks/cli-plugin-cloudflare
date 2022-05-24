package cf

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
)

type OriginCertificate struct {
	registry.ResourceBase

	Hostnames       fields.ArrayInputField  `state:"force_new"`
	RequestType     fields.StringInputField `state:"force_new" default:"origin-rsa"`
	RequestValidity fields.IntInputField    `state:"force_new" default:"5475"`

	ID          fields.StringOutputField
	ExpiresOn   fields.IntOutputField
	CSR         fields.StringOutputField
	Certificate fields.StringOutputField
	PrivateKey  fields.StringOutputField
}

func (o *OriginCertificate) GetName() string {
	h := o.Hostnames.Any()[0]

	return fields.VerboseString(fields.String(h.(string)))
}

func (o *OriginCertificate) Read(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	if o.ID.Current() == "" {
		return nil
	}

	c, err := cli.OriginCertificate(ctx, o.ID.Current())
	if err != nil || !c.RevokedAt.IsZero() {
		o.MarkAsNew()

		return nil
	}

	o.MarkAsExisting()

	return nil
}

func (o *OriginCertificate) Create(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	hostnames := make([]string, len(o.Hostnames.Wanted()))

	for i, v := range o.Hostnames.Wanted() {
		hostnames[i] = v.(string)
	}

	keyBytes, _ := rsa.GenerateKey(rand.Reader, 2048)

	csr := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: "Cloudflare Origin Certificate",
		},
	}

	csrBytes, _ := x509.CreateCertificateRequest(rand.Reader, &csr, keyBytes)
	certReqPem := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes}))
	privateKeyPem := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(keyBytes),
	}))

	out, err := cli.CreateOriginCertificate(ctx, cloudflare.OriginCACertificate{
		CSR:             certReqPem,
		Hostnames:       hostnames,
		RequestType:     o.RequestType.Wanted(),
		RequestValidity: o.RequestValidity.Wanted(),
	})

	if err != nil {
		return err
	}

	cert, err := cli.OriginCertificate(ctx, out.ID)
	if err != nil {
		return err
	}

	o.ID.SetCurrent(cert.ID)
	o.CSR.SetCurrent(certReqPem)
	o.PrivateKey.SetCurrent(privateKeyPem)
	o.Certificate.SetCurrent(cert.Certificate)
	o.ExpiresOn.SetCurrent(int(cert.ExpiresOn.Unix()))

	return nil
}

func (o *OriginCertificate) Update(ctx context.Context, meta interface{}) error {
	return fmt.Errorf("unimplemented")
}

func (o *OriginCertificate) Delete(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	cli := pctx.CloudflareClient()

	_, err := cli.RevokeOriginCertificate(ctx, o.ID.Current())

	return err
}
