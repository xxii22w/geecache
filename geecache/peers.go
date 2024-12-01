package geecache

import "geecache/proto"

type PeerPicker interface {
	PickPeer(key string) (peer PeerGetter,ok bool)	// 根据传入的 key 选择相应节点 PeerGetter
}

type PeerGetter interface {
	Get(in *proto.Request, out *proto.Response) error	// 用于从对应 group 查找缓存值
}