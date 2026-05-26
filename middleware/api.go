package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/brianerandall/chirpy/internal/database"
)

type ApiConfig struct {
	FileserverHits atomic.Int32
	DbQueries      *database.Queries
	Platform       string
	TokenSecret    string
}

type Response struct {
	Valid        bool   `json:"valid,omitempty"`
	Cleaned_Body string `json:"cleaned_body,omitempty"`
}

func (cfg *ApiConfig) MiddlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.FileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *ApiConfig) MiddlewareMetricReset(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.FileserverHits.Store(0)
		next.ServeHTTP(w, r)
	})
}

func RespondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf("{\"error\":\"%s\"}", msg)))
}

func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}
