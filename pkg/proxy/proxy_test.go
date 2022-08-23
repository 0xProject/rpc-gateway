package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func createConfig() Config {
	return Config{
		Proxy: ProxyConfig{
			AllowedNumberOfRetriesPerTarget: 3,
			AllowedNumberOfReroutes:         1,
			RetryDelay:                      0,
			UpstreamTimeout:                 0,
		},
		HealthChecks: HealthCheckConfig{
			Interval:                      0,
			Timeout:                       0,
			FailureThreshold:              0,
			SuccessThreshold:              0,
			RollingWindowSize:             100,
			RollingWindowFailureThreshold: 0.9,
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
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("server returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// This test makes sure that the request's body is forwarded to
	// the next RPC Provider
	want := `{"this_is": "body"}`
	if rr.Body.String() != want {
		t.Errorf("server returned unexpected body: got %v want %v", rr.Body.String(), want)
	}
}

func TestHttpFailoverProxyNotRerouteRequests(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
	}))
	defer fakeRPC1Server.Close()
	fakeRPC2Server := httptest.NewServer(&responder{
		value:     []byte(""),
		onRequest: func(r *http.Request) {},
	})
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

	// Tell HttpFailoverProxy to not reroute the request
	rpcGatewayConfig.Proxy.AllowedNumberOfReroutes = 0

	healthcheckManager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: rpcGatewayConfig.Targets,
		Config:  rpcGatewayConfig.HealthChecks,
	})
	// Setup HttpFailoverProxy but not starting the HealthCheckManager
	// so the no target will be tainted or marked as unhealthy by the HealthCheckManager
	httpFailoverProxy := NewProxy(rpcGatewayConfig, healthcheckManager)

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)

	handler.ServeHTTP(rr, req)

	// expect server to return 503 as the first RPC is unhealthy and
	// the failover proxy doesn't try to reroute the request to the second RPC (healthy)
	if status := rr.Code; status != http.StatusServiceUnavailable {
		t.Errorf("server returned wrong status code: got %v want %v", status, http.StatusServiceUnavailable)
	}
}

func TestHttpFailoverProxyDecompressRequest(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var receivedBody, receivedHeaderContentEncoding, receivedHeaderContentLength string
	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderContentEncoding = r.Header.Get("Content-Encoding")
		receivedHeaderContentLength = r.Header.Get("Content-Length")
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

	// Tell HttpFailoverProxy to not reroute the request
	rpcGatewayConfig.Proxy.AllowedNumberOfReroutes = 0

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
	if err != nil {
		t.Fatal(err)
	}
	err = g.Close()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "/", &buf)
	req.Header.Add("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	want := `{"body": "content"}`
	if receivedBody != want {
		t.Errorf("the proxy didn't decompress the request before forwarding the body to the target: want: %s, got: %s", want, receivedBody)
	}
	want = ""
	if receivedHeaderContentEncoding != want {
		t.Errorf("the proxy didn't remove the `Content-Encoding: gzip` after decompressing the body, want empty, got: %s", receivedHeaderContentEncoding)
	}
	want = strconv.Itoa(len(`{"body": "content"}`))
	if receivedHeaderContentLength != want {
		t.Errorf("the proxy didn't correctly re-calculate the `Content-Length` after decompressing the body, want: %s, got: %s", want, receivedHeaderContentLength)
	}
}

func TestHttpFailoverProxyWithCompressionSupportedTarget(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var receivedHeaderContentEncoding string
	var receivedBody []byte
	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderContentEncoding = r.Header.Get("Content-Encoding")
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

	// Tell HttpFailoverProxy to not reroute the request
	rpcGatewayConfig.Proxy.AllowedNumberOfReroutes = 0

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
	if err != nil {
		t.Fatal(err)
	}
	err = g.Close()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "/", &buf)
	req.Header.Add("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	want := "gzip"
	if receivedHeaderContentEncoding != want {
		t.Errorf("the proxy didn't keep the header of `Content-Encoding: gzip`, want: %s, got: %s", want, receivedHeaderContentEncoding)
	}

	var wantBody bytes.Buffer
	g = gzip.NewWriter(&wantBody)
	g.Write([]byte(`{"body": "content"}`))
	g.Close()

	if !bytes.Equal(receivedBody, wantBody.Bytes()) {
		t.Errorf("the proxy didn't keep the body as is when forwarding gzipped body to the target.")
	}
}

func TestHttpFailoverProxyNotObserveFailureWhenClientCanceledRequest(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // The RPC Provider takes 100ms to reply
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

	req, err := http.NewRequest("POST", "/", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	// Make one successful request
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	// Now create a new request that will be canceled by client
	// before the proxy can respond
	ctx, cancel := context.WithCancel(context.TODO())
	req, err = http.NewRequest("POST", "/", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	// Mimic the client canceling the request after 50ms (mid-flight request)
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(httpFailoverProxy.ServeHTTP)
	handler.ServeHTTP(rr, req)

	rollingWindow := healthcheckManager.GetRollingWindowByName("Server1")
	if len(rollingWindow.Window()) != 1 {
		t.Errorf("the proxy observed a canceled request while it shouldn't")
	}
}
