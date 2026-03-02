package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AbhinavGarg815/video-streaming-platform/internal/auth"
	"github.com/AbhinavGarg815/video-streaming-platform/internal/config"
	"github.com/AbhinavGarg815/video-streaming-platform/internal/database"
)

func main() {
	env, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()

	pool, err := database.NewPool(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	startupQueryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	startupTime, err := database.GetCurrentTime(startupQueryCtx, pool)
	cancel()
	if err != nil {
		log.Fatalf("run startup query: %v", err)
	}
	log.Printf("database connected, current time: %s", startupTime.Format(time.RFC3339))

	authRepo := auth.NewRepository(pool)
	if err := authRepo.EnsureSchema(ctx); err != nil {
		log.Fatalf("ensure auth schema: %v", err)
	}

	authService := auth.NewService(authRepo, env.JWTSecret, env.AccessTTL, env.RefreshTTL)
	authHandler := auth.NewHandler(authService)

	r := gin.Default()
	authHandler.RegisterRoutes(r)

	r.GET("/health", func(c *gin.Context) {
		queryCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		dbTime, err := database.GetCurrentTime(queryCtx, pool)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "degraded",
				"error":  err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"db_time": dbTime.Format(time.RFC3339),
		})
	})

	if err := r.Run(":" + env.Port); err != nil {
		panic(err)
	}
}
