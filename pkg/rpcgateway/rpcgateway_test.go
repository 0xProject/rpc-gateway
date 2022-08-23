package rpcgateway

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	toxiproxy "github.com/Shopify/toxiproxy/client"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var rpcGatewayConfig = `
metrics:
  port: 9090 # port for prometheus metrics, served on /metrics and /

proxy:
  port: 3000 # port for RPC gateway
  upstreamTimeout: "200m" # when is a request considered timed out
  allowedNumberOfRetriesPerTarget: 2 # The number of retries within the same RPC target for a single request
  retryDelay: "10ms" # delay between retries
  allowedNumberOfReroutes: 1 # The total number of re-routes (to the next healthy RPC target)

healthChecks:
  interval: "1s" # how often to do healthchecks
  timeout: "1s" # when should the timeout occur and considered unhealthy
  failureThreshold: 2 # how many failed checks until marked as unhealthy
  successThreshold: 1 # how many successes to be marked as healthy again
  # Rolling windows are used by the healthmanager to mark certain targets as
  # unhealthy if a failure rate is high.
  rollingWindowSize: 10 # how many requests should we be sliding over
  rollingWindowFailureThreshold: 0.90 # If the request success rate falls below 90% mark target as tainted

targets:
  - name: "ToxicAnkr"
    connection:
      http:
        url: "{{ .URLOne }}"
        compression: false
      ws:
        url: ""
  - name: "AnkrTwo"
    connection:
      http:
        url: "{{ .URLTwo }}"
        compression: false
      ws:
        url: ""
`

var rpcRequestBody = `{"jsonrpc":"2.0","method":"eth_getBlockByHash","params":["0xb3b20624f8f0f86eb50dd04688409e5cea4bd02d700bf6e79e9384d47d6a5a35",true],"id":1}`

type TestURL struct {
	URLOne string
	URLTwo string
}

func TestRpcGatewayFailover(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	// initial setup
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	// RPC backends setup
	onReq := func(r *http.Request) {
		fmt.Println("got request")
	}
	rpcBackend := &responder{
		value:     []byte(`{"jsonrpc":"2.0","id":1,"result":"0xd8d7df"}`),
		onRequest: onReq,
	}
	ts := httptest.NewServer(rpcBackend)
	defer ts.Close()

	// Toxic Proxy setup
	toxiClient := toxiproxy.NewClient("localhost:8474")
	if err := toxiClient.ResetState(); err != nil {
		t.Fatal(err)
	}
	proxy, err := toxiClient.CreateProxy("cloudflare", "0.0.0.0:9991", ts.URL[7:])
	if err != nil {
		t.Fatal(err)
	}
	_, err = proxy.AddToxic("latency_down", "latency", "downstream", 1.0, toxiproxy.Attributes{
		"latency": 100000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = toxiClient.ResetState()
		_ = proxy.Delete()
	}()

	// config string
	var tpl bytes.Buffer
	tu := TestURL{"http://0.0.0.0:9991", "https://rpc.ankr.com/eth"}
	tmpl, err := template.New("test").Parse(rpcGatewayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpl.Execute(&tpl, tu); err != nil {
		t.Fatal(err)
	}
	configString := tpl.String()

	fmt.Println(configString)
	config, err := NewRPCGatewayFromConfigString(configString)
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewRPCGateway(*config)
	go gateway.Start(context.TODO())
	gs := httptest.NewServer(gateway)
	gsClient := gs.Client()
	// We limit the connection pool to have a single sourceIP on localhost
	gsClient.Transport = &http.Transport{
		MaxIdleConns:    1,
		MaxConnsPerHost: 1,
	}

	fmt.Println("gateway serving from: ", gs.URL)

	req, _ := http.NewRequest("POST", gs.URL, bytes.NewBufferString(``))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(rpcRequestBody))
	res, err := gsClient.Do(req)
	if err != nil {
		t.Fatalf("gateway failed to handle the first failover with err: %s", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("gateway failed to handle the first failover, expect 200, got %v", res.StatusCode)
	}

	bodyContent, _ := io.ReadAll(res.Body)
	fmt.Println("Response from RPC gateway:")
	fmt.Println(string(bodyContent))

	err = gateway.Stop(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
}

func TestRPCGatewayFromConfigBytes(t *testing.T) {
	tests := []struct {
		config   string
		hasError bool
	}{
		{
			config: `
---

proxy:
  allowedNumberOfReroutes: 0

targets:
  - name: "Foo1"
    connection:
      http:
        url: "http://localhost:3000"
  - name: "Foo2"
    connection:
      http:
       url: "http://localhost:3000"
`,
			hasError: true,
		},
		{
			config: `
---

proxy:
  allowedNumberOfReroutes: 1

targets:
  - name: "Foo1"
    connection:
      http:
        url: "http://localhost:3000"
  - name: "Foo2"
    connection:
      http:
       url: "http://localhost:3000"
`,
			hasError: false,
		},
	}

	for _, tc := range tests {
		_, err := NewRPCGatewayFromConfigBytes([]byte(tc.config))

		if tc.hasError && err == nil {
			t.Errorf("expected error but got nil")
		}

		if !tc.hasError && err != nil {
			t.Errorf("didn't expect error but got: %v", err)
		}
	}
}
