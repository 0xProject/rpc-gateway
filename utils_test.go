package main

import (
	"testing"
)

func TestRollingAverage(t *testing.T) {
	r := NewRollingWindow(10)

	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(0)

	// [1, 1, 1, 1, 0] - 80%
	if r.Avg() != 0.8 {
		t.Fatal("expected the average to be 80%")
	}

	if r.Sum() != 4 {
		t.Fatal("expected the sum to be equal 4")
	}

}

func TestRollingWindowReset(t *testing.T) {
	r := NewRollingWindow(10)

	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)
	r.Observe(0)

	r.Reset()
	if r.Sum() != 0 {
		t.Fatal("expected the sum to be equal 0")
	}
}


func TestRollingWindowHasEnoughObservations(t *testing.T) {
	r := NewRollingWindow(5)

	r.Observe(0)
	r.Observe(1)
	r.Observe(1)
	r.Observe(1)

	if r.HasEnoughObservations() {
		t.Fatal("expected HasEnoughObservations to return false")
	}

	r.Observe(1)

	if !r.HasEnoughObservations() {
		t.Fatal("expected HasEnoughObservations to return true")
	}
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
