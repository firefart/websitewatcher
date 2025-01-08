package http

import (
	"net/http"
	"net/url"

	"github.com/firefart/websitewatcher/internal/config"
	"golang.org/x/net/http/httpproxy"
)

type proxy struct {
	proxyFunc func(*url.URL) (*url.URL, error)
}

func newProxy(c config.ProxyConfig) (*proxy, error) {
	proxyURL, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	if c.Username != "" && c.Password != "" {
		proxyURL.User = url.UserPassword(c.Username, c.Password)
	}
	config := &httpproxy.Config{
		HTTPProxy:  proxyURL.String(),
		HTTPSProxy: proxyURL.String(),
		NoProxy:    c.NoProxy,
	}
	return &proxy{
		proxyFunc: config.ProxyFunc(),
	}, nil
}

func (p *proxy) ProxyFromConfig(req *http.Request) (*url.URL, error) {
	return p.proxyFunc(req.URL)
}
