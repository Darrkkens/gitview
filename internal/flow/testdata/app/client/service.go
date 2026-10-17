package client

import (
	"context"
	"errors"
)

type Client struct {
	ID   int
	Name string
}

// Repository is implemented by PGRepo.
type Repository interface {
	Find(ctx context.Context, id int) (*Client, error)
}

type ClientService struct{ repo Repository }

func NewClientService(r Repository) *ClientService { return &ClientService{repo: r} }

// GetClientByID validates the id and loads the client.
func (s *ClientService) GetClientByID(ctx context.Context, id int) (*Client, error) {
	if err := ValidateClient(id); err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, id)
}

func ValidateClient(id int) error {
	if id <= 0 {
		return errors.New("invalid id")
	}
	return nil
}

// Leaf makes no calls.
func Leaf() int { return 1 }
