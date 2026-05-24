package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/brianerandall/chripy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

type response struct {
	Valid        bool   `json:"valid,omitempty"`
	Cleaned_Body string `json:"cleaned_body,omitempty"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) middlewareMetricReset(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Store(0)
		next.ServeHTTP(w, r)
	})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf("{\"error\":\"%s\"}", msg)))
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func validateChirp(chirp string) string {
	// For simplicity, we'll just check for the presence of "badword" and replace it with "****"
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	cleanedWords := strings.Split(chirp, " ")

	for _, badWord := range badWords {
		for i, word := range cleanedWords {
			if strings.EqualFold(word, badWord) {
				cleanedWords[i] = "****"
			}
		}
	}

	return strings.Join(cleanedWords, " ")
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		panic(err)
	}

	dbQueries := database.New(db)

	serveMux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}

	apiCfg := &apiConfig{}
	apiCfg.dbQueries = dbQueries

	fs := http.FileServer(http.Dir("."))

	serveMux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fs)))
	serveMux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	serveMux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`
		<html>
  			<body>
    			<h1>Welcome, Chirpy Admin</h1>
    			<p>Chirpy has been visited %d times!</p>
  			</body>
		</html>`, apiCfg.fileserverHits.Load())))
	})

	serveMux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		apiCfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	serveMux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type returnVals struct {
			Body string `json:"body"`
		}

		decoder := json.NewDecoder(r.Body)
		retVal := returnVals{}
		err := decoder.Decode(&retVal)
		if err != nil {
			respondWithError(w, http.StatusOK, "Something went wrong")
			return
		}

		if len(retVal.Body) > 140 {
			respondWithError(w, http.StatusBadRequest, "Chirp is too long")
			return
		}

		cleanedBody := validateChirp(retVal.Body)

		respondWithJSON(w, http.StatusOK, response{Cleaned_Body: cleanedBody})
	})

	server.ListenAndServe()
}
