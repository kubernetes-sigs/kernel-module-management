package cache

import (
	"context"
	"sync"
	"time"
)

//go:generate mockgen -source=cache.go -package=cache -destination=mock_cache.go

type Cache[T comparable] interface {
	DeleteExpired()
	Get(key T) (interface{}, bool)
	Set(key T, value interface{})
	StartCollecting(ctx context.Context, interval time.Duration)
	WaitForTermination()
}

type item struct {
	Object     interface{}
	Expiration time.Time
}

type cache[T comparable] struct {
	mu         sync.RWMutex
	items      map[T]item
	expiration time.Duration
	wg         sync.WaitGroup
}

// New returns a new cache with the requested item expiration duration, which automatically
// removes expired items at the cleanup interval.
func New[T comparable](expiration time.Duration) *cache[T] {
	return &cache[T]{
		items:      make(map[T]item),
		expiration: expiration,
	}
}

// Get fetches an item from the cache. It returns the item or nil and a bool indicating
// whether the key was found.
func (c *cache[T]) Get(k T) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, found := c.items[k]
	if !found || time.Now().After(item.Expiration) {
		return nil, false
	}
	return item.Object, true
}

// Set adds an item to the cache, replacing any existing item.
func (c *cache[T]) Set(key T, value interface{}) {
	expiration := time.Now().Add(c.expiration)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = item{
		Object:     value,
		Expiration: expiration,
	}
}

// StartCollecting spawns a goroutine to remove expired items from the cache at the given interval.
// It returns when its context is cancelled.
func (c *cache[T]) StartCollecting(ctx context.Context, interval time.Duration) {
	c.wg.Add(1)
	go func(ctx context.Context, interval time.Duration) {
		defer c.wg.Done()
		t := time.NewTicker(interval)

		for {
			select {
			case <-t.C:
				c.DeleteExpired()
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
	}(ctx, interval)
}

// DeleteExpired deletes all expired items from the cache.
func (c *cache[T]) DeleteExpired() {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.items {
		if now.After(v.Expiration) {
			delete(c.items, k)
		}
	}
}

func (c *cache[T]) WaitForTermination() {
	c.wg.Wait()
}
