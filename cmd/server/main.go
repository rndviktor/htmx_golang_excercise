package main

import (
	"htmx-golang-excercise/internal/web"
	"log"
	"net/http"
	"time"
)

func main() {
	if err := web.InitTemplates(); err != nil {
		log.Fatalf("Failed to initialize templates: %v", err)
	}

	srv := web.NewServer()

	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      srv.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("Server started on http://localhost:8080")
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
