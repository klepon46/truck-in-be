package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "truckin-be/docs"
	"truckin-be/internal/auth"
	"truckin-be/internal/movement"
	"truckin-be/internal/operations"
)

const (
	appPermission = "unitlog:app:default:view"
	webPermission = "unitlog:web:default:view"
)

type Dependencies struct {
	Database           Pinger
	Movement           *movement.Service
	Operations         *operations.Service
	JWTSecret          []byte
	PermissionClaim    string
	ActorIDClaim       string
	ActorNameClaim     string
	CORSAllowedOrigins []string
	Drivers            DriverSearcher
	Documents          DocumentLookup
	UnitPicker         UnitPicker
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.SetTrustedProxies(nil)
	router.Use(gin.Recovery())
	router.Use(cors(dependencies.CORSAllowedOrigins))
	router.GET("/health", health(dependencies.Database))
	router.GET("/swagger-ui/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/swagger/*any", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/swagger-ui"+c.Param("any"))
	})

	api := router.Group("/api/v1")
	api.Use(auth.Authenticate(dependencies.JWTSecret))
	api.POST("/movements", auth.RequirePermission(dependencies.PermissionClaim, appPermission), movementHandler(dependencies))
	api.GET("/drivers", auth.RequirePermission(dependencies.PermissionClaim, appPermission), driversHandler(dependencies.Drivers))
	api.GET("/movement-documents/work-orders", auth.RequirePermission(dependencies.PermissionClaim, appPermission), workshopLookupHandler(dependencies.Documents))
	api.GET("/movement-documents/spp", auth.RequirePermission(dependencies.PermissionClaim, appPermission), sppLookupHandler(dependencies.Documents))
	api.GET("/unit-picker", auth.RequirePermission(dependencies.PermissionClaim, appPermission), unitPickerHandler(dependencies.UnitPicker))

	operationsAPI := api.Group("/")
	operationsAPI.Use(auth.RequirePermission(dependencies.PermissionClaim, webPermission))
	operationsAPI.GET("/dashboard", dashboardHandler(dependencies.Operations))
	operationsAPI.GET("/units", unitsHandler(dependencies.Operations))
	operationsAPI.GET("/units/export", unitsExportHandler(dependencies.Operations))
	operationsAPI.GET("/units/:id", unitHandler(dependencies.Operations))
	operationsAPI.GET("/units/:id/transactions", unitTransactionsHandler(dependencies.Operations))
	operationsAPI.GET("/transactions", transactionsHandler(dependencies.Operations))
	operationsAPI.GET("/transactions/export", transactionsExportHandler(dependencies.Operations))
	return router
}
