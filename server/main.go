// cmd/forum-server/main.go
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rexlx/volconvo/forum"
)

func main() {
	// Get the database connection string from an environment variable.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("DATABASE_URL environment variable is not set")
		dbURL = "user=rxlx password=thereISnosp0)n host=192.168.86.120 dbname=volconvo" // Default for local development
	}

	// Initialize the database connection.
	forumDB, err := forum.NewDatabase(dbURL)
	if err != nil {
		log.Fatalf("Could not initialize database: %v", err)
	}
	log.Println("Successfully connected to the database.")
	forumDB.CreateTables()

	// Create the forum handler, injecting the database dependency.
	forumHandler, err := forum.NewHandlers(forumDB)
	if err != nil {
		log.Fatalf("Could not create forum handler: %v", err)
	}

	// Create a new ServeMux and register the forum routes.
	mux := http.NewServeMux()
	forumHandler.RegisterRoutes(mux)

	// Start the server.
	port := ":8080"
	log.Printf("Starting forum server on %s", port)
	sessionHandler := forumHandler.Session.LoadAndSave(mux)
	svr := &http.Server{
		Addr:    port,
		Handler: sessionHandler,
	}

	go forumHandler.StartNotificationListener(1250 * time.Second)
	if err := svr.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
