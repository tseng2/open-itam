package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultPublicRateLimitPerMinute 免登录公开接口的默认限流阈值（次/分钟/IP）。
// 扫码盘点读多写少，该阈值对人工扫码足够宽裕，同时压制令牌枚举与脚本轰炸
const DefaultPublicRateLimitPerMinute = 120

// maxRateLimitBuckets 桶表规模上限：超过时在同窗口惰性回收过期桶，防内存无限增长
const maxRateLimitBuckets = 10000

// rateWindow 固定窗口计数：minute 为窗口编号（Unix 分钟），count 为窗口内请求数
type rateWindow struct {
	minute int64
	count  int
}

// PublicRateLimit 按客户端 IP 的内存固定窗口限流。项目单进程部署
// （与 webhook 引擎同理由），无需 Redis；桶规模失控时在同窗口惰性回收过期桶，
// 防内存无限增长。超限返回 429 与统一错误信封
func PublicRateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = DefaultPublicRateLimitPerMinute
	}

	var (
		mu      sync.Mutex
		buckets = make(map[string]*rateWindow)
	)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		cur := time.Now().Unix() / 60

		mu.Lock()
		w, ok := buckets[ip]
		if !ok || w.minute != cur {
			w = &rateWindow{minute: cur}
			buckets[ip] = w
			if len(buckets) > maxRateLimitBuckets {
				for k, v := range buckets {
					if v.minute != cur {
						delete(buckets, k)
					}
				}
			}
		}
		w.count++
		exceeded := w.count > perMinute
		mu.Unlock()

		if exceeded {
			c.AbortWithStatusJSON(429, gin.H{"code": 42901, "message": "请求过于频繁，请稍后再试"})
			return
		}
		c.Next()
	}
}
