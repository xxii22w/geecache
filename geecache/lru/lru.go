package lru

import (
	"container/list"
)

// LRU 缓存淘汰算法
type Cache struct {
	maxBytes  int64 // 缓存容量
	nbytes    int64 // 当前缓存大小
	ll        *list.List
	cache     map[string]*list.Element
	OnEvicted func(key string, value Value) // 可选，在entry被移除的时候执⾏
}

type entry struct {
	key   string
	value Value
}

// Value使用Len来统计需要多少字节
type Value interface {
	Len() int
}

// 生成缓存
func New(maxbytes int64, onEvicted func(string, Value)) *Cache {
	return &Cache{
		maxBytes:  maxbytes,
		ll:        list.New(),
		cache:     make(map[string]*list.Element),
		OnEvicted: onEvicted,
	}
}

// 根据键值缓存中的值，存在就把节点移动到链表最前面(最近使用),如果不存在或键值过期,返回0或false
func (c *Cache) Get(key string) (value Value, ok bool) {
	if ele, ok := c.cache[key]; ok {
		kv := ele.Value.(*entry)
		c.ll.MoveToFront(ele) // 如果存在，则将节点移到队尾 约定 front 为队尾
		return kv.value, true
	}
	return
}

// 找到最久未使用且已过期的缓存项，然后将其从缓存中移除。
func (c *Cache) RemoveOldest() {
	ele := c.ll.Back()
	if ele != nil {
		c.ll.Remove(ele)
		kv := ele.Value.(*entry)
		delete(c.cache, kv.key)
		c.nbytes -= int64(len(kv.key)) + int64(kv.value.Len())
		if c.OnEvicted != nil {
			c.OnEvicted(kv.key, kv.value)
		}
	}

}
	

// 向缓存中添加新的键值对,如果键存在，就更新，并把节点移动到连接前面
// 如果键不存在,则链表头部插入新的节点，并更新已占有的容器
// 如果添加新的键值对后超出了最大存储容量，则会连续移除最久未使用的记录，直到满足容量要求
func (c *Cache) Add(key string, value Value) {
	if ele, ok := c.cache[key]; ok {
		// 如果找得到，就移动到队尾 (更新)
		c.ll.MoveToFront(ele)
		kv := ele.Value.(*entry)
		c.nbytes += int64(value.Len()) - int64(kv.value.Len())
		kv.value = value
	} else {
		// 如果没有，就添加到队尾 (新增)
		ele := c.ll.PushFront(&entry{key, value})
		c.cache[key] = ele
		c.nbytes += int64(len(key)) + int64(value.Len())
	}
	for c.maxBytes != 0 && c.maxBytes < c.nbytes {
		c.RemoveOldest()
	}
}

func (c *Cache) Len() int {
	return c.ll.Len()
}