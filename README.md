> [!CAUTION]
> The rpc-gateway is in development mode, and you should not consider it
> stable yet.

## RPC Gateway

The rpc-gateway is a failover proxy for node providers. When health checks
fail, the rpc-gateway automatically routes requests to a backup node provider.

```mermaid
sequenceDiagram
Alice->>RPC Gateway: eth_call
loop Healthcheck
    RPC Gateway->>Alchemy: Check health
    RPC Gateway->>Infura: Check health
end
Note right of RPC Gateway: Routes only to healthy targets
loop Configurable Retries
RPC Gateway->>Alchemy: eth_call?
Alchemy-->>RPC Gateway: ERROR
end
Note right of RPC Gateway: RPC Call is rerouted after failing retries
RPC Gateway->>Infura: eth_call?
Infura-->>RPC Gateway: {"result":[...]}
RPC Gateway-->>Alice: {"result":[...]}
```

## Development

Make sure the test pass
```console
go test -v ./...
```

To run the app locally
```console
DEBUG=true go run . --config example_config.yml
```

## Configuration

```yaml
metrics:
  port: "9090" # port for prometheus metrics, served on /metrics and /

proxy:
  port: "3000" # port for RPC gateway
  upstreamTimeout: "1s" # when is a request considered timed out

healthChecks:
  interval: "5s" # how often to do healthchecks
  timeout: "1s" # when should the timeout occur and considered unhealthy
  failureThreshold: 2 # how many failed checks until marked as unhealthy
  successThreshold: 1 # how many successes to be marked as healthy again

targets: # the order here determines the failover order
  - name: "Cloudflare"
    connection:
      http: # ws is supported by default, it will be a sticky connection.
        url: "https://cloudflare-eth.com"
  - name: "Alchemy"
    connection:
      http: # ws is supported by default, it will be a sticky connection.
        url: "https://alchemy.com/rpc/<apikey>"
```
