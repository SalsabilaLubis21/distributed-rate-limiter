package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"path", "status"},
	)
	redisClient *redis.Client
)

// Tier details
var tiers = map[string]map[string]int{
	"free": {
		"rate":   5,  // requests
		"burst":  10, // burst size
		"period": 60, // seconds
	},
	"premium": {
		"rate":   20, // requests
		"burst":  25, // burst size
		"period": 60, // seconds
	},
	"anonymous": {
		"rate":   2,  // requests
		"burst":  2,  // burst size
		"period": 60, // seconds
	},
}

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
}

func main() {
	router := gin.New()
	router.Use(prometheusMiddleware())

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Hello, world!"})
	})

	// Public endpoint
	router.GET("/public", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "This is a public endpoint."})
	})

	// Protected endpoint group
	protected := router.Group("/protected")
	protected.Use(rateLimiterMiddleware())
	protected.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "This is a protected endpoint."})
	})

	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	router.Run(":8080")
}

func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		// Only increment if the request was not already handled as denied
		if c.Writer.Status() != http.StatusTooManyRequests {
			httpRequestsTotal.WithLabelValues(c.Request.URL.Path, "allowed").Inc()
		}
	}
}

func rateLimiterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetHeader("user_id")
		tier := c.GetHeader("X-User-Tier")

		if userID == "" {
			userID = c.ClientIP()
			tier = "anonymous"
		}

		if _, ok := tiers[tier]; !ok {
			tier = "anonymous" // Default to anonymous if tier is unknown
		}

		rate := tiers[tier]["rate"]
		burst := tiers[tier]["burst"]

		// The LUA script is the core of the rate limiter.
		// It's executed atomically by Redis.
		// KEYS[1] - The user's unique key (e.g., user_id or IP address)
		// ARGV[1] - The fill rate (tokens per second)
		// ARGV[2] - The bucket size (burst)
		// ARGV[3] - The current timestamp
		// It returns 1 if the request is allowed, 0 otherwise.
		luaScript := `
            local key = KEYS[1]
            local rate = tonumber(ARGV[1])
            local burst = tonumber(ARGV[2])
            local now = tonumber(ARGV[3])

            local bucket = redis.call("HGETALL", key)
            
            if #bucket == 0 then
                -- New user, create a full bucket
                redis.call("HSET", key, "tokens", burst - 1, "last_seen", now)
                return 1
            end

            local last_tokens = tonumber(bucket[2])
            local last_seen = tonumber(bucket[4])
            
            local elapsed = now - last_seen
            local new_tokens = last_tokens + (elapsed * rate)

            if new_tokens > burst then
                new_tokens = burst
            end

            if new_tokens >= 1 then
                redis.call("HSET", key, "tokens", new_tokens - 1, "last_seen", now)
                return 1
            else
                -- Not enough tokens
                redis.call("HSET", key, "last_seen", now) -- Still update last_seen
                return 0
            end
        `

		script := redis.NewScript(luaScript)
		key := fmt.Sprintf("rate_limit:%s", userID)

		res, err := script.Run(context.Background(), redisClient, []string{key}, float64(rate)/float64(tiers[tier]["period"]), burst, time.Now().Unix()).Result()

		if err != nil {
			// If Redis is down, we can choose to fail open or closed.
			// Failing open for now.
			fmt.Println("Redis error:", err)
			c.Next()
			return
		}

		allowed, ok := res.(int64)
		if !ok || allowed == 0 {
			httpRequestsTotal.WithLabelValues(c.Request.URL.Path, "denied").Inc()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"message": "You have exceeded the rate limit."})
			return
		}

		c.Next()
	}
}