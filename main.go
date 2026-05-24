package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/brianerandall/chirpy/dtos"
	"github.com/brianerandall/chirpy/internal/database"
	"github.com/brianerandall/chirpy/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")

	if platform == "" {
		platform = "dev"
	}

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

	apiCfg := &middleware.ApiConfig{}
	apiCfg.DbQueries = dbQueries
	apiCfg.Platform = platform

	fs := http.FileServer(http.Dir("."))

	serveMux.Handle("/app/", apiCfg.MiddlewareMetricsInc(http.StripPrefix("/app", fs)))
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
		</html>`, apiCfg.FileserverHits.Load())))
	})

	serveMux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			Email string `json:"email"`
		}

		decoder := json.NewDecoder(r.Body)
		req := request{}
		err := decoder.Decode(&req)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		user, err := apiCfg.DbQueries.CreateUser(r.Context(), req.Email)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
			return
		}

		middleware.RespondWithJSON(w, http.StatusCreated, dtos.User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		})
	})

	serveMux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			Body   string    `json:"body"`
			UserId uuid.UUID `json:"user_id"`
		}

		decoder := json.NewDecoder(r.Body)
		req := request{}
		err := decoder.Decode(&req)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		if len(req.Body) > 140 {
			middleware.RespondWithError(w, http.StatusBadRequest, "Chirp is too long")
			return
		}

		cleanedBody := middleware.ValidateChirp(req.Body)

		qurtyParams := database.CreateChirpParams{
			Body:   cleanedBody,
			UserID: req.UserId,
		}

		chirp, err := apiCfg.DbQueries.CreateChirp(r.Context(), qurtyParams)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to create chirp")
			return
		}

		middleware.RespondWithJSON(w, http.StatusCreated, dtos.Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		})
	})

	serveMux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if apiCfg.Platform != "dev" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
			return
		}

		err := apiCfg.DbQueries.DeleteUsers(r.Context())
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to reset users")
			return
		}

		apiCfg.FileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	server.ListenAndServe()
}
