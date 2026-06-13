package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cashcowvalley/backend/internal/config"
	"cashcowvalley/backend/internal/delivery/http/handler"
	"cashcowvalley/backend/internal/delivery/http/middleware"
	"cashcowvalley/backend/internal/usecase"
	customRedis "cashcowvalley/backend/pkg/redis"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Initialize DB and Redis
	db := config.InitDatabase()
	customRedis.InitRedis()

	// 2. Setup Clean Architecture Dependencies
	farmUC := usecase.NewFarmUsecase(db)
	marketUC := usecase.NewMarketUsecase(db)
	adWebhookUC := usecase.NewAdWebhookUsecase(db)
	userUC := usecase.NewUserUsecase(db)
	authUC := usecase.NewAuthUsecase(db)
	adminUC := usecase.NewAdminUsecase(db)

	// Seed Dev Wallet as Root Admin
	authUC.SeedDevWallet(context.Background())

	gameHandler := handler.NewGameHandler(farmUC, marketUC, adWebhookUC, userUC)
	userHandler := handler.NewUserHandler(userUC)
	authHandler := handler.NewAuthHandler(authUC)
	adminHandler := handler.NewAdminHandler(adminUC)

	// 3. Setup Router
	if os.Getenv("ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// Global Error Recovery & Security
	r.Use(middleware.GlobalErrorHandler())
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.BodySizeLimiter(2 * 1024 * 1024))

	// V1 API Group
	v1 := r.Group("/api/v1")
	v1.Use(middleware.RateLimiter(customRedis.Client))
	{
		v1.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "UP"})
		})

		// Public Auth
		v1.GET("/auth/nonce/:wallet", authHandler.GetNonceHandler)
		v1.POST("/auth/login", authHandler.LoginWithSignatureHandler)
		// Legacy login (no signature verification) — ONLY enabled in non-production
		if os.Getenv("ENV") != "production" {
			v1.POST("/auth/login-legacy", authHandler.LoginOrRegisterHandler)
		}

		// Webhooks (Public but Signature protected)
		v1.POST("/webhooks/ad-reward", gameHandler.AdWebhookHandler)

		// Protected Game Routes
		protected := v1.Group("/")
		protected.Use(middleware.RequireAuth())
		{
			// User / Referral
			protected.POST("/user/referral/bind", userHandler.BindReferrerHandler)
			protected.GET("/user/referral/stats", userHandler.GetReferralStatsHandler)

			// Farm
			protected.GET("/farm/status", gameHandler.GetFarmStatusHandler)
			protected.POST("/farm/feed", gameHandler.FeedCowHandler)
			protected.POST("/farm/harvest", gameHandler.HarvestFarmHandler)

			// Market (P2P & Platform)
			protected.GET("/market/listings", gameHandler.GetMarketListingsHandler)
			protected.POST("/market/buy", gameHandler.BuyItemHandler)
			protected.POST("/market/sell", gameHandler.SellItemHandler)
			protected.POST("/market/platform/buy", gameHandler.BuyPlatformItemHandler)

			// Gold Economy
			protected.POST("/market/sell-milk-gold", gameHandler.SellMilkForGoldHandler)
			protected.POST("/market/buy-inapp-gold", gameHandler.BuyInAppItemHandler)
			protected.POST("/market/swap-gold", gameHandler.SwapGoldHandler)
			protected.POST("/market/stake-inapp", gameHandler.StakeInAppHandler)
			protected.POST("/market/claim-inapp", gameHandler.ClaimInAppHandler)
			protected.POST("/market/deposit", gameHandler.DepositHandler)
			protected.POST("/market/withdraw", gameHandler.WithdrawHandler)
		}

		// Admin Routes (ADMIN role required)
		admin := v1.Group("/admin")
		admin.Use(middleware.RequireAuth(), middleware.RequireRole("ADMIN"))
		{
			admin.POST("/transfer", adminHandler.TransferHandler)
			admin.GET("/users", adminHandler.ListUsersHandler)
			admin.GET("/stats", adminHandler.StatsHandler)
		}
	}

	// 404 Handler
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": "Endpoint tidak ditemukan",
		})
	})

	// 405 Method Not Allowed Handler
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"status":  "error",
			"message": "Metode HTTP tidak diizinkan untuk endpoint ini",
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Graceful Shutdown: menangkap sinyal OS (SIGTERM/SIGINT)
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Printf("[CASH COW VALLEY] Golang API v1 Server Listening on PORT %s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server gagal dijalankan: %v", err)
		}
	}()

	// Tunggu sinyal shutdown (Ctrl+C / Docker stop / kill)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[CASH COW VALLEY] Menerima sinyal shutdown, menunggu request selesai...")

	// Beri waktu 10 detik untuk request yang sedang berjalan
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server gagal shutdown dengan bersih: %v", err)
	}

	// Tutup koneksi DB
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.Close()
	}

	log.Println("[CASH COW VALLEY] Server berhenti dengan aman. Selamat tinggal! 🐮")
}
