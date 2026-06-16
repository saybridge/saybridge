package main

import (
	"fmt"
	"log"
	"os"

	"github.com/saybridge/saybridge/internal/repositories"
	"github.com/saybridge/saybridge/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: migrate <up|status>")
		fmt.Println("  up      Run all pending migrations (GORM AutoMigrate)")
		fmt.Println("  status  Show current migration status (tables & row counts)")
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := repositories.NewConnectionWithoutMigrate(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	switch os.Args[1] {
	case "up":
		if err := repositories.RunMigrations(db); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		fmt.Println("✅ Migrations completed successfully")

	case "status":
		repositories.PrintMigrationStatus(db)

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
