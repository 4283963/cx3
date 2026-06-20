package router

import (
	"cx3/config"
	"cx3/controller"
	"cx3/middleware"
	redisrepo "cx3/repository/redis"
	mysqlrepo "cx3/repository/mysql"
	"cx3/service"

	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)

	r := gin.New()

	r.Use(middleware.TraceID())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.ErrorHandler())
	r.Use(middleware.CORSMiddleware())

	middleware.InitRateLimiters(&cfg.RateLimit)

	stockRepo := redisrepo.NewStockRepo(redisrepo.Client, cfg)
	transactionRepo := mysqlrepo.NewTransactionRepo(mysqlrepo.DB)
	shelfRepo := mysqlrepo.NewShelfRepo(mysqlrepo.DB)

	shelfService := service.NewShelfService(cfg, stockRepo, transactionRepo, shelfRepo)
	shelfController := controller.NewShelfController(shelfService)

	api := r.Group("/api/v1")
	{
		shelf := api.Group("/shelf")
		{
			shelf.POST("/pickup", middleware.PickupRateLimit(), shelfController.Pickup)
			shelf.POST("/lock", middleware.LockRateLimit(), shelfController.Lock)
			shelf.POST("/unlock", middleware.LockRateLimit(), shelfController.Unlock)
			shelf.POST("/promo", middleware.LockRateLimit(), shelfController.SetPromo)
			shelf.POST("/promo/cancel", middleware.LockRateLimit(), shelfController.CancelPromo)
			shelf.GET("/promo/:shelf_id/:slot_no", shelfController.GetPromo)
			shelf.GET("/status/:shelf_id", shelfController.GetStatus)
			shelf.GET("/stock/:shelf_id/:slot_no", shelfController.GetStock)
			shelf.GET("/check/:shelf_id", shelfController.SelfCheck)
			shelf.GET("/audit/:shelf_id", shelfController.GetAuditLogs)
		}

		health := api.Group("/health")
		{
			health.GET("", func(c *gin.Context) {
				c.JSON(200, gin.H{
					"status": "ok",
					"time":   gin.H{},
				})
			})
		}
	}

	return r
}
