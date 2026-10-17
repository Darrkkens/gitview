package service

import (
	"context"
	"errors"
	"fmt"

	"example.com/deep/model"
)

var (
	ErrInvalidID = errors.New("invalid id")
	ErrNotFound  = errors.New("client not found")
)

// Repository is implemented by repository.ClientRepository.
type Repository interface {
	FindByID(ctx context.Context, id int64) (*model.Client, error)
	Search(ctx context.Context, name string) ([]model.Client, error)
}

type ClientService struct{ repo Repository }

func NewClientService(r Repository) *ClientService { return &ClientService{repo: r} }

// GetClientByID validates the id and loads the client.
func (s *ClientService) GetClientByID(ctx context.Context, id int64) (*model.Client, error) {
	if id <= 0 {
		return nil, ErrInvalidID
	}
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find client %d: %w", id, err)
	}
	if c == nil {
		return nil, ErrNotFound
	}
	return c, nil
}

// Search forwards a free-text term to the repository.
func (s *ClientService) Search(ctx context.Context, term string) ([]model.Client, error) {
	return s.repo.Search(ctx, term)
}
