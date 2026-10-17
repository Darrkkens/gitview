package store

import (
	"errors"
	"sync"
)

// Store is implemented only by MemStore, so calls through it are "likely".
type Store interface {
	Get(id int) (*User, error)
}

type Profile struct{ Name string }

type User struct {
	ID      int
	Profile Profile
}

type Base struct{}

func (Base) Close() error { return nil }

type MemStore struct {
	Base
	mu    sync.Mutex
	users map[int]*User
}

func NewMem() *MemStore { return &MemStore{users: map[int]*User{}} }

func (m *MemStore) Get(id int) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}
