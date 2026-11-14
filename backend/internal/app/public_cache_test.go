package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicCacheTTLAndSnapshots(t *testing.T) {
	var c publicCache
	calls := 0
	load := func() (map[string][]string, error) {
		calls++
		return map[string][]string{"names": {"original"}}, nil
	}
	before := time.Now()
	first, err := cachedPublicJSON(context.Background(), &c, cachePublicSettings, load)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	expires := c.entries[cachePublicSettings].expires
	c.mu.Unlock()
	if expires.Before(before.Add(30*time.Second)) || expires.After(time.Now().Add(30*time.Second)) {
		t.Fatalf("unexpected TTL: %v", expires)
	}
	first["names"][0] = "changed"
	second, err := cachedPublicJSON(context.Background(), &c, cachePublicSettings, load)
	if err != nil || second["names"][0] != "original" || calls != 1 {
		t.Fatalf("snapshot/hit failed: %v, %v, calls=%d", second, err, calls)
	}
	c.mu.Lock()
	entry := c.entries[cachePublicSettings]
	entry.expires = time.Now().Add(-time.Second)
	c.entries[cachePublicSettings] = entry
	c.mu.Unlock()
	if _, err := cachedPublicJSON(context.Background(), &c, cachePublicSettings, load); err != nil || calls != 2 {
		t.Fatalf("expired entry not reloaded: err=%v calls=%d", err, calls)
	}
}

func TestPublicCacheErrorsNotCached(t *testing.T) {
	var c publicCache
	want := errors.New("load failed")
	for i := 0; i < 2; i++ {
		_, err := c.get(context.Background(), cacheHomeSort, func() ([]byte, error) {
			return nil, want
		})
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
		if len(c.entries) != 0 || len(c.flights) != 0 {
			t.Fatal("failed load retained")
		}
	}
	_, err := cachedPublicJSON(context.Background(), &c, cacheHomeSort, func() (any, error) {
		return make(chan int), nil
	})
	if err == nil || len(c.entries) != 0 {
		t.Fatal("serialization error cached")
	}
	data, err := c.get(context.Background(), cacheHomeSort, func() ([]byte, error) { return []byte(`"ok"`), nil })
	if err != nil || string(data) != `"ok"` {
		t.Fatalf("recovery failed: %s %v", data, err)
	}
	data[0] = 'x'
	data, _ = c.get(context.Background(), cacheHomeSort, func() ([]byte, error) {
		t.Fatal("expected hit")
		return nil, nil
	})
	if string(data) != `"ok"` {
		t.Fatal("byte snapshot shared")
	}
}

func TestPublicCacheConcurrentMiss(t *testing.T) {
	var c publicCache
	var calls atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	load := func() ([]byte, error) {
		if calls.Add(1) == 1 {
			close(start)
		}
		<-release
		return []byte(`[]`), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.get(context.Background(), cachePublicPlugins, load)
			if err != nil || string(data) != "[]" {
				t.Errorf("get: %s %v", data, err)
			}
		}()
	}
	<-start
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("misses not merged: %d", calls.Load())
	}
}

func TestPublicCacheInvalidateDuringLoad(t *testing.T) {
	for _, oldError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[oldError], func(t *testing.T) {
			var c publicCache
			started := make(chan struct{})
			release := make(chan struct{})
			result := make(chan string, 1)
			var calls atomic.Int32
			go func() {
				data, err := c.get(context.Background(), cacheEnabledExtensions, func() ([]byte, error) {
					if calls.Add(1) == 1 {
						close(started)
						<-release
						if oldError {
							return nil, errors.New("old load failed")
						}
						return []byte(`"old"`), nil
					}
					return []byte(`"new"`), nil
				})
				if err != nil {
					t.Errorf("old error leaked after invalidation: %v", err)
				}
				result <- string(data)
			}()
			<-started
			c.clear()
			// 新请求不能等待旧代加载，即使旧加载仍被阻塞。
			data, err := c.get(context.Background(), cacheEnabledExtensions, func() ([]byte, error) {
				return []byte(`"new"`), nil
			})
			if err != nil || string(data) != `"new"` {
				t.Errorf("new generation: %s %v", data, err)
			}
			close(release)
			if got := <-result; got != `"new"` {
				t.Fatalf("stale in-flight result returned: %s", got)
			}
			data, _ = c.get(context.Background(), cacheEnabledExtensions, func() ([]byte, error) {
				t.Error("fresh entry lost")
				return nil, nil
			})
			if string(data) != `"new"` {
				t.Fatal("stale load refilled cache")
			}
		})
	}
}

func TestPublicCacheWaiterCancellation(t *testing.T) {
	var c publicCache
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.get(context.Background(), cacheDefaultCovers, func() ([]byte, error) {
			close(started)
			<-release
			return []byte(`{}`), nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.get(ctx, cacheDefaultCovers, func() ([]byte, error) {
		t.Error("waiter started another load")
		return nil, nil
	})
	close(release)
	<-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
