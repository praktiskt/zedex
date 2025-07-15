package utils

import (
	"sync"

	"golang.org/x/exp/constraints"
)

type number interface {
	constraints.Integer | constraints.Float
}

type ConcurrentCounter[T number] struct {
	count T
	mtx   sync.Mutex
}

func NewConcurrentCounter[T number]() ConcurrentCounter[T] {
	return ConcurrentCounter[T]{}
}

func (c *ConcurrentCounter[T]) Add(n T) *ConcurrentCounter[T] {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.count += n
	return c
}

func (c *ConcurrentCounter[T]) Increment() *ConcurrentCounter[T] {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.count = c.count + 1
	return c
}

func (c *ConcurrentCounter[T]) Decrement() *ConcurrentCounter[T] {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.count = c.count - 1
	return c
}

func (c *ConcurrentCounter[T]) Reset() *ConcurrentCounter[T] {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.count = 0
	return c
}

func (c *ConcurrentCounter[T]) Value() T {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	v := c.count
	return v
}
