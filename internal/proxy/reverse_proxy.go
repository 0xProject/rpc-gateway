package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/mwitkow/go-conntrack"
	"github.com/pkg/errors"

	"go.uber.org/zap"
)

func doProcessRequest(r *http.Request, config TargetConfig) error {
	if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") && !config.Connection.HTTP.Compression {
		return errors.Wrap(doGunzip(r), "gunzip failed")
	}

	return nil
}

func doGunzip(r *http.Request) error {
	uncompressed, err := gzip.NewReader(r.Body)
	if err != nil {
		return errors.Wrap(err, "cannot decompress the data")
	}

	body := &bytes.Buffer{}
	if _, err := io.Copy(body, uncompressed); err != nil { // nolint:gosec
		return errors.Wrap(err, "cannot read uncompressed data")
	}

	r.Header.Del("Content-Encoding")
	r.Body = io.NopCloser(body)
	r.ContentLength = int64(body.Len())

	return nil
}

func NewReverseProxy(targetConfig TargetConfig, config Config) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(targetConfig.Connection.HTTP.URL)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse url")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.URL.Path = target.Path

		if err := doProcessRequest(r, targetConfig); err != nil {
			zap.L().Error("cannot process request", zap.Error(err))
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
		DisableKeepAlives:     targetConfig.Connection.HTTP.DisableKeepAlives,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: config.Proxy.UpstreamTimeout,
	}

	conntrack.PreRegisterDialerMetrics(targetConfig.Name)

	return proxy, nil
}
