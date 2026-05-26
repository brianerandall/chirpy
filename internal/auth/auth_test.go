package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHashPassword(t *testing.T) {
	password := "mysecretpassword"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		panic("HashPassword failed: " + err.Error())
	}

	match, err := CheckPasswordHash(password, hashedPassword)
	if err != nil {
		panic("CheckPasswordHash failed: " + err.Error())
	}

	if !match {
		panic("CheckPasswordHash returned false for correct password")
	}

	wrongMatch, err := CheckPasswordHash("wrongpassword", hashedPassword)
	if err != nil {
		panic("CheckPasswordHash failed: " + err.Error())
	}

	if wrongMatch {
		panic("CheckPasswordHash returned true for incorrect password")
	}
}

func TestJWT(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "mysecrettoken"
	expiresIn := time.Hour

	token, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		panic("MakeJWT failed: " + err.Error())
	}

	validatedUserID, err := ValidateJWT(token, tokenSecret)
	if err != nil {
		panic("ValidateJWT failed: " + err.Error())
	}

	if validatedUserID != userID {
		panic("ValidateJWT returned incorrect user ID")
	}

	_, err = ValidateJWT(token, "wrongsecret")
	if err == nil {
		panic("ValidateJWT should have failed with wrong secret")
	}
}

func TestBearerTokenValidation(t *testing.T) {
	tokenSecret := "mysecrettoken"
	userID := uuid.New()
	token, err := MakeJWT(userID, tokenSecret, time.Hour)
	if err != nil {
		panic("MakeJWT failed: " + err.Error())
	}

	bearerToken := "Bearer " + token
	headers := http.Header{}
	headers.Set("Authorization", bearerToken)

	extractedToken, err := GetBearerToken(headers)
	if err != nil {
		panic("GetBearerToken failed: " + err.Error())
	}

	if extractedToken != token {
		panic("GetBearerToken did not extract the correct token")
	}
}
