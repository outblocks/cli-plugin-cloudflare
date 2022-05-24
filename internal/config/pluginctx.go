package config

import (
	"sync"

	"github.com/cloudflare/cloudflare-go"
	"github.com/outblocks/outblocks-plugin-go/env"
)

type funcCacheData struct {
	ret interface{}
	err error
}

type PluginContext struct {
	env      env.Enver
	cli      *cloudflare.API
	settings *Settings

	funcCache map[string]*funcCacheData

	mu struct {
		funcCache sync.Mutex
	}
}

func NewPluginContext(e env.Enver, cli *cloudflare.API, settings *Settings) *PluginContext {
	return &PluginContext{
		env:       e,
		cli:       cli,
		settings:  settings,
		funcCache: make(map[string]*funcCacheData),
	}
}

func (c *PluginContext) Settings() *Settings {
	return c.settings
}

func (c *PluginContext) Env() env.Enver {
	return c.env
}

func (c *PluginContext) CloudflareClient() *cloudflare.API {
	return c.cli
}

func (c *PluginContext) FuncCache(key string, f func() (interface{}, error)) (interface{}, error) {
	c.mu.funcCache.Lock()

	cache, ok := c.funcCache[key]
	if !ok {
		ret, err := f()
		cache = &funcCacheData{ret: ret, err: err}
		c.funcCache[key] = cache
	}

	c.mu.funcCache.Unlock()

	return cache.ret, cache.err
}
