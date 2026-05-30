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
			IsChirpyRed:    false,
		}

		user, err := apiCfg.DbQueries.CreateUser(r.Context(), dbParams)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
			return
		}

		middleware.RespondWithJSON(w, http.StatusCreated, dtos.User{
			ID:          user.ID,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
			Email:       user.Email,
			IsChirpyRed: user.IsChirpyRed,
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

		token, err := auth.MakeJWT(user.ID, apiCfg.TokenSecret, time.Duration(3600)*time.Second)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to generate token")
			return
		}

		refresh_token, err := auth.MakeRefreshToken()
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to generate refresh token")
			return
		}

		queryParams := database.CreateRefreshTokenParams{
			UserID:    user.ID,
			Token:     refresh_token,
			ExpiresAt: time.Now().AddDate(0, 0, 60), // Refresh token valid for 60 days
		}

		refreshToken, err := apiCfg.DbQueries.CreateRefreshToken(r.Context(), queryParams)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to create refresh token")
			return
		}

		middleware.RespondWithJSON(w, http.StatusOK, dtos.User{
			ID:           user.ID,
			CreatedAt:    user.CreatedAt,
			UpdatedAt:    user.UpdatedAt,
			Email:        user.Email,
			Token:        token,
			RefreshToken: refreshToken.Token,
			IsChirpyRed:  user.IsChirpyRed,
		})
	})

	serveMux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			RefreshToken string `json:"refresh_token"`
		}

		token, tokenErr := auth.GetBearerToken(r.Header)
		if tokenErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}

		refreshToken, err := apiCfg.DbQueries.GetRefreshTokenByToken(r.Context(), token)
		if err != nil || refreshToken.RevokedAt.Valid || refreshToken.ExpiresAt.Before(time.Now()) {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid or expired refresh token")
			return
		}

		user, err := apiCfg.DbQueries.GetUserFromRefreshToken(r.Context(), refreshToken.Token)
		if err != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid refresh token")
			return
		}

		newAccessToken, err := auth.MakeJWT(user.ID, apiCfg.TokenSecret, time.Duration(3600)*time.Second)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to generate new token")
			return
		}

		middleware.RespondWithJSON(w, http.StatusOK, dtos.RefreshToken{
			Token: newAccessToken,
		})
	})

	serveMux.HandleFunc("POST /api/revoke", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			RefreshToken string `json:"refresh_token"`
		}

		token, tokenErr := auth.GetBearerToken(r.Header)
		if tokenErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}

		revokeErr := apiCfg.DbQueries.RevokeRefreshToken(r.Context(), token)
		if revokeErr != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to revoke refresh token")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	serveMux.HandleFunc("PUT /api/users", func(w http.ResponseWriter, r *http.Request) {
		type request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		decoder := json.NewDecoder(r.Body)
		var req request
		if err := decoder.Decode(&req); err != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid request payload")
			return
		}

		token, tokenErr := auth.GetBearerToken(r.Header)
		if tokenErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}

		validated_id, validateErr := auth.ValidateJWT(token, apiCfg.TokenSecret)
		if validateErr != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		user, err := apiCfg.DbQueries.GetUserByID(r.Context(), validated_id)
		if err != nil {
			middleware.RespondWithError(w, http.StatusUnauthorized, "User not found")
			return
		}

		hashedPassword, err := auth.HashPassword(req.Password)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}

		user.Email = req.Email
		user.HashedPassword = hashedPassword

		updatedUser, err := apiCfg.DbQueries.UpdateUserEmailAndPassword(r.Context(), database.UpdateUserEmailAndPasswordParams{
			ID:             user.ID,
			Email:          user.Email,
			HashedPassword: user.HashedPassword,
		})

		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to update user")
			return
		}

		middleware.RespondWithJSON(w, http.StatusOK, dtos.User{
			ID:          updatedUser.ID,
			CreatedAt:   updatedUser.CreatedAt,
			UpdatedAt:   updatedUser.UpdatedAt,
			Email:       updatedUser.Email,
			IsChirpyRed: updatedUser.IsChirpyRed,
		})
	})

	serveMux.HandleFunc("DELETE /api/chirps/{chirpId}", func(w http.ResponseWriter, r *http.Request) {
		chirpIdStr := r.URL.Path[len("/api/chirps/"):]
		chirpId, err := uuid.Parse(chirpIdStr)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
			return
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

		chirp, err := apiCfg.DbQueries.GetChirpByID(r.Context(), chirpId)
		if err != nil {
			middleware.RespondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		}

		if chirp.UserID != jwtUserId {
			middleware.RespondWithError(w, http.StatusForbidden, "You can only delete your own chirps")
			return
		}

		err = apiCfg.DbQueries.DeleteChirp(r.Context(), chirpId)
		if err != nil {
			middleware.RespondWithError(w, http.StatusInternalServerError, "Failed to delete chirp")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	serveMux.HandleFunc("POST /api/polka/webhooks", func(w http.ResponseWriter, r *http.Request) {
		type data struct {
			UserId uuid.UUID `json:"user_id"`
		}

		type request struct {
			Event string `json:"event"`
			Data  data   `json:"data"`
		}

		apiKey, apiKeyErr := auth.GetAPIKey(r.Header)
		if apiKeyErr != nil || apiKey != os.Getenv("POLKA_KEY") {
			middleware.RespondWithError(w, http.StatusUnauthorized, "Missing or invalid API key")
			return
		}

		decoder := json.NewDecoder(r.Body)
		req := request{}
		err := decoder.Decode(&req)
		if err != nil {
			middleware.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		if req.Event == "user.upgraded" {
			_, err := apiCfg.DbQueries.UpdateUserToChirpyRed(r.Context(), req.Data.UserId)
			if err != nil {
				middleware.RespondWithJSON(w, http.StatusNotFound, "Failed to update users to Chirpy Red")
				return
			} else {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	fmt.Println("Server is running on port 8080...")

	server.ListenAndServe()
}
