package main

import (
	"log"

	"github.com/saybridge/saybridge/internal/server"
	"github.com/saybridge/saybridge/pkg/config"

	// Native plugins — blank imports trigger init() hook registration
	_ "github.com/saybridge/saybridge/plugins/ai-agent"
	_ "github.com/saybridge/saybridge/plugins/custom-emoji"
	_ "github.com/saybridge/saybridge/plugins/linkpreview"
	_ "github.com/saybridge/saybridge/plugins/pin"
	_ "github.com/saybridge/saybridge/plugins/slashcmd"
	_ "github.com/saybridge/saybridge/plugins/star"

	_ "github.com/saybridge/saybridge/plugins/call"
	_ "github.com/saybridge/saybridge/plugins/e2ee"
	_ "github.com/saybridge/saybridge/plugins/federation"
	_ "github.com/saybridge/saybridge/plugins/workflow"
)

// @title Saybridge API
// @version 1.0
// @description Saybridge Platform Backend API.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /
// @query.collection.format multi

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter your JWT token with the Bearer prefix, e.g. "Bearer eyJhbGci..."

func main() {
	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
