package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	toxiproxy "github.com/Shopify/toxiproxy/client"
	"go.uber.org/zap"
)

var rpcGatewayConfig = `
metrics:
  port: "9090" # port for prometheus metrics, served on /metrics and /

proxy:
  port: "3000" # port for RPC gateway
  upstreamTimeout: "200m" # when is a request considered timed out
  allowedNumberOfRetriesPerTarget: 2 # The number of retries within the same RPC target for a single request
  retryDelay: "10ms" # delay between retries
  allowedNumberOfFailovers: 1 # The total number of failovers (switching to the next healthy RPC target)

healthChecks:
  interval: "1s" # how often to do healthchecks
  timeout: "1s" # when should the timeout occur and considered unhealthy
  failureThreshold: 2 # how many failed checks until marked as unhealthy
  successThreshold: 1 # how many successes to be marked as healthy again

targets:
  - name: "ToxicCloudflare"
    connection:
      http:
        url: "{{.UrlOne}}"
        compression: false
      ws:
        url: ""
  - name: "CloudflareTwo"
    connection:
      http:
        url: "{{.UrlTwo}}"
        compression: false
      ws:
        url: ""
`

type TestUrl struct {
	UrlOne string
	UrlTwo string
}

func TestSetupRpcGateway(t *testing.T) {
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
	tu := TestUrl{"http://0.0.0.0:9991", "https://cloudflare-eth.com/"}
	tmpl, err := template.New("test").Parse(rpcGatewayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpl.Execute(&tpl, tu); err != nil {
		t.Fatal(err)
	}
	configString := tpl.String()

	fmt.Println(configString)
	config, err := NewRpcGatewayFromConfigString(configString)
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewRpcGateway(*config)
	go gateway.Start(context.TODO())
	gs := httptest.NewServer(gateway)
	gsClient := gs.Client()
	// We limit the connection pool to have a single sourceIP on localhost
	gsClient.Transport = &http.Transport{
		MaxIdleConns:    1,
		MaxConnsPerHost: 1,
	}

	fmt.Println("gateway serving from: ", gs.URL)

	res, err := gsClient.Get(gs.URL)
	if err != nil {
		t.Fatalf("gateway failed to handle the first failover with err: %s", err)
	}

	bodyContent, _ := ioutil.ReadAll(res.Body)
	fmt.Println(string(bodyContent))

	err = gateway.Stop(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
}
