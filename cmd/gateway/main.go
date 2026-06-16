package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// Health check for Gateway
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
		})
	})

	// WebSocket Upgrade Endpoint (Placeholder for gorilla/websocket upgrade in future phases)
	r.GET("/ws", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "WebSocket upgrade endpoint placeholder",
		})
	})

	log.Println("Starting WebSocket Gateway on port 8081...")
	if err := r.Run(":8081"); err != nil {
		log.Fatalf("Failed to run WebSocket Gateway: %v", err)
	}
}
