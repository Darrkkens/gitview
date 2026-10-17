package model

// Client is a row of the clients table.
type Client struct {
	ID     int64
	Name   string
	Email  string
	Status string
}
