package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	emailverifier "github.com/AfterShip/email-verifier"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// Redis key prefix
const creditKeyPrefix = "credits:"

var (
	verifier = emailverifier.NewVerifier()
	rdb      *redis.Client
	ctx      = context.Background()
	
	// Concurrency control
	maxWorkers = runtime.NumCPU() * 8
	workerPool = make(chan struct{}, maxWorkers)
	
	// Rate limiting
	limiter = rate.NewLimiter(100, 200)
	ipLimiters   = make(map[string]*rate.Limiter)
	ipLimitersMu sync.RWMutex
)

func init() {
	verifier.EnableGravatarCheck()
	verifier.EnableSMTPCheck()

	// Initialize Redis
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379" // fallback for local development
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisURL,
		Password: "", // no password set
		DB:       0,  // use default DB
		PoolSize: maxWorkers, // Match pool size with worker count
	})

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}

	// Initialize test user credits if not exists
	rdb.SetNX(ctx, creditKeyPrefix+"test-key", 1000, 0)
	
	// Initialize worker pool
	for i := 0; i < maxWorkers; i++ {
		workerPool <- struct{}{}
	}
}

// Credit operations with Redis
func checkCredits(apiKey string, required int64) (bool, error) {
	credits, err := rdb.Get(ctx, creditKeyPrefix+apiKey).Int64()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return credits >= required, nil
}

func deductCredits(apiKey string, amount int64) (int64, error) {
	key := creditKeyPrefix + apiKey
	pipe := rdb.Pipeline()
	
	// Atomic operation: deduct credits and get new balance
	pipe.DecrBy(ctx, key, amount)
	pipe.Get(ctx, key)
	
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	
	// Get the new balance from the second command
	balance, err := cmds[1].(*redis.StringCmd).Int64()
	if err != nil {
		return 0, err
	}
	
	return balance, nil
}

func getCredits(apiKey string) (int64, error) {
	credits, err := rdb.Get(ctx, creditKeyPrefix+apiKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return credits, err
}

func getIPLimiter(ip string) *rate.Limiter {
	ipLimitersMu.RLock()
	limiter, exists := ipLimiters[ip]
	ipLimitersMu.RUnlock()

	if !exists {
		ipLimitersMu.Lock()
		limiter = rate.NewLimiter(10, 20)
		ipLimiters[ip] = limiter
		ipLimitersMu.Unlock()
	}

	return limiter
}

func rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			c.Abort()
			return
		}

		ip := c.ClientIP()
		ipLimiter := getIPLimiter(ip)
		if !ipLimiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests from your IP"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API key is required"})
			return
		}

		exists, err := rdb.Exists(ctx, creditKeyPrefix+apiKey).Result()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to check API key"})
			return
		}
		if exists == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			return
		}

		c.Set("apiKey", apiKey)
		c.Next()
	}
}

func verifyEmailWithTimeout(email string) (*emailverifier.Result, error) {
	resultChan := make(chan *emailverifier.Result, 1)
	errChan := make(chan error, 1)
	
	// Get worker from pool
	<-workerPool
	defer func() { workerPool <- struct{}{} }()

	go func() {
		result, err := verifier.Verify(email)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("verification timeout")
	}
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(rateLimitMiddleware())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"workers": maxWorkers,
			"redis": rdb.Ping(ctx).Err() == nil,
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		api.POST("/verify", func(c *gin.Context) {
			var req struct {
				Email string `json:"email" binding:"required,email"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
				return
			}

			apiKey := c.GetString("apiKey")
			
			// Check credits
			hasCredits, err := checkCredits(apiKey, 1)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check credits"})
				return
			}
			if !hasCredits {
				c.JSON(http.StatusPaymentRequired, gin.H{"error": "Insufficient credits"})
				return
			}

			// Verify email
			ret, err := verifyEmailWithTimeout(req.Email)
			
			// Deduct credits atomically
			creditsLeft, err := deductCredits(apiKey, 1)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deduct credits"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"email": req.Email,
				"result": ret,
				"error": err.Error(),
				"credits_left": creditsLeft,
			})
		})

		api.POST("/verify/batch", func(c *gin.Context) {
			var req struct {
				Emails []string `json:"emails" binding:"required,min=1,max=1000,dive,email"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
				return
			}

			apiKey := c.GetString("apiKey")
			requiredCredits := int64(len(req.Emails))

			// Check credits
			hasCredits, err := checkCredits(apiKey, requiredCredits)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check credits"})
				return
			}
			if !hasCredits {
				c.JSON(http.StatusPaymentRequired, gin.H{"error": "Insufficient credits"})
				return
			}

			results := make([]map[string]interface{}, len(req.Emails))
			var wg sync.WaitGroup
			
			for i, email := range req.Emails {
				wg.Add(1)
				go func(idx int, emailAddr string) {
					defer wg.Done()
					ret, err := verifyEmailWithTimeout(emailAddr)
					results[idx] = map[string]interface{}{
						"email": emailAddr,
						"result": ret,
					}
					if err != nil {
						results[idx]["error"] = err.Error()
					}
				}(i, email)
			}

			wg.Wait()

			// Deduct credits atomically
			creditsLeft, err := deductCredits(apiKey, requiredCredits)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deduct credits"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"results": results,
				"credits_left": creditsLeft,
			})
		})

		api.GET("/credits", func(c *gin.Context) {
			apiKey := c.GetString("apiKey")
			credits, err := getCredits(apiKey)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get credits"})
				return
			}
			
			c.JSON(http.StatusOK, gin.H{
				"credits": credits,
			})
		})

		api.POST("/credits/add", func(c *gin.Context) {
			var req struct {
				Amount int64 `json:"amount" binding:"required,min=1"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
				return
			}

			apiKey := c.GetString("apiKey")
			key := creditKeyPrefix + apiKey
			
			// Atomic increment
			newBalance, err := rdb.IncrBy(ctx, key, req.Amount).Result()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add credits"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"credits_added": req.Amount,
				"new_balance": newBalance,
			})
		})
	}

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Starting server on :8080 with %d workers\n", maxWorkers)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
