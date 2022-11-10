package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-http-utils/headers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func createConfig() Config {
	return Config{
		Proxy: ProxyConfig{
			UpstreamTimeout: 1 * time.Second,
		},
		HealthChecks: HealthCheckConfig{
			Interval:         5 * time.Second,
			Timeout:          1 * time.Second,
			FailureThreshold: 2,
			SuccessThreshold: 1,
		},
		Targets: []TargetConfig{},
	}
}

func TestHttpFailoverProxyRerouteRequests(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusInternalServerError)
	}))
	defer fakeRPC1Server.Close()

	fakeRPC2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	}))
	defer fakeRPC2Server.Close()

	rpcGatewayConfig := createConfig()
	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRPC1Server.URL,
				},
			},
		},
		{
			Name: "Server2",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRPC2Server.URL,
				},
			},
		},
	}
	healthcheckManager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: rpcGatewayConfig.Targets,
		Config:  rpcGatewayConfig.HealthChecks,
	})

	// Setup HttpFailoverProxy but not starting the HealthCheckManager
	// so the no target will be tainted or marked as unhealthy by the HealthCheckManager
	// the failoverProxy should automatically reroute the request to the second RPC Server by itself
	httpFailoverProxy := NewProxy(rpcGatewayConfig, healthcheckManager)

	requestBody := bytes.NewBufferString(`{"this_is": "body"}`)
	req, err := http.NewRequest("POST", "/", requestBody)

	assert.Nil(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// This test makes sure that the request's body is forwarded to
	// the next RPC Provider
	//
	assert.Equal(t, `{"this_is": "body"}`, rr.Body.String())
}

func TestHttpFailoverProxyDecompressRequest(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var receivedBody, receivedHeaderContentEncoding, receivedHeaderContentLength string
	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderContentEncoding = r.Header.Get(headers.ContentEncoding)
		receivedHeaderContentLength = r.Header.Get(headers.ContentLength)
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.Write([]byte("OK"))
	}))
	defer fakeRPC1Server.Close()
	rpcGatewayConfig := createConfig()
	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRPC1Server.URL,
				},
			},
		},
	}

	healthcheckManager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: rpcGatewayConfig.Targets,
		Config:  rpcGatewayConfig.HealthChecks,
	})
	// Setup HttpFailoverProxy but not starting the HealthCheckManager
	// so the no target will be tainted or marked as unhealthy by the HealthCheckManager
	httpFailoverProxy := NewProxy(rpcGatewayConfig, healthcheckManager)

	var buf bytes.Buffer
	g := gzip.NewWriter(&buf)
	_, err := g.Write([]byte(`{"body": "content"}`))
	assert.Nil(t, err)

	err = g.Close()
	assert.Nil(t, err)

	req, err := http.NewRequest("POST", "/", &buf)
	req.Header.Add(headers.ContentEncoding, "gzip")
	assert.Nil(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, receivedBody, `{"body": "content"}`)
	assert.Equal(t, receivedHeaderContentEncoding, "")
	assert.Equal(t, receivedHeaderContentLength, strconv.Itoa(len(`{"body": "content"}`)))
}

func TestHttpFailoverProxyWithCompressionSupportedTarget(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var receivedHeaderContentEncoding string
	var receivedBody []byte
	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderContentEncoding = r.Header.Get(headers.ContentEncoding)
		receivedBody, _ = io.ReadAll(r.Body)
		w.Write([]byte("OK"))
	}))
	defer fakeRPC1Server.Close()
	rpcGatewayConfig := createConfig()
	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL:         fakeRPC1Server.URL,
					Compression: true,
				},
			},
		},
	}

	healthcheckManager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: rpcGatewayConfig.Targets,
		Config:  rpcGatewayConfig.HealthChecks,
	})
	// Setup HttpFailoverProxy but not starting the HealthCheckManager
	// so the no target will be tainted or marked as unhealthy by the HealthCheckManager
	httpFailoverProxy := NewProxy(rpcGatewayConfig, healthcheckManager)

	var buf bytes.Buffer
	g := gzip.NewWriter(&buf)
	_, err := g.Write([]byte(`{"body": "content"}`))
	assert.Nil(t, err)

	err = g.Close()
	assert.Nil(t, err)

	req, err := http.NewRequest("POST", "/", &buf)
	req.Header.Add(headers.ContentEncoding, "gzip")

	assert.Nil(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, receivedHeaderContentEncoding, "gzip")

	var wantBody bytes.Buffer
	g = gzip.NewWriter(&wantBody)
	g.Write([]byte(`{"body": "content"}`))
	g.Close()

	// t.Errorf("the proxy didn't keep the body as is when forwarding gzipped body to the target.")
	assert.Equal(t, receivedBody, wantBody.Bytes())
}

func TestHTTPFailoverProxyWhenCannotConnectToPrimaryProvider(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	fakeRPCServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	}))
	defer fakeRPCServer.Close()

	rpcGatewayConfig := createConfig()

	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					// This service should not exist at all.
					//
					URL: "http://foo.bar",
				},
			},
		},
		{
			Name: "Server2",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRPCServer.URL,
				},
			},
		},
	}
	healthcheckManager := NewHealthcheckManager(
		HealthcheckManagerConfig{
			Targets: rpcGatewayConfig.Targets,
			Config:  rpcGatewayConfig.HealthChecks,
		})

	// Setup HttpFailoverProxy but not starting the HealthCheckManager so the
	// no target will be tainted or marked as unhealthy by the
	// HealthCheckManager the failoverProxy should automatically reroute the
	// request to the second RPC Server by itself

	httpFailoverProxy := NewProxy(rpcGatewayConfig, healthcheckManager)

	requestBody := bytes.NewBufferString(`{"this_is": "body"}`)
	req, err := http.NewRequest("POST", "/", requestBody)
	assert.Nil(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, rr.Code, http.StatusOK)
	assert.Equal(t, rr.Body.String(), `{"this_is": "body"}`)
}
