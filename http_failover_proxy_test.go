package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func createTestRpcGatewayConfig() RpcGatewayConfig {
	return RpcGatewayConfig{
		Metrics:      MetricsConfig{},
		Proxy:        ProxyConfig{
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
		Targets:      []TargetConfig{},
	}
}

func TestHttpFailoverProxyRerouteRequests(t *testing.T) {
	fakeRpc1Server := httptest.NewServer(http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer fakeRpc1Server.Close()
	fakeRpc2Server := httptest.NewServer(&responder{
		value: []byte(`{"name": "fakeRpc2"}`),
		onRequest: func(r *http.Request) {},
	})
	defer fakeRpc2Server.Close()
	rpcGatewayConfig := createTestRpcGatewayConfig()
	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRpc1Server.URL,
				},
			},
		},
		{
			Name: "Server2",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRpc2Server.URL,
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
	httpFailoverProxy := NewHttpFailoverProxy(rpcGatewayConfig, healthcheckManager)

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(httpFailoverProxy.ServeHTTP)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("server returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	want := `{"name": "fakeRpc2"}`
	if rr.Body.String() != want {
		t.Errorf("server returned unexpected body: got %v want %v", rr.Body.String(), want)
	}
}


func TestHttpFailoverProxyNotRerouteRequests(t *testing.T) {
	fakeRpc1Server := httptest.NewServer(http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
	}))
	defer fakeRpc1Server.Close()
	fakeRpc2Server := httptest.NewServer(&responder{
		value: []byte(""),
		onRequest: func(r *http.Request) {},
	})
	defer fakeRpc2Server.Close()
	rpcGatewayConfig := createTestRpcGatewayConfig()
	rpcGatewayConfig.Targets = []TargetConfig{
		{
			Name: "Server1",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRpc1Server.URL,
				},
			},
		},
		{
			Name: "Server2",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL: fakeRpc2Server.URL,
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
	httpFailoverProxy := NewHttpFailoverProxy(rpcGatewayConfig, healthcheckManager)

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