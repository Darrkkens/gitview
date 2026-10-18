package report

import (
	"fmt"

	"example.com/repo/store"
)

func Format(u *store.User) string { return fmt.Sprintf("%d", u.ID) }
