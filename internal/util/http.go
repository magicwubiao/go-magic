package util

import (
	"net/http"
	"sync"
	"time"
)

var (
	// sharedClient 是全局共享的 HTTP 客户端实例
	sharedClient *http.Client
	once         sync.Once
)

// GetHTTPClient 返回全局共享的 HTTP 客户端实例
// 该客户端配置了合理的超时时间和连接池设置
func GetHTTPClient() *http.Client {
	once.Do(func() {
		sharedClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	})
	return sharedClient
}

// NewHTTPClient 创建一个新的 HTTP 客户端实例
// 仅在需要特殊配置时使用，一般情况下应使用 GetHTTPClient()
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}
