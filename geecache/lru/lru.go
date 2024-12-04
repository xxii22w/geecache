package lru

import (
	"container/list"
	"log"
	"math/rand"
	"time"
)

// LRU 缓存淘汰算法
type Cache struct {
	maxBytes  int64 // 最大存储容量
	nbytes    int64 // 已占用的容量
	ll        *list.List
	cache     map[string]*list.Element
	OnEvicted func(key string, value Value) // 可选，在entry被移除的时候执⾏
	defaultTTL time.Duration
}

type entry struct {
	key   string
	value Value
	expire time.Time	// 节点的过期时间
}

type Value interface {
	Len() int
}

// 生成缓存
func New(maxbytes int64, onEvicted func(string, Value),defaultTTL time.Duration) *Cache {
	return &Cache{
		maxBytes:  maxbytes,
		ll:        list.New(),
		cache:     make(map[string]*list.Element),
		OnEvicted: onEvicted,
		defaultTTL: defaultTTL,
	}
}

// 根据键值缓存中的值，存在就把节点移动到链表最前面(最近使用),如果不存在或键值过期,返回0或false
func (c *Cache) Get(key string) (value Value, ok bool) {
	if ele, ok := c.cache[key]; ok {
		kv := ele.Value.(*entry)
		if kv.expire.Before(time.Now()) {
			c.RemoveElement(ele)
			log.Printf("The LRUcache key—%s has expired", key)
			return nil, false
		}
		c.ll.MoveToFront(ele)
		return kv.value, true
	}
	return
}

// 找到最久未使用且已过期的缓存项，然后将其从缓存中移除。
func (c *Cache) RemoveOldest() {
	for e := c.ll.Back(); e != nil; e = e.Prev() {
		kv := e.Value.(*entry)
		if kv.expire.Before(time.Now()) {
			c.RemoveElement(e)
			break
		}
	}
}
	

// 向缓存中添加新的键值对,如果键存在，就更新，并把节点移动到连接前面
// 如果键不存在,则链表头部插入新的节点，并更新已占有的容器
// 如果添加新的键值对后超出了最大存储容量，则会连续移除最久未使用的记录，直到满足容量要求
func (c *Cache) Add(key string, value Value,ttl time.Duration) {
	expireTime := time.Now().Add(ttl + time.Duration(rand.Intn(60))*time.Second)
	if ele, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		kv := ele.Value.(*entry)
		c.nbytes += int64(value.Len()) - int64(kv.value.Len())
		kv.value = value
		// 更新过期时间时，判断是否应该保留原本的过期时间
		if kv.expire.Before(expireTime) {
			kv.expire = expireTime
		}
	} else {
		ele = c.ll.PushFront(&entry{key: key, value: value, expire: expireTime})
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

// RemoveElement 函数用于删除某个节点
func (c *Cache) RemoveElement(e *list.Element) {
	c.ll.Remove(e)
	kv := e.Value.(*entry)
	delete(c.cache, kv.key)                                //删除key-节点这对映射
	c.nbytes -= int64(len(kv.key)) + int64(kv.value.Len()) //重新计算已用容量
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value) //调用对应的回调函数
	}
}