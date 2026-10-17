package services

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("not found")

type User struct{ ID, Name, Email string }

type Repository interface {
	Insert(ctx context.Context, name, email string) (User, error)
	Find(ctx context.Context, id string) (User, error)
	List(ctx context.Context, offset, limit int) []User
}

type UserService struct {
	repo   Repository
	status int // configured at runtime
}

func NewUserService(r Repository) *UserService { return &UserService{repo: r} }

func (s *UserService) Create(ctx context.Context, name, email string) (User, error) {
	if name == "" {
		return User{}, errors.New("name required")
	}
	return s.repo.Insert(ctx, name, email)
}

func (s *UserService) Get(ctx context.Context, id string) (User, error) { return s.repo.Find(ctx, id) }

func (s *UserService) List(ctx context.Context, page string, limit int) []User {
	return s.repo.List(ctx, 0, limit)
}

func (s *UserService) AvatarStatus() int { return s.status }
