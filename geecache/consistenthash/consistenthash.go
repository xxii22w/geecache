package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

// 函数类型·hash，用依赖注入
type Hash func(data []byte) uint32

// Map包含所有哈希值
type Map struct {
	hash     Hash  // 哈希函数依赖，后续可自行更换哈希函数
	replicas int   // 虚拟节点倍数
	keys     []int // 哈希环
	hashMap  map[int]string	// 虚拟节点hash到真实节点名称的映射
}

// 传入虚拟节点的倍数和哈希函数
func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE	
	}
	return m
}

// ADD adds some keys to the hash.
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = key // 虚拟节点和真实节点的映射关系
		}
	}
	sort.Ints(m.keys) // 哈希值排序
}

// Get gets the closest item in the hash to the provided key.
func (m *Map) Get(key string) string {
	if len(m.keys) == 0 {
		return ""
	}
	// 1. 计算key哈希值
	hash := int(m.hash([]byte(key)))
	// 2. 通过二分查找找到哈希环数组中第一个比它大的位置（即环的顺时针）
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})
	// 通过hashMap找到真实的节点
	return m.hashMap[m.keys[idx%len(m.keys)]]
}
