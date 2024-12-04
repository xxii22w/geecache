package geecache

import (
	"fmt"
	"geecache/proto"
	"geecache/singleflight"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Group 是缓存命名空间 每个group都有一个名字
type Group struct {
	name      string               // 缓存空间的名字
	getter    Getter               // 数据源获取数据
	mainCache BaseCache            // 主缓存,用于存储本地节点作为主节点所拥有的数据
	hotCache  BaseCache            // hotCache 则是为了存储热门数据的缓存
	peers     PeerPicker           // 用于获取远程节点请求客户端
	loader    *singleflight.Group  // 避免被同一个key多次加载造成缓存击穿
	keys      map[string]*KeyStats // 根据键key获取对应key的统计信息
}

type AtomicInt int64 // 封装一个原子类，用于进行原子操作，保证并发安全.

// Add 方法用于对 AtomicInt 中的值进行原子自增
func (i *AtomicInt) Add(n int64) { //原子自增
	atomic.AddInt64((*int64)(i), n)
}

// Get 方法用于获取 AtomicInt 中的值。
func (i *AtomicInt) Get() int64 {
	return atomic.LoadInt64((*int64)(i))
}

type KeyStats struct { //Key的统计信息
	firstGetTime time.Time //第一次请求的时间
	remoteCnt    AtomicInt //请求的次数（利用atomic包封装的原子类）
}

var (
	maxMinuteRemoteQPS = 10                      //最大QPS
	mu                 sync.RWMutex              // 读写锁
	groups             = make(map[string]*Group) // 根据缓存组的名称，获取缓存组
)

// 调用 RegisterPeers 函数，我们可以将实现了 PeerPicker 接口的对象注册到 Group 结构体中
func (g *Group) RegisterPeers(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeerPicker called more than once")
	}
	g.peers = peers
}

// NewGroup create a new instance of Group
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: &cache{cacheBytes: cacheBytes, ttl: time.Second * 60},
		hotCache:  &cache{cacheBytes: cacheBytes / 8, ttl: time.Second * 60},
		loader:    &singleflight.Group{},
		keys:   map[string]*KeyStats{},
	}
	groups[name] = g
	return g
}

// GetGroup 根据name获取对应的Group
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

// Get 函数用于获取缓存数据，获取顺序为：热点缓存、主缓存、数据源
func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required")
	}
	if v, ok := g.hotCache.get(key); ok {
		log.Println("[GeeCache] hit hotCache")
		return v, nil
	}
	// 从maincache中查找缓存
	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] hit")
		return v, nil
	}
	// 缓存不在就用回调函数查，然后加载到缓存
	return g.load(key)
}

// load 方法的逻辑是首先尝试从远程节点获取数据，如果失败或者没有配置远程节点，则回退到本地获取
func (g *Group) load(key string) (value ByteView, err error) {
	// 每个key只被获取一次（本地或远程）
	// 无论有多少并发调用
	viewi, err := g.loader.Do(key, func() (interface{}, error) {
		if g.peers != nil {
			if peer, ok := g.peers.PickPeer(key); ok {
				if value, err := g.getFromPeer(peer, key); err == nil {
					return value, nil
				}
				log.Println("[GeeCache] Failed to get from peer", err)
			}
		}
		return g.getLocally(key) //从本地获取缓存数据
	})

	if err == nil {
		return viewi.(ByteView), nil
	}
	return
}

func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
	req := &proto.Request{
		Group: g.name,
		Key:   key,
	}
	res := &proto.Response{}
	err := peer.Get(req, res)
	if err != nil {
		return ByteView{}, err
	}
	// 远程获取cnt++
	if stat, ok := g.keys[key]; ok {
		stat.remoteCnt.Add(1)
		// 计算qps
		interval := float64(time.Now().Unix()-stat.firstGetTime.Unix()) / 60
		qps := stat.remoteCnt.Get() / int64(math.Max(1, math.Round(interval)))
		if qps >= int64(maxMinuteRemoteQPS) {
			// 存入hotcache
			g.populateHotCache(key, ByteView{b: res.Value})
			// 删除映射关系节省内存
			mu.Lock()
			delete(g.keys, key)
			mu.Unlock()
		} else {
			g.keys[key] = &KeyStats{
				firstGetTime: time.Now(),
				remoteCnt:    1,
			}
		}
	}
	return ByteView{b: res.Value}, nil
}

// getLocally 从数据源获取数据，然后将数据添加到mainCache中
func (g *Group) getLocally(key string) (ByteView, error) {
	bytes, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err

	}
	value := ByteView{b: cloneBytes(bytes)}
	g.populateCache(key, value)
	return value, nil
}

// populateCache 将数据添加到mainCache中
func (g *Group) populateCache(key string, value ByteView) {
	g.mainCache.add(key, value)
}

// populateHotCache 将数据添加到hotCache中
func (g *Group) populateHotCache(key string, value ByteView) {
	g.hotCache.add(key, value)
}

type Getter interface {
	Get(key string) ([]byte, error)
}

type GetterFunc func(key string) ([]byte, error)

func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}
