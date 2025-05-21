package utils

import (
	"sync"

	"golang.org/x/exp/maps"
)

type ConcurrentMap[A comparable, B any] struct {
	m map[A]B
	l sync.Mutex
}

func NewConcurrentMap[A comparable, B any]() ConcurrentMap[A, B] {
	return ConcurrentMap[A, B]{
		m: map[A]B{},
	}
}

func (m *ConcurrentMap[A, B]) Set(k A, v B) {
	m.l.Lock()
	defer m.l.Unlock()
	m.m[k] = v
}

func (m *ConcurrentMap[A, B]) Get(k A) B {
	m.l.Lock()
	defer m.l.Unlock()
	return m.m[k]
}

func (m *ConcurrentMap[A, B]) GetUnsafe(k A) B {
	return m.m[k]
}

func (m *ConcurrentMap[A, B]) SetUnsafe(k A, v B) {
	m.m[k] = v
}

func (m *ConcurrentMap[A, B]) Delete(k A) {
	m.l.Lock()
	defer m.l.Unlock()
	delete(m.m, k)
}

func (m *ConcurrentMap[A, B]) Transaction(k A, f func(m *ConcurrentMap[A, B])) {
	m.l.Lock()
	defer m.l.Unlock()
	f(m)
}

func (m *ConcurrentMap[A, B]) Pop(k A) B {
	m.l.Lock()
	defer m.l.Unlock()
	v := m.m[k]
	delete(m.m, k)
	return v
}

func (m *ConcurrentMap[A, B]) Exists(k A) bool {
	m.l.Lock()
	defer m.l.Unlock()
	_, exists := m.m[k]
	return exists
}

func (m *ConcurrentMap[A, B]) Map() map[A]B {
	return m.m
}

func (m *ConcurrentMap[A, B]) Keys() []A {
	m.l.Lock()
	defer m.l.Unlock()
	return maps.Keys(m.m)
}

func (m *ConcurrentMap[A, B]) Values() []B {
	m.l.Lock()
	defer m.l.Unlock()
	return maps.Values(m.m)
}

func (m *ConcurrentMap[A, B]) Len() int {
	m.l.Lock()
	defer m.l.Unlock()
	return len(m.Map())
}
