package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/mwitkow/go-conntrack"
	"github.com/pkg/errors"

	"go.uber.org/zap"
)

func NewPathPreservingProxy(targetConfig TargetConfig, proxyConfig ProxyConfig) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(targetConfig.Connection.HTTP.URL)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse url")
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Director = func(r *http.Request) {
		r.Host = targetURL.Host
		r.URL.Scheme = targetURL.Scheme
		r.URL.Host = targetURL.Host

		// this bit right here makes sure that all the rpc URLs with
		// /<apikey> work.
		//
		r.URL.Path = targetURL.Path

		// Workaround to reserve request body in ReverseProxy.ErrorHandler
		// see more here: https://github.com/golang/go/issues/33726
		//
		if r.Body != nil && r.ContentLength > 0 {
			var buf bytes.Buffer
			var body io.Reader

			// If the body is gzip-ed but the target doesn't support request
			// compression we decompress the body before sending
			//
			// Edge case: target 1 doesn't support request compression but
			// target 2 does In this case, since the body is already
			// decompressed to serve the target 1, in a reroute event, target 2
			// will just receive the decompressed body instead of the original
			// compressed one. We could fix this by either re-compress the body
			// or keep a copy of the original (gzipped) body.
			//
			if r.Header.Get("Content-Encoding") == "gzip" && !targetConfig.Connection.HTTP.Compression {
				zap.L().Debug("go to gzip")

				uncompressed, err := gzip.NewReader(r.Body)
				if err != nil {
					zap.L().Error("cannot initiate gzip reader", zap.Error(err))

					// Failed to read gzip content, treat it as uncompressed data.
					//
					body = io.TeeReader(r.Body, &buf)
				} else {
					// Decompress the body.
					//
					data, err := ioutil.ReadAll(uncompressed)
					if err != nil {
						zap.L().Fatal("cannot read uncompress data", zap.Error(err))
					}

					// Replace body content with uncompressed data
					// Remove the "Content-Encoding: gzip" because the body is decompressed already
					// and correct the Content-Length header
					//
					body = io.TeeReader(bytes.NewReader(data), &buf)

					r.Header.Del("Content-Encoding")
					r.ContentLength = int64(len(data))
				}
			} else {
				zap.L().Debug("not go to gzip")
				body = io.TeeReader(r.Body, &buf)
			}

			r.Body = io.NopCloser(body)

			// Here's an interesting fact. There's no data in buf, until a call
			// to Read(). With Read() call, it will write data to bytes.Buffer.
			//
			// I want to call it out, because it's damn smart.
			//
			ctx := context.WithValue(r.Context(), "bodybuf", &buf)

			// WithContext creates a shallow copy. It's highly important to
			// override underlying memory pointed by pointer.
			//
			r2 := r.WithContext(ctx)
			*r = *r2
		}

		zap.L().Debug("request forward", zap.String("URL", r.URL.String()))
	}

	conntrackDialer := conntrack.NewDialContextFunc(
		conntrack.DialWithName(targetConfig.Name),
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
		ResponseHeaderTimeout: proxyConfig.UpstreamTimeout,
	}

	conntrack.PreRegisterDialerMetrics(targetConfig.Name)

	return proxy, nil
}
