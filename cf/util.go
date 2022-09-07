package cf

import (
	"fmt"
	"strings"

	"github.com/outblocks/outblocks-plugin-go/env"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/util"
)

var Types = []registry.Resource{
	(*DNSRecord)(nil),
	(*OriginCertificate)(nil),
	(*PagesProject)(nil),
	(*PagesFiles)(nil),
	(*PagesDeployment)(nil),
	(*WorkerScript)(nil),
	(*WorkerRoute)(nil),
}

var (
	_ registry.ResourceDiffCalculator = (*PagesFiles)(nil)
)

func RegisterTypes(reg *registry.Registry) {
	for _, t := range Types {
		reg.RegisterType(t)
	}
}

func ShortShaID(id string) string {
	return util.LimitString(util.SHAString(id), 4)
}

func ID(e env.Enver, resourceID string) string {
	sanitizedID := util.SanitizeName(resourceID, false, false)
	sanitizedEnv := util.LimitString(util.SanitizeName(e.Env(), false, false), 4)

	if len(sanitizedID) > 44 {
		sanitizedID = util.LimitString(sanitizedID, 40) + ShortShaID(sanitizedID)
	}

	return fmt.Sprintf("%s-%s-%s", sanitizedID, sanitizedEnv, ShortShaID(e.ProjectID()))
}

func FixURL(url string) string {
	split := strings.SplitN(url, "://", 2)
	if len(split) == 2 {
		url = split[1]
	}

	path := ""

	urlSplit := strings.SplitN(url, "/", 2)

	if len(urlSplit) == 2 {
		path = "/" + urlSplit[1]
	}

	if path == "" || path == "/" {
		return urlSplit[0] + "/*"
	}

	if strings.HasSuffix(path, "/") {
		path += "*"
	}

	return urlSplit[0] + path
}
