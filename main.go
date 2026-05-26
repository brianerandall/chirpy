package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/brianerandall/chirpy/dtos"
	"github.com/brianerandall/chirpy/internal/auth"
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
	jwt_secret := os.Getenv("JWT_SECRET")

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
	apiCfg.TokenSecret = jwt_secret

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
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		decoder := json.NewDecoder(r.Body)
		req := request{}
		err := decoder.Decode(&req)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		hashedPassword, err := auth.HashPassword(req.Password)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}

		dbParams := database.CreateUserParams{
			Email:          req.Email,
			HashedPassword: hashedPassword,
		}

		user, err := apiCfg.DbQueries.CreateUser(r.Context(), dbParams)
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

		token, tokenErr := auth.GetBearerToken(r.Header)
		if tokenErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}

		jwtUserId, validateErr := auth.ValidateJWT(token, apiCfg.TokenSecret)
		if validateErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid token")
			return
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

		queryParams := database.CreateChirpParams{
			Body:   cleanedBody,
			UserID: jwtUserId,
			//UserID: req.UserId,
		}

		chirp, err := apiCfg.DbQueries.CreateChirp(r.Context(), queryParams)
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

	serveMux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		chirps, err := apiCfg.DbQueries.GetAllChirps(r.Context())
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to fetch chirps")
			return
		}

		var chirpDtos []dtos.Chirp
		for _, chirp := range chirps {
			chirpDtos = append(chirpDtos, dtos.Chirp{
				ID:        chirp.ID,
				CreatedAt: chirp.CreatedAt,
				UpdatedAt: chirp.UpdatedAt,
				Body:      chirp.Body,
				UserID:    chirp.UserID,
			})
		}

		middleware.RespondWithJSON(w, http.StatusOK, chirpDtos)
	})

	serveMux.HandleFunc("GET /api/chirps/{chirpId}", func(w http.ResponseWriter, r *http.Request) {
		chirpIdStr := r.URL.Path[len("/api/chirps/"):]
		chirpId, err := uuid.Parse(chirpIdStr)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
			return
		}

		chirp, err := apiCfg.DbQueries.GetChirpByID(r.Context(), chirpId)
		if err != nil {
			middleware.RespondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		}

		middleware.RespondWithJSON(w, http.StatusOK, dtos.Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		})
	})

	serveMux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			Email            string `json:"email"`
			Password         string `json:"password"`
			ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
		}

		decoder := json.NewDecoder(r.Body)
		req := request{}
		err := decoder.Decode(&req)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		if req.ExpiresInSeconds == 0 {
			req.ExpiresInSeconds = 3600 // Default to 1 hour if not specified
		}

		if req.ExpiresInSeconds > 3600 {
			req.ExpiresInSeconds = 3600 // Cap at 1 hour for security
		}

		user, err := apiCfg.DbQueries.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}

		match, err := auth.CheckPasswordHash(req.Password, user.HashedPassword)
		if err != nil || !match {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}

		token, err := auth.MakeJWT(user.ID, apiCfg.TokenSecret, time.Duration(req.ExpiresInSeconds)*time.Second)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to generate token")
			return
		}

		middleware.RespondWithJSON(w, http.StatusOK, dtos.User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
			Token:     token,
		})
	})

	fmt.Println("Server is running on port 8080...")

	server.ListenAndServe()
}
