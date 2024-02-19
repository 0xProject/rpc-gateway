package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	userAgent = "rpc-gateway-health-check"
)

type HealthCheckerConfig struct {
	URL    string
	Name   string // identifier imported from RPC gateway config
	Logger *slog.Logger

	// How often to check health.
	Interval time.Duration `yaml:"healthcheckInterval"`

	// How long to wait for responses before failing
	Timeout time.Duration `yaml:"healthcheckTimeout"`

	// Try FailureThreshold times before marking as unhealthy
	FailureThreshold uint `yaml:"healthcheckInterval"`

	// Minimum consecutive successes required to mark as healthy
	SuccessThreshold uint `yaml:"healthcheckInterval"`
}

type HealthChecker struct {
	client     *rpc.Client
	httpClient *http.Client
	config     HealthCheckerConfig
	logger     *slog.Logger

	// latest known blockNumber from the RPC.
	blockNumber uint64
	// gasLimit received from the GasLeft.sol contract call.
	gasLimit uint64

	// is the ethereum RPC node healthy according to the RPCHealthchecker
	isHealthy bool

	mu sync.RWMutex
}

func NewHealthChecker(config HealthCheckerConfig) (*HealthChecker, error) {
	client, err := rpc.Dial(config.URL)
	if err != nil {
		return nil, err
	}

	client.SetHeader("User-Agent", userAgent)

	healthchecker := &HealthChecker{
		logger:     config.Logger.With("nodeprovider", config.Name),
		client:     client,
		httpClient: &http.Client{},
		config:     config,
		isHealthy:  true,
	}

	return healthchecker, nil
}

func (h *HealthChecker) Name() string {
	return h.config.Name
}

func (h *HealthChecker) checkBlockNumber(c context.Context) (uint64, error) {
	// First we check the block number reported by the node. This is later
	// used to evaluate a single RPC node against others
	var blockNumber hexutil.Uint64

	err := h.client.CallContext(c, &blockNumber, "eth_blockNumber")
	if err != nil {
		h.logger.Error("could not fetch block number", "error", err)

		return 0, err
	}
	h.logger.Debug("fetch block number completed", "blockNumber", uint64(blockNumber))

	return uint64(blockNumber), nil
}

// checkGasLimit performs an `eth_call` with a GasLeft.sol contract call. We also
// want to perform an eth_call to make sure eth_call requests are also succeding
// as blockNumber can be either cached or routed to a different service on the
// RPC provider's side.
func (h *HealthChecker) checkGasLimit(c context.Context) (uint64, error) {
	gasLimit, err := performGasLeftCall(c, h.httpClient, h.config.URL)
	if err != nil {
		h.logger.Error("could not fetch gas limit", "error", err)

		return gasLimit, err
	}
	h.logger.Debug("fetch gas limit completed", "gasLimit", gasLimit)

	return gasLimit, nil
}

// CheckAndSetHealth makes the following calls
// - `eth_blockNumber` - to get the latest block reported by the node
// - `eth_call` - to get the gas limit
// And sets the health status based on the responses.
func (h *HealthChecker) CheckAndSetHealth() {
	go h.checkAndSetBlockNumberHealth()
	go h.checkAndSetGasLeftHealth()
}

func (h *HealthChecker) checkAndSetBlockNumberHealth() {
	c, cancel := context.WithTimeout(context.Background(), h.config.Timeout)
	defer cancel()

	// TODO
	//
	// This should be moved to a different place, because it does not do a
	// health checking but it provides additional context.

	blockNumber, err := h.checkBlockNumber(c)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.blockNumber = blockNumber
}

func (h *HealthChecker) checkAndSetGasLeftHealth() {
	c, cancel := context.WithTimeout(context.Background(), h.config.Timeout)
	defer cancel()

	gasLimit, err := h.checkGasLimit(c)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.isHealthy = false

		return
	}
	h.gasLimit = gasLimit
	h.isHealthy = true
}

func (h *HealthChecker) Start(c context.Context) {
	h.CheckAndSetHealth()

	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.Done():
			return
		case <-ticker.C:
			h.CheckAndSetHealth()
		}
	}
}

func (h *HealthChecker) Stop(_ context.Context) error {
	// TODO: Additional cleanups?
	return nil
}

func (h *HealthChecker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.isHealthy
}

func (h *HealthChecker) BlockNumber() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.blockNumber
}

func (h *HealthChecker) GasLimit() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.gasLimit
}
