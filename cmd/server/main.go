package main

import (
	"htmx-golang-excercise/internal/web"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := web.InitTemplates(); err != nil {
		log.Fatalf("Failed to initialize templates: %v", err)
	}

	srv, err := web.NewServer()
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Server started on http://localhost%s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
