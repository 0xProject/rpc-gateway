package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func NewPathPreservingProxy(turl string, proxyConfig ProxyConfig) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(turl)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		// this bit right here makes sure that all the rpc URLs with
		// /<apikey> work.
		req.URL.Path = fmt.Sprintf("%s%s", targetURL.Path, req.URL.Path)
		req.Host = targetURL.Host
	}

	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: proxyConfig.UpstreamTimeout,
	}

	return proxy, nil
}
