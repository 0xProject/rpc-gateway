package proxy

import "sync"

// This code is a modified version of
// https://github.com/smallnest/weighted/blob/master/smooth_weighted.go
//

// weightedRoundRobinItem is a wrapped weighted item.
//

type weightedRoundRobinItem struct {
	Item            interface{}
	Weight          int
	CurrentWeight   int
	EffectiveWeight int
}

/*
	WeightedRoundRobin (Smooth Weighted) is a struct that contains weighted items
	and provides methods to select a weighted item.  It is used for the smooth
	weighted round-robin balancing algorithm. This algorithm is implemented in
	Nginx: https://github.com/phusion/nginx/commit/27e94984486058d73157038f7950a0a36ecc6e35.

	Algorithm is as follows: on each peer selection we increase current_weight of
	each eligible peer by its weight, select peer with greatest current_weight
	and reduce its current_weight by total number of weight points distributed
	among peers.

	In case of { 5, 1, 1 } weights this gives the following sequence of
	current_weight's: (a, a, b, a, c, a, a)
*/

type WeightedRoundRobin struct {
	mu    sync.Mutex
	items []*weightedRoundRobinItem
	size  int
}

func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{}
}

// Add a weighted server.
func (wrr *WeightedRoundRobin) Add(item interface{}, weight int) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	wrr.items = append(
		wrr.items,
		&weightedRoundRobinItem{
			Item:            item,
			Weight:          weight,
			EffectiveWeight: weight},
	)

	wrr.size++
}

// RemoveAll removes all weighted items.
func (wrr *WeightedRoundRobin) RemoveAll() {
	wrr.items = wrr.items[:0]
	wrr.size = 0
}

// Reset resets all current weights.
func (wrr *WeightedRoundRobin) Reset() {
	for _, s := range wrr.items {
		s.EffectiveWeight = s.Weight
		s.CurrentWeight = 0
	}
}

// All returns all items.
func (wrr *WeightedRoundRobin) All() map[interface{}]int {
	items := make(map[interface{}]int)

	for _, i := range wrr.items {
		items[i.Item] = i.Weight
	}

	return items
}

// Next returns next selected server.
func (wrr *WeightedRoundRobin) Next() interface{} {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	i := wrr.next()

	if i == nil {
		return nil
	}

	return i.Item
}

// Empty returns true if number of items is equal to zero.:w

func (wrr *WeightedRoundRobin) Empty() bool {
	return wrr.size == 0
}

// next returns next selected weighted object.
func (wrr *WeightedRoundRobin) next() *weightedRoundRobinItem {
	if wrr.Empty() {
		return nil
	}

	if wrr.size == 1 {
		return wrr.items[0]
	}

	return wrr.doSmoothWeight()
}

// https://github.com/phusion/nginx/commit/27e94984486058d73157038f7950a0a36ecc6e35

func (wrr *WeightedRoundRobin) doSmoothWeight() *weightedRoundRobinItem {
	var total int
	var best *weightedRoundRobinItem

	for idx := 0; idx < wrr.size; idx++ {
		w := wrr.items[idx]

		if w == nil {
			continue
		}

		w.CurrentWeight += w.EffectiveWeight
		total += w.EffectiveWeight

		if w.EffectiveWeight < w.Weight {
			w.EffectiveWeight++
		}

		if best == nil || w.CurrentWeight > best.CurrentWeight {
			best = w
		}
	}

	if best == nil {
		return nil
	}

	best.CurrentWeight -= total

	return best
}
