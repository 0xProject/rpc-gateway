package rollingwindow

import (
	"sync"
)

// RollingWindow is used to compute a rolling average of observations with a
// given windowSize.
// Comparing this dynamic slice to a fixed-length array (without the if):
// goos: linux
// goarch: amd64
// pkg: 0xProject/rpc-gateway
// cpu: AMD Ryzen 9 5950X 16-Core Processor
// BenchmarkRollingAverage-32         	    1226	    919260 ns/op
// BenchmarkFixedRollingAverage-32    	    1196	    875721 ns/op
// 5% perf difference in synthetic benchmark.

type RollingWindow struct {
	size   int
	window []int
	offset int

	mu sync.RWMutex
}

func NewRollingWindow(size int) *RollingWindow {
	return &RollingWindow{
		size:   size,
		window: make([]int, 0, size),
	}
}

func (r *RollingWindow) Window() []int {
	return r.window
}

func (r *RollingWindow) Observe(value int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.window) < r.size {
		r.window = append(r.window, value)
		return
	}

	r.window[r.offset] = value
	r.offset = (r.offset + 1) % r.size
}

func (r *RollingWindow) Sum() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := 0
	for _, v := range r.window {
		result += v
	}

	return result
}

func (r *RollingWindow) Avg() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := 0
	for _, v := range r.window {
		result += v
	}

	return float64(result) / float64(len(r.window))
}

func (r *RollingWindow) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.window = make([]int, 0, r.size)
}

// TODO: can be combined with Avg() to reduce locks.
//
func (r *RollingWindow) HasEnoughObservations() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.window) == 0 {
		return false
	}

	return float64(len(r.window)/r.size) > 0.9 // TODO: parameterize this
}
