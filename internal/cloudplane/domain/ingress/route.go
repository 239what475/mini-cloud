// Package ingress 定义 cloud-plane 外置入口数据面的领域对象。
package ingress

// Route 表示一个 public service 当前应发布到 ingress 数据面的路由。
type Route struct {
	// Host 是入口数据面用于匹配请求 Host header 的域名。
	Host string
	// Backends 是该 host 当前可用的私网 backend 列表，格式为 host:port。
	Backends []string
}
