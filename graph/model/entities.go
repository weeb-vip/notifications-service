package model

// Entities owned by other subgraphs. This service returns only the key; the
// router resolves the rest from user-service and anime-api. Hand-written
// rather than generated because gqlgen will not generate a model for a type
// whose only field is external, yet the federation entity union needs one.

// PublicUser is user-service's public profile entity.
type PublicUser struct {
	ID string `json:"id"`
}

// IsEntity marks the type as a federation entity.
func (PublicUser) IsEntity() {}

// Anime is anime-api's anime entity.
type Anime struct {
	ID string `json:"id"`
}

// IsEntity marks the type as a federation entity.
func (Anime) IsEntity() {}

// Work is anime-api's manga / light novel entity.
type Work struct {
	ID string `json:"id"`
}

// IsEntity marks the type as a federation entity.
func (Work) IsEntity() {}
