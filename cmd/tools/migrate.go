package main

import (
	"flag"
	"log"

	"github.com/baxromumarov/job-hunter/internal/store"
)

func main() {
	dbURL := flag.String("db", "postgres://postgres:postgres@localhost:5432/jobhunterdb?sslmode=disable", "Database URL")
	schema := flag.String("schema", "internal/store/schema.sql", "Path to schema file")
	flag.Parse()

	db, err := store.NewStore(*dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(*schema); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	log.Println("Migrations executed successfully")
}
