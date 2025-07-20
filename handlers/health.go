package handlers

import (
	"encoding/json"
	"net/http"
)

// HandleHealth handles the GET /health endpoint
// @Summary Health check
// @Description Check if the API server is running
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}