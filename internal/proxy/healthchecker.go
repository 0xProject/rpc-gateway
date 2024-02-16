package proxy

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	MetricBlockNumber int = iota
	MetricGasLimit
)

const (
	userAgent = "rpc-gateway-health-check"
)

type Healthchecker interface {
	Start(ctx context.Context)
	Stop(ctx context.Context) error
	IsHealthy() bool
	BlockNumber() uint64
	Taint()
	RemoveTaint()
	IsTainted() bool
	Name() string
	SetMetric(int, interface{})
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

const (
	// Initially we wait for 30s then remove the taint.
	initialTaintWaitTime = time.Second * 30
	// We do exponential backoff taint removal but the wait time won't be more than 10 minutes.
	maxTaintWaitTime = time.Minute * 10
	// Reset taint wait time (to `initialTaintWaitTime`) if it's been 5 minutes since the last taint removal.
	resetTaintWaitTimeAfterDuration = time.Minute * 5
)

type RPCHealthchecker struct {
	client     *rpc.Client
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
	// The time when the last taint removal happened
	lastTaintRemoval time.Time
	// The current wait time for the taint removal
	currentTaintWaitTime time.Duration

	// is the ethereum RPC node healthy according to the RPCHealthchecker
	isHealthy bool

	// health check ticker
	ticker *time.Ticker
	mu     sync.RWMutex

	// metrics
	metricRPCProviderBlockNumber *prometheus.GaugeVec
	metricRPCProviderGasLimit    *prometheus.GaugeVec
}

func NewHealthchecker(config RPCHealthcheckerConfig) (Healthchecker, error) {
	client, err := rpc.Dial(config.URL)
	if err != nil {
		return nil, err
	}

	client.SetHeader("User-Agent", userAgent)

	healthchecker := &RPCHealthchecker{
		client:               client,
		httpClient:           &http.Client{},
		config:               config,
		isHealthy:            true,
		currentTaintWaitTime: initialTaintWaitTime,
	}

	return healthchecker, nil
}

func (h *RPCHealthchecker) Name() string {
	return h.config.Name
}

func (h *RPCHealthchecker) SetMetric(i int, metric interface{}) {
	switch i {
	case MetricBlockNumber:
		h.metricRPCProviderBlockNumber = metric.(*prometheus.GaugeVec)
	case MetricGasLimit:
		h.metricRPCProviderGasLimit = metric.(*prometheus.GaugeVec)
	default:
		zap.L().Warn("invalid metric type, ignoring.")
	}
}

func (h *RPCHealthchecker) checkBlockNumber(ctx context.Context) (uint64, error) {
	// First we check the block number reported by the node. This is later
	// used to evaluate a single RPC node against others
	var blockNumber hexutil.Uint64

	err := h.client.CallContext(ctx, &blockNumber, "eth_blockNumber")
	if err != nil {
		zap.L().Warn("error fetching the block number", zap.Error(err), zap.String("name", h.config.Name))
		return 0, err
	}
	if h.metricRPCProviderBlockNumber != nil {
		h.metricRPCProviderBlockNumber.WithLabelValues(h.config.Name).Set(float64(blockNumber))
	}
	zap.L().Debug("fetched block", zap.Uint64("blockNumber", uint64(blockNumber)), zap.String("rpcProvider", h.config.Name))

	return uint64(blockNumber), nil
}

// checkGasLimit performs an `eth_call` with a GasLeft.sol contract call. We also
// want to perform an eth_call to make sure eth_call requests are also succeding
// as blockNumber can be either cached or routed to a different service on the
// RPC provider's side.
func (h *RPCHealthchecker) checkGasLimit(ctx context.Context) (uint64, error) {
	gasLimit, err := performGasLeftCall(ctx, h.httpClient, h.config.URL)
	if h.metricRPCProviderGasLimit != nil {
		h.metricRPCProviderGasLimit.WithLabelValues(h.config.Name).Set(float64(gasLimit))
	}
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

	// TODO
	//
	// This should be moved to a different place, because it does not do a
	// health checking but it provides additional context.

	blockNumber, err := h.checkBlockNumber(ctx)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.blockNumber = blockNumber
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

func (h *RPCHealthchecker) Stop(_ context.Context) error {
	// TODO: Additional cleanups?
	return nil
}

func (h *RPCHealthchecker) IsHealthy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isTainted {
		// If the healthchecker is tainted, we always return unhealthy
		return false
	}

	return h.isHealthy
}

func (h *RPCHealthchecker) BlockNumber() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.blockNumber
}

func (h *RPCHealthchecker) IsTainted() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isTainted
}

func (h *RPCHealthchecker) Taint() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.isTainted {
		return
	}
	h.isTainted = true
	// Increase the wait time (exponentially) for taint removal if the RPC was tainted
	// within resetTaintWaitTimeAfterDuration since the last taint removal
	if time.Since(h.lastTaintRemoval) <= resetTaintWaitTimeAfterDuration {
		h.currentTaintWaitTime *= 2
		if h.currentTaintWaitTime > maxTaintWaitTime {
			h.currentTaintWaitTime = maxTaintWaitTime
		}
	} else {
		h.currentTaintWaitTime = initialTaintWaitTime
	}
	zap.L().Info("RPC Tainted", zap.String("name", h.config.Name), zap.Int64("taintWaitTime", int64(h.currentTaintWaitTime)))
	go func() {
		<-time.After(h.currentTaintWaitTime)
		h.RemoveTaint()
	}()
}

func (h *RPCHealthchecker) RemoveTaint() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isTainted = false
	h.lastTaintRemoval = time.Now()
	zap.L().Info("RPC Taint Removed", zap.String("name", h.config.Name))
}
