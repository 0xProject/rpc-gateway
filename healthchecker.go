package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

type Healthchecker interface {
	Start(ctx context.Context)
	Stop(ctx context.Context) error
	IsHealthy() bool
	BlockNumber() uint64
	SetTaint(bool)
	IsTainted() bool
	Name() string
}

type RPCHealthcheckerConfig struct {
	URL  string
	Name string // identifier imported from RPC gateway config

	// How often to check health.
	Interval time.Duration `yaml:"healthcheckInterval"`

	// How long to wait for responses before failing
	Timeout time.Duration `yaml:"healthcheckTimeout"`

	// Try FailureThreshold times before marking as unhealthy
	FailureThreshold uint `yaml:"healthcheckInterval"`

	// Minimum consecutive successes required to mark as healthy
	SuccessThreshold uint `yaml:"healthcheckInterval"`
}

type RPCHealthchecker struct {
	client     *ethclient.Client
	httpClient *http.Client
	config     RPCHealthcheckerConfig

	// latest known blockNumber from the RPC.
	blockNumber uint64
	// gasLimit received from the GasLeft.sol contract call.
	gasLimit uint64

	// RPCHealthChecker can be tainted by the abstraction on top. Reasons:
	// Forced failover
	// Blocknumber is behind the other
	isTainted bool
	// is the ethereum RPC node healthy according to the RPCHealthchecker
	isHealthy bool

	// health check ticker
	ticker *time.Ticker
	mu     sync.RWMutex
}

func NewHealthchecker(config RPCHealthcheckerConfig) (Healthchecker, error) {
	client, err := ethclient.Dial(config.URL)
	if err != nil {
		return nil, err
	}

	return &RPCHealthchecker{
		client:     client,
		httpClient: &http.Client{},
		config:     config,
		isHealthy:  true,
	}, nil
}

func (h *RPCHealthchecker) Name() string {
	return h.config.Name
}

func (h *RPCHealthchecker) checkBlockNumber(ctx context.Context) (uint64, error) {
	// First we check the block number reported by the node. This is later
	// used to evaluate a single RPC node against others
	start := time.Now()
	blockNumber, err := h.client.BlockNumber(ctx)
	if err != nil {
		zap.L().Warn("error fetching the block number", zap.Error(err), zap.String("name", h.config.Name))
		return 0, err
	}
	duration := time.Since(start)
	healthcheckResponseTimeHistogram.WithLabelValues(h.config.Name, "eth_blockNumber").Observe(duration.Seconds())
	rpcProviderBlockNumber.WithLabelValues(h.config.Name).Set(float64(blockNumber))
	zap.L().Debug("fetched block", zap.Uint64("blockNumber", blockNumber), zap.String("rpcProvider", h.config.Name))

	return blockNumber, nil
}

// checkGasLimit performs an `eth_call` with a GasLeft.sol contract call. We also
// want to perform an eth_call to make sure eth_call requests are also succeding
// as blockNumber can be either cached or routed to a different service on the
// RPC provider's side.
func (h *RPCHealthchecker) checkGasLimit(ctx context.Context) (uint64, error) {
	gasLimit, err := performGasLeftCall(ctx, h.httpClient, h.config.URL)
	rpcProviderGasLimit.WithLabelValues(h.config.Name).Set(float64(gasLimit))
	zap.L().Debug("fetched gas limit", zap.Uint64("gasLimit", gasLimit), zap.String("rpcProvider", h.config.Name))
	if err != nil {
		zap.L().Warn("failed fetching the gas limit", zap.Error(err), zap.String("rpcProvider", h.config.Name))
		return gasLimit, err
	}

	return gasLimit, nil
}

// CheckAndSetHealth makes the following calls
// - `eth_blockNumber` - to get the latest block reported by the node
// - `eth_call` - to get the gas limit
// And sets the health status based on the responses.
func (h *RPCHealthchecker) CheckAndSetHealth() {
	go h.checkAndSetBlockNumberHealth()
	go h.checkAndSetGasLeftHealth()
}

func (h *RPCHealthchecker) checkAndSetBlockNumberHealth() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, h.config.Timeout)
	defer cancel()

	blockNumber, err := h.checkBlockNumber(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.isHealthy = false
		return
	}
	h.blockNumber = blockNumber
	h.isHealthy = true
}

func (h *RPCHealthchecker) checkAndSetGasLeftHealth() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, h.config.Timeout)
	defer cancel()

	gasLimit, err := h.checkGasLimit(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.isHealthy = false
		return
	}
	h.gasLimit = gasLimit
	h.isHealthy = true
}

func (h *RPCHealthchecker) Start(ctx context.Context) {
	h.CheckAndSetHealth()
	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()
	h.ticker = ticker
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.CheckAndSetHealth()
		}
	}
}

func (h *RPCHealthchecker) Stop(ctx context.Context) error {
	// TODO: Additional cleanups?
	return nil
}

func (h *RPCHealthchecker) IsHealthy() bool {
	if h.isTainted {
		// If the healthchecker is tainted, we always return unhealthy
		return false
	} else {
		return h.isHealthy
	}
}

func (h *RPCHealthchecker) BlockNumber() uint64 {
	return h.blockNumber
}

func (h *RPCHealthchecker) IsTainted() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isTainted
}

func (h *RPCHealthchecker) SetTaint(tainted bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isTainted = tainted
}
