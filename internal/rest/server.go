package rest

import (
	"net/http"

	"github.com/klauspost/compress/gzhttp"
	"github.com/robalyx/rotector/internal/common/api/middleware/header"
	"github.com/robalyx/rotector/internal/common/api/middleware/ip"
	"github.com/robalyx/rotector/internal/common/api/middleware/ratelimit"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/rest/handler"
	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
)

// Server implements the REST API service.
type Server struct {
	userHandler  *handler.UserHandler
	groupHandler *handler.GroupHandler
}

// NewServer creates a new REST API server.
func NewServer(db database.Client, logger *zap.Logger, config *config.APIConfig) (http.Handler, error) {
	// Create server instance with handlers
	server := &Server{
		userHandler:  handler.NewUserHandler(db, logger),
		groupHandler: handler.NewGroupHandler(db, logger),
	}

	// Create middleware instances
	headerMiddleware := header.New(logger)
	ipMiddleware := ip.New(logger, &config.IP)
	rateLimiter := ratelimit.New(&config.RateLimit, db, logger)

	// Create base router
	router := bunrouter.New()

	// Create API routes group
	router.Use(
		headerMiddleware.AsRESTMiddleware,
		ipMiddleware.AsRESTMiddleware,
		rateLimiter.AsRESTMiddleware,
	).WithGroup("/v1", func(g *bunrouter.Group) {
		g.GET("/users/:id", server.userHandler.GetUser)
		g.GET("/groups/:id", server.groupHandler.GetGroup)
	})

	// Add redirect for /docs to /docs/index.html
	router.GET("/docs", func(w http.ResponseWriter, req bunrouter.Request) error {
		http.Redirect(w, req.Request, "/docs/index.html", http.StatusFound)
		return nil
	})

	// Add swagger documentation endpoint
	router.GET("/docs/:path", func(w http.ResponseWriter, req bunrouter.Request) error {
		// Reconstruct the full path for swagger
		path := "/docs/" + req.Param("path")
		req.Request.URL.Path = path

		httpSwagger.Handler(
			httpSwagger.URL("/docs/doc.json"),
		).ServeHTTP(w, req.Request)
		return nil
	})

	// Add gzip compression
	return gzhttp.GzipHandler(router), nil
}
