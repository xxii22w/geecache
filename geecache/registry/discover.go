package registry

import (
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// EtcdDial 向grpc请求一个服务，通过提供一个etcd client和service name即可获得Connection
func EtcdDial(c *clientv3.Client, service string) (*grpc.ClientConn, error) {
	etcdResolver, err := resolver.NewBuilder(c) //使用etcd客户端构建了一个服务发现的构建器。
	if err != nil {                             
		return nil, err
	}
	return grpc.Dial(
		"etcd:///"+service,                                       //指定了服务的地址
		grpc.WithResolvers(etcdResolver),                         //用于服务发现的解析器
		grpc.WithTransportCredentials(insecure.NewCredentials()), 
		grpc.WithBlock(),                                         
	)
} 