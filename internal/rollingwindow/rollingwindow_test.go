package rollingwindow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRollingAverage(t *testing.T) {
	r := NewRollingWindow(10)

	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(0)

	assert.Equal(t, 0.8, r.Avg())
	assert.Equal(t, 4, r.Sum())
}

func TestRollingWindowReset(t *testing.T) {
	r := NewRollingWindow(10)

	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(0)

	r.Reset()

	assert.Zero(t, r.Sum())
}

func TestRollingWindowHasEnoughObservations(t *testing.T) {
	r := NewRollingWindow(5)

	r.Observe(0)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)

	assert.False(t, r.HasEnoughObservations())

	r.Observe(1)

	assert.True(t, r.HasEnoughObservations())
}

func BenchmarkRollingAverageParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r := NewRollingWindow(1000)

			for x := 0; x < 100000; x++ {
				r.Observe(1)
			}

			r.HasEnoughObservations()
			r.Avg()
		}
	})
}

func BenchmarkRollingAverageSerial(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := NewRollingWindow(1000)

		for x := 0; x < 100000; x++ {
			r.Observe(1)
		}

		r.HasEnoughObservations()
		r.Avg()
	}
}
