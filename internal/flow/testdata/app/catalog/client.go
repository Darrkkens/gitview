package catalog

// GetClientByID is a homonym in another package, with no callers.
func GetClientByID(id int) string {
	return lookup(id)
}

func lookup(id int) string { return "" }
