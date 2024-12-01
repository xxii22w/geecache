package geecache

import (
	"fmt"
	"geecache/proto"
	"geecache/singleflight"
	"log"
	"sync"
)

// Group 是缓存命名空间 每个group都有一个名字
type Group struct {
	name             string              // 缓存空间的名字
	getter           Getter              // 数据源获取数据
	mainCache        cache               // 主缓存，并发缓存
	peers            PeerPicker          // 用于获取远程节点请求客户端
	loader           *singleflight.Group // 避免被同一个key多次加载造成缓存击穿
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group)
)

// RegisterPeers 注册一个 PeerPicker 以选择远程对等点
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
		mainCache: cache{cacheBytes: cacheBytes},
		loader:    &singleflight.Group{},
	}
	groups[name] = g
	return g
}

// GetGroup 返回之前用 NewGroup 创建的命名分组，或者
// 如果没有这样的组，则返回 nil。
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

// 从缓存中找到key对应的值
func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required")
	}
	// 从maincache中查找缓存
	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] hit")
		return v, nil
	}
	// 缓存不在就用回调函数查，然后加载到缓存
	return g.load(key)
}

// PickPeer() 方法选择节点，若非本机节点，则调用 getFromPeer() 从远程获取
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
		return g.getLocally(key)
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
	return ByteView{b: res.Value}, nil
}

func (g *Group) getLocally(key string) (ByteView, error) {
	bytes, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err

	}
	value := ByteView{b: cloneBytes(bytes)}
	g.populateCache(key, value)
	return value, nil
}

func (g *Group) populateCache(key string, value ByteView) {
	g.mainCache.add(key, value)
}

// A getter loads data for a key
type Getter interface {
	Get(key string) ([]byte, error)
}

// A GetterFunc implements Getter with a function.
type GetterFunc func(key string) ([]byte, error)

// Get implements Getter interface function
func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}
