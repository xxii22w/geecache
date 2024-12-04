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

// New 函数通过传入的虚拟节点倍数replicas和哈希函数fn
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

// 对每一个真实节点 key，对应创建 m.replicas 个虚拟节点，虚拟节点的名称是：strconv.Itoa(i) + key，即通过添加编号的方式区分不同虚拟节点
// 使用 m.hash() 计算虚拟节点的哈希值，使用 append(m.keys, hash) 添加到环上。在 hashMap 中增加虚拟节点和真实节点的映射关系。
// 最后一步，环上的哈希值排序。
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

// Get 函数主要是通过key获取真实节点
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
