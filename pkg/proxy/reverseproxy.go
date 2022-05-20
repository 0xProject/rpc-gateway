package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/pkg/errors"

	"go.uber.org/zap"
)

// TODO
// This code needs a new abstraction. We should bring a model and attach helper to a model.
//

func doProcessRequest(r *http.Request, config TargetConfig) error {
	var body io.Reader
	var buf bytes.Buffer
	var err error

	if r.Body == nil {
		return errors.New("no body")
	}

	// The standard library stores ContentLength as signed data type.
	//
	if r.ContentLength == 0 || r.ContentLength < 0 {
		return errors.New("invalid content length")
	}

	if r.Header.Get("Content-Encoding") == "gzip" && !config.Connection.HTTP.Compression {
		body, err = doGunzip(r, config)
		if err != nil {
			return errors.Wrap(err, "cannot gunzip data")
		}
	} else {
		body = io.TeeReader(r.Body, &buf)
	}

	r.Body = io.NopCloser(body)

	return nil
}

func doGunzip(r *http.Request, config TargetConfig) (io.Reader, error) {
	var buf bytes.Buffer
	var body io.Reader

	uncompressed, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "cannot decompress the data")
	}
	// Decompress the body.
	//
	data, err := ioutil.ReadAll(uncompressed)
	if err != nil {
		return nil, errors.Wrap(err, "cannot read uncompressed data")
	}

	// Replace body content with uncompressed data
	// Remove the "Content-Encoding: gzip" because the body is decompressed already
	// and correct the Content-Length header
	//
	body = io.TeeReader(bytes.NewReader(data), &buf)

	r.Header.Del("Content-Encoding")
	r.ContentLength = int64(len(data))

	return body, nil
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
		if err := doProcessRequest(r, targetConfig); err != nil {
			zap.L().Error("cannot process request", zap.Error(err))
		}

		zap.L().Debug("request forward", zap.String("URL", r.URL.String()))
	}

	proxy.Transport = &RetryRoundTrip{
		Next: http.DefaultTransport,
		Config: RetryRoundTripConfig{
			Retries: int(config.Proxy.AllowedNumberOfRetriesPerTarget),
			Delay:   config.Proxy.RetryDelay,
		},
		RetryOn: func(resp *http.Response) bool {
			// I am dumb and I always expect HTTP 200.
			//
			return resp.StatusCode != 200
		},
	}

	// conntrackDialer := conntrack.NewDialContextFunc(
	// 	conntrack.DialWithName(targetConfig.Name),
	// 	conntrack.DialWithTracing(),
	// 	conntrack.DialWithDialer(&net.Dialer{
	// 		Timeout:   30 * time.Second,
	// 		KeepAlive: 30 * time.Second,
	// 	}),
	// )

	// proxy.Transport = &http.Transport{
	// 	Proxy:                 http.ProxyFromEnvironment,
	// 	DialContext:           conntrackDialer,
	// 	ForceAttemptHTTP2:     true,
	// 	MaxIdleConns:          100,
	// 	IdleConnTimeout:       30 * time.Second,
	// 	TLSHandshakeTimeout:   10 * time.Second,
	// 	ExpectContinueTimeout: 1 * time.Second,
	// 	ResponseHeaderTimeout: config.Proxy.UpstreamTimeout,
	// }

	// conntrack.PreRegisterDialerMetrics(targetConfig.Name)

	return proxy, nil
}
