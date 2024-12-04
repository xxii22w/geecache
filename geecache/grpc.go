package geecache

import (
	"context"
	"fmt"
	"geecache/consistenthash"
	pb "geecache/proto"
	"geecache/registry"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const defaultReplicas = 50 // 默认虚拟节点数量

// server 模块为geecache之间提供通信能力
// 这样部署在其他机器上的cache可以通过访问server获取缓存

var (
	defaultEtcdConfig = clientv3.Config{
		Endpoints:   []string{"localhost:2379"}, // etcd服务器的地址，这里使用本地地址和默认端口
		DialTimeout: 5 * time.Second,            // 建立连接的超时时间为5秒
	}
)

type Client struct {
	baseURL string // 服务名称 geecache/ip:addr
}

// NewClient 创建一个远程节点客户端
func NewClient(service string) *Client {
	return &Client{baseURL: service}
}

// server 和group是解耦的，所以server要自己做并发控制
type Server struct {
	pb.UnimplementedGroupCacheServer
	self       string     // 当前服务器地址,ip:port
	status     bool       // 服务器运行状态
	stopSignal chan error // 用于接收通知，通知服务器停止运行
	mu         sync.Mutex
	peers      *consistenthash.Map // 一致性哈希，用于确定缓存数据在集群中的分布
	clients    map[string]*Client  //  用于存储其他节点的客户端连接
}

func NewServer(self string) (*Server, error) {
	return &Server{
		self:    self,
		peers:   consistenthash.New(defaultReplicas, nil),
		clients: map[string]*Client{},
	}, nil
}

// Get 实现了server结构体处理grpc客户端的请求
func (s *Server) Get(ctx context.Context, in *pb.Request) (*pb.Response, error) {
	group, key := in.Group, in.Key
	resp := &pb.Response{}
	log.Printf("[Geecache_svr %s] Recv RPC Request - (%s)/(%s)", s.self, group, key)
	if key == "" {
		return resp, fmt.Errorf("key required")
	}
	g := GetGroup(group)
	if g == nil {
		return resp, fmt.Errorf("group not found")
	}
	view, err := g.Get(key)
	if err != nil {
		return resp, err
	}
	// 将获取到的缓存数据序列化为protubuf，存到value字段
	body, err := proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		log.Printf("encodeing response body:%v", err)
	}
	resp.Value = body
	return resp, nil
}

// start 负责启动缓存服务，监听指定端口，注册grpc服务到服务器，并且收到停止信号关闭服务
func (s *Server) Start() error {
	s.mu.Lock()
	if s.status == true {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}
	// 1. 设置status为true 表示服务器已在运行
	// 2. 初始化stop channel,这用于通知registry stop keep alive
	// 3. 初始化tcp socket并开始监听
	// 4. 注册rpc服务至grpc 这样grpc收到request可以分发给server处理
	// 5. 将自己的服务名/Host地址注册至etcd 这样client可以通过etcd
	//    获取服务Host地址 从而进行通信。这样的好处是client只需知道服务名
	//    以及etcd的Host即可获取对应服务IP 无需写死至client代码中
	s.status = true
	s.stopSignal = make(chan error)

	port := strings.Split(s.self, ":")[1]
	lis, err := net.Listen("tcp", ":"+port) // 监听指定的tcp端口，用于接收grpc请求
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterGroupCacheServer(grpcServer, s)
	//创建一个新的 gRPC 服务器 grpcServer，然后将当前的 Server 对象 s 注册为 gRPC 服务。
	//这样，gRPC 服务器就能够处理来自客户端的请求。
	go func() {
		// 注册服务至 etcd。该操作会一直阻塞，直到停止信号被接收。
		err := registry.Register("geecache", s.self, s.stopSignal)
		if err != nil {
			log.Fatalf(err.Error())
		}
		// Close channel
		close(s.stopSignal)
		// Close tcp listen
		err = lis.Close()
		if err != nil {
			log.Fatalf(err.Error())
		}
		log.Printf("[%s] Revoke service and close tcp socket ok.", s.self)
	}()

	s.mu.Unlock()

	if err := grpcServer.Serve(lis); s.status && err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}
	return nil
}

// Set 方法用于设置其他缓存节点的地址信息，并为每个节点创建相应的客户端连接
func (s *Server) Set(peersAddr ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers.Add(peersAddr...)
	for _, peerAddr := range peersAddr {
		service := fmt.Sprintf("geecache/%s", peerAddr)
		s.clients[peerAddr] = NewClient(service) // 使用 NewClient(service) 函数创建一个新的客户端连接，并将连接对象存储在 s.clients 映射中，以便后续通过节点地址进行查找和通信
	}
}

// PickPeer 方法，用于根据给定的键选择相应的对等节点
func (s *Server) PickPeer(key string) (PeerGetter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peerAddr := s.peers.Get(key) //根据给定的键 key 选择相应的对等节点的地址 peerAddr
	if peerAddr == s.self {      //如果选择的节点地址与当前服务器的地址相同，说明该节点就是当前服务器本身
		log.Printf("ooh! pick myself, I am %s\n", s.self)
		return nil, false
	}
	log.Printf("[cache %s] pick remote peer: %s\n", s.self, peerAddr)
	return s.clients[peerAddr], true //如果选择的节点不是当前服务器本身，日志会记录当前服务器选择了远程对等节点，并且函数会返回选择的对等节点的客户端连接（s.clients[peerAddr]）和 true，表示选择成功
}

func (s *Server) Stop() {
	s.mu.Lock()
	if s.status == false {
		s.mu.Unlock()
		return
	}
	s.stopSignal <- nil // 发送停止keepalive信号
	s.status = false    // 设置server运行状态为stop
	s.clients = nil     // 清空一致性哈希信息 有助于垃圾回收
	s.peers = nil       // 清空一致性哈希映射
	s.mu.Unlock()
}

// Get 方法允许 Client 结构体实例向远程节点发送请求，获取缓存数据，并将响应解码为 pb.Response 结构体。
func (g *Client) Get(in *pb.Request, out *pb.Response) error {
	cli, err := clientv3.New(defaultEtcdConfig) // 创建一个etcd客户端
	if err != nil {
		return err
	}
	defer cli.Close()

	conn, err := registry.EtcdDial(cli, g.baseURL) //使用etcd客户端发现指定服务（g.baseURL）并建立连接（conn）。如果发现服务或建立连接失败，则返回错误。
	if err != nil {
		return err
	}
	defer conn.Close()

	grpcClient := pb.NewGroupCacheClient(conn)                               //创建一个 gRPC 客户端，用于向远程对等节点发送请求
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) //创建一个带有10秒超时时间的上下文，并使用该上下文发送 gRPC 请求到远程节点
	defer cancel()
	response, err := grpcClient.Get(ctx, in)
	if err != nil {
		return fmt.Errorf("reading response body:%v", err)
	}
	if err = proto.Unmarshal(response.GetValue(), out); err != nil {
		return fmt.Errorf("decoding response body:%v", err)
	}
	return nil
}

var _ PeerPicker = (*Server)(nil)

// 测试 Client 是否实现了 PeerGetter 接口
var _ PeerGetter = (*Client)(nil)