package handlers

import (
	"context"
	"crawler/config"
	"crawler/models"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HandleGetCrawls handles the GET /crawls endpoint
// @Summary List recent crawl results
// @Description Retrieves a list of recent crawl results from the database
// @Tags crawls
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Success 200 {array} models.CrawlResult
// @Failure 503 {object} map[string]string
// @Router /crawls [get]
func HandleGetCrawls(w http.ResponseWriter, r *http.Request) {
	if config.CrawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := int64(10) // default
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			limit = l
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find crawls sorted by crawled_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "crawled_at", Value: -1}})
	cursor, err := config.CrawlCollection.Find(ctx, bson.D{}, opts)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var crawls []models.CrawlResult
	if err := cursor.All(ctx, &crawls); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawls)
}

// HandleGetCrawlByID handles the GET /crawls/{id} endpoint
// @Summary Get specific crawl result
// @Description Retrieves a specific crawl result by its ID
// @Tags crawls
// @Accept json
// @Produce json
// @Param id path string true "Crawl ID"
// @Success 200 {object} models.CrawlResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /crawls/{id} [get]
func HandleGetCrawlByID(w http.ResponseWriter, r *http.Request) {
	if config.CrawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]

	objectID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var crawl models.CrawlResult
	err = config.CrawlCollection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&crawl)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			http.Error(w, "Crawl not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawl)
}