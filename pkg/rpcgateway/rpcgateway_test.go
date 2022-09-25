package rpcgateway

import (
	"bytes"
	"compress/gzip"
	"context"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/0xProject/rpc-gateway/pkg/proxy"
	toxiproxy "github.com/Shopify/toxiproxy/client"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func createConfig() proxy.Config {
	return proxy.Config{
		Proxy: proxy.ProxyConfig{
			AllowedNumberOfRetriesPerTarget: 3,
			RetryDelay:                      0,
			UpstreamTimeout:                 0,
		},
		HealthChecks: proxy.HealthCheckConfig{
			Interval:                      0,
			Timeout:                       0,
			FailureThreshold:              0,
			SuccessThreshold:              0,
			RollingWindowSize:             100,
			RollingWindowFailureThreshold: 0.9,
		},
		Targets: []proxy.TargetConfig{},
	}
}

var rpcGatewayConfig = `
metrics:
  port: 9090 # port for prometheus metrics, served on /metrics and /

proxy:
  port: 3000 # port for RPC gateway
  upstreamTimeout: "200m" # when is a request considered timed out
  allowedNumberOfRetriesPerTarget: 2 # The number of retries within the same RPC target for a single request
  retryDelay: "10ms" # delay between retries

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

	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xd8d7df"}`))

		}))
	defer ts.Close()

	// Toxic Proxy setup
	toxiClient := toxiproxy.NewClient("localhost:8474")
	err := toxiClient.ResetState()
	assert.Nil(t, err)

	proxy, err := toxiClient.CreateProxy("cloudflare", "0.0.0.0:9991", ts.URL[7:])
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
	tu := TestURL{"http://0.0.0.0:9991", "https://rpc.ankr.com/eth"}
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

	t.Logf("gateway serving from: %s", gs.URL)

	config, err := NewRPCGatewayFromConfigBytes([]byte(configString))
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewRPCGateway(*config)
	go gateway.Start(context.TODO())

	rec := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/", bytes.NewBufferString(rpcRequestBody))

	assert.Nil(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(rpcRequestBody))

	res, err := gsClient.Do(req)
	assert.Nil(t, err)

	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	bodyContent, _ := io.ReadAll(res.Body)

	t.Log("Response from RPC gateway:")
	t.Logf("%s", bodyContent)

	err = gateway.Stop(context.TODO())
	assert.Nil(t, err)

	c := gateway.instance.NewContext(req, rec)

	failover := echo.WrapHandler(gateway.httpFailoverProxy)
	failover(c)

	assert.Equal(t, http.StatusOK, rec.Code, "failed to handle the first failover")

	if err = gateway.Stop(context.TODO()); err != nil {
		t.Fatal(err)
	}
}

func TestHttpFailoverProxyDecompressRequest(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var receivedBody, receivedHeaderContentEncoding, receivedHeaderContentLength string

	fakeRPC1Server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			receivedHeaderContentEncoding = r.Header.Get("Content-Encoding")
			receivedHeaderContentLength = r.Header.Get("Content-Length")
			body, _ := io.ReadAll(r.Body)
			receivedBody = string(body)
			w.Write([]byte("OK"))
		}))
	defer fakeRPC1Server.Close()

	rpcGatewayConfig := createConfig()
	rpcGatewayConfig.Targets = []proxy.TargetConfig{
		{
			Name: "Server1",
			Connection: proxy.TargetConfigConnection{
				HTTP: proxy.TargetConnectionHTTP{
					URL: fakeRPC1Server.URL,
				},
			},
		},
	}

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
	gateway := NewRPCGateway(rpcGatewayConfig)

	ctx := gateway.instance.NewContext(req, rr)
	failover := echo.WrapHandler(gateway.httpFailoverProxy)
	failover(c)

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
