package middleware

import (
	"strings"
)

func ValidateChirp(chirp string) string {
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
