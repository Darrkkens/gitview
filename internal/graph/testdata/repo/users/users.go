package users

import (
	"strings"

	"example.com/repo/store"
)

type Service struct {
	S store.Store
}

func New(s store.Store) *Service { return &Service{S: s} }

func (s *Service) GetUser(id int) (*store.User, error) {
	return s.S.Get(id)
}

// Format has the same name as report.Format.
func Format(u *store.User) string { return strings.ToUpper(u.Profile.Name) }
