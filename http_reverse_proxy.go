package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"go.uber.org/zap"
)

func NewPathPreservingProxy(turl string, proxyConfig ProxyConfig) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(turl)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Director = func(req *http.Request) {
		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host

		// this bit right here makes sure that all the rpc URLs with
		// /<apikey> work.
		req.URL.Path = targetURL.Path

		// Workaround to reserve request body in ReverseProxy.ErrorHandler
		// see more here: https://github.com/golang/go/issues/33726\
		if req.Body != nil && req.ContentLength != 0 {
			var buf bytes.Buffer
			tee := io.TeeReader(req.Body, &buf)
			req.Body = ioutil.NopCloser(tee)
			ctx := context.WithValue(req.Context(), "bodybuf", &buf)
			r2 := req.WithContext(ctx)
			*req = *r2
		}

		zap.L().Debug(fmt.Sprintf("forwarding request to: %s", req.URL))
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
