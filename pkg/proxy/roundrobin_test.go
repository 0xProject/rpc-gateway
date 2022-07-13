package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWeightedRoundRobin(t *testing.T) {
	tests := []struct {
		items    [][2]int
		expected []interface{}
	}{
		{
			// Equal traffic distribution.
			//
			items: [][2]int{
				{
					100,
					100,
				},
				{
					101,
					100,
				},
			},
			expected: []interface{}{
				100,
				101,
			},
		},
		{
			// Do not pick up a node with a weight of 0.
			//
			items: [][2]int{
				{
					100,
					100,
				},
				{
					101,
					100,
				},
				{
					102,
					0,
				},
			},
			expected: []interface{}{
				100,
				101,
				100,
			},
		},
	}

	for _, test := range tests {
		wrr := NewWeightedRoundRobin()

		// Building out a backend.
		//
		for _, item := range test.items {
			wrr.Add(item[0], item[1])
		}

		var output []interface{}

		for i := 0; i < wrr.size; i++ {
			output = append(output, wrr.Next())
		}

		assert.Equal(t, test.expected, output)
	}
}
