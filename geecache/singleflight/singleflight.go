package singleflight

import "sync"

type call struct { // call 代表正在进行中或者已经结束的请求
	wg  sync.WaitGroup // 避免重入
	val interface{}
	err error
}

type Group struct { // 管理不同key的请求
	mu sync.Mutex
	m  map[string]*call // 正在进行中，或已经结束的请求
}

// 实现了singleFlight原理：在多个并发请求触发的回调操作里，只有第⼀个回调方法被执行
// 其余请求（落在第⼀个回调方法执行的时间窗口里）阻塞等待第⼀个回调函数执行完成后直接取结果
// 以此保证同⼀时刻只有⼀个回调方法执行，达到防止缓存击穿的目的
func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok { // 如果请求正在进行中，则等待
		g.mu.Unlock()
		c.wg.Wait() // 等待协程结束
		return c.val, c.err
	}
	c := new(call)
	c.wg.Add(1)  // 发起请求前加锁
	g.m[key] = c // 表明该key已经有请求在进⾏
	g.mu.Unlock()

	c.val, c.err = fn() // 执⾏请求
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key) // 完成请求 更新Fligh
	g.mu.Unlock()

	return c.val, c.err
}
