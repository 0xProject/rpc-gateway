package proxy

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/mwitkow/go-conntrack"
	"github.com/pkg/errors"
)

func NewNodeProviderProxy(config NodeProviderConfig) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(config.Connection.HTTP.URL)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse url")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.URL.Path = target.Path
	}

	conntrackDialer := conntrack.NewDialContextFunc(
		conntrack.DialWithName(config.Name),
		conntrack.DialWithTracing(),
		conntrack.DialWithDialer(&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
	)

	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           conntrackDialer,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	conntrack.PreRegisterDialerMetrics(config.Name)

	return proxy, nil
}
