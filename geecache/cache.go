package geecache

import (
	"geecache/lru"
	"sync"
	"time"
)

// 基本的缓存操作
type BaseCache interface {
	add(key string, value ByteView)
	get(key string) (value ByteView, ok bool)
}

type cache struct {
	mu         sync.Mutex
	lru        *lru.Cache
	cacheBytes int64         // lru的maxbytes
	ttl        time.Duration // lru 的defaultttl
}

// 向缓存添加数据
func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 延迟初始化
	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil,c.ttl)
	}
	c.lru.Add(key, value,c.ttl)
}

func (c *cache) get(key string) (value ByteView, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return
	}

	if v, ok := c.lru.Get(key); ok {
		return v.(ByteView), ok
	}

	return
}
