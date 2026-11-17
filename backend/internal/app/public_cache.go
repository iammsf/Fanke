package app

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const publicCacheTTL = 30 * time.Second

type publicCacheKey uint8

const (
	cachePublicSettings publicCacheKey = iota
	cacheHomeSort
	cacheDefaultCovers
	cachePublicPlugins
	cacheEnabledExtensions
)

type publicCacheEntry struct {
	data    []byte
	expires time.Time
}

type publicCacheFlight struct {
	done chan struct{}
	data []byte
	err  error
}

// publicCache 仅保存五个 App 公开配置入口的 JSON 快照，零值可用。
// clear 换代后，旧加载既不能回填，也不能被新请求合并。
type publicCache struct {
	mu         sync.Mutex
	generation uint64
	entries    map[publicCacheKey]publicCacheEntry
	flights    map[publicCacheKey]*publicCacheFlight
}

func (c *publicCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.entries = nil
	c.flights = nil
}

// gen 返回当前换代号。挂在它上面的旁路缓存(如 s3Settings)不需要各自的失效路径:
// 任何一处 clear() 都会让它们一起失效。
func (c *publicCache) gen() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *publicCache) get(ctx context.Context, key publicCacheKey, load func() ([]byte, error)) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		generation := c.generation
		if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expires) {
			data := append([]byte(nil), entry.data...)
			c.mu.Unlock()
			return data, nil
		}
		if flight := c.flights[key]; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
			}
			c.mu.Lock()
			current := generation == c.generation
			c.mu.Unlock()
			if !current {
				continue
			}
			return append([]byte(nil), flight.data...), flight.err
		}
		flight := &publicCacheFlight{done: make(chan struct{})}
		if c.flights == nil {
			c.flights = make(map[publicCacheKey]*publicCacheFlight)
		}
		c.flights[key] = flight
		c.mu.Unlock()

		data, err := load()
		c.mu.Lock()
		current := generation == c.generation
		flight.data, flight.err = append([]byte(nil), data...), err
		if current {
			delete(c.flights, key)
			if err == nil {
				if c.entries == nil {
					c.entries = make(map[publicCacheKey]publicCacheEntry)
				}
				c.entries[key] = publicCacheEntry{data: flight.data, expires: time.Now().Add(publicCacheTTL)}
			}
		}
		close(flight.done)
		c.mu.Unlock()
		if !current {
			continue
		}
		return append([]byte(nil), flight.data...), err
	}
}

func cachedPublicJSON[T any](ctx context.Context, c *publicCache, key publicCacheKey, load func() (T, error)) (T, error) {
	var out T
	data, err := c.get(ctx, key, func() ([]byte, error) {
		value, err := load()
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}
