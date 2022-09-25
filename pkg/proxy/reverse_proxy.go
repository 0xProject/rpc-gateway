package proxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/mwitkow/go-conntrack"
	"github.com/pkg/errors"

	"go.uber.org/zap"
)

func doProcessRequest(r *http.Request) error {
	var buf bytes.Buffer

	// The standard library stores ContentLength as signed data type.
	//
	if r.ContentLength == 0 || r.ContentLength < 0 {
		return errors.New("invalid content length")
	}

	body := io.TeeReader(r.Body, &buf)

	// I don't like so much but the refactor is coming up soon!
	//
	// This is nothing more than ugly a workaround.
	// This code guarantee the context buf will not be empty upon primary
	// provider roundtrip failures.
	//
	data, err := io.ReadAll(body)
	if err != nil {
		return errors.New("cannot read body")
	}

	r.Body = io.NopCloser(bytes.NewBuffer(data))

	// Here's an interesting fact. There's no data in buf, until a call
	// to Read(). With Read() call, it will write data to bytes.Buffer.
	//
	// I want to call it out, because it's damn smart.
	//
	ctx := context.WithValue(r.Context(), "bodybuf", &buf) // nolint:revive,staticcheck

	// WithContext creates a shallow copy. It's highly important to
	// override underlying memory pointed by pointer.
	//
	r2 := r.WithContext(ctx)
	*r = *r2

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

		// Workaround to reserve request body in ReverseProxy.ErrorHandler
		// see more here: https://github.com/golang/go/issues/33726
		//
		if err := doProcessRequest(r); err != nil {
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
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: config.Proxy.UpstreamTimeout,
	}

	conntrack.PreRegisterDialerMetrics(targetConfig.Name)

	return proxy, nil
}
