package utils

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentMap(t *testing.T) {
	s := NewConcurrentMap[int, int]()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		s.Set(1, 1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				s.Set(1, 2)
				s.Get(1)
				s.Len()
				s.Get(0)
				s.Set(2, 1)
				s.Get(2)
				s.Keys()
				s.Map()
				s.Values()
				s.Exists(1)
				s.Delete(1)
				s.Set(2, 1)
				s.Pop(1)
			}
		}(i)
	}
	wg.Wait()

	assert.Equal(t, s.Len(), 1)
}
