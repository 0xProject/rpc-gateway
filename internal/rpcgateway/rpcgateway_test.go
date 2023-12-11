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
	"github.com/caitlinelfring/go-env-default"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

var rpcGatewayConfig = `
metrics:
  port: 9090 # port for prometheus metrics, served on /metrics and /

proxy:
  port: 3000 # port for RPC gateway
  upstreamTimeout: "200m" # when is a request considered timed out

healthChecks:
  interval: "1s" # how often to do healthchecks
  timeout: "1s" # when should the timeout occur and considered unhealthy
  failureThreshold: 2 # how many failed checks until marked as unhealthy
  successThreshold: 1 # how many successes to be marked as healthy again

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
	err := toxiClient.ResetState()
	assert.Nil(t, err)

	proxy, err := toxiClient.CreateProxy("primary", "0.0.0.0:9991", ts.URL[7:])
	assert.Nil(t, err)

	_, err = proxy.AddToxic("latency_down", "latency", "downstream", 1.0, toxiproxy.Attributes{
		"latency": 100000,
	})
	assert.Nil(t, err)

	defer func() {
		_ = toxiClient.ResetState()
		_ = proxy.Delete()
	}()

	// config string
	var tpl bytes.Buffer
	tu := TestURL{"http://0.0.0.0:9991", env.GetDefault("RPC_GATEWAY_NODE_URL_1", "https://cloudflare-eth.com")}
	tmpl, err := template.New("test").Parse(rpcGatewayConfig)
	assert.Nil(t, err)

	err = tmpl.Execute(&tpl, tu)
	assert.Nil(t, err)

	configString := tpl.String()

	t.Log(configString)

	config, err := NewRPCGatewayFromConfigString(configString)
	assert.Nil(t, err)

	gateway := NewRPCGateway(*config)
	go gateway.Start(context.TODO())
	gs := httptest.NewServer(gateway)

	gsClient := gs.Client()
	// We limit the connection pool to have a single sourceIP on localhost
	gsClient.Transport = &http.Transport{
		MaxIdleConns:    1,
		MaxConnsPerHost: 1,
	}

	req, _ := http.NewRequest("POST", gs.URL, bytes.NewBufferString(rpcRequestBody))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(rpcRequestBody))

	res, err := gsClient.Do(req)
	assert.Nil(t, err)

	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	_, err = io.ReadAll(res.Body)
	assert.Nil(t, err)

	err = gateway.Stop(context.TODO())
	assert.Nil(t, err)
}
