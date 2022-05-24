package cf

import (
	"github.com/outblocks/outblocks-plugin-go/registry"
)

var Types = []registry.Resource{
	(*DNSRecord)(nil),
	(*OriginCertificate)(nil),
}

func RegisterTypes(reg *registry.Registry) {
	for _, t := range Types {
		reg.RegisterType(t)
	}
}
