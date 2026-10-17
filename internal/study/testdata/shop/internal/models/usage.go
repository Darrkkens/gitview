package models

// Usage is the consumption of a company in a period.
type Usage struct {
	ID        int64
	CompanyID int64
	Period    string
	Total     int64
	ZeroRated bool
	PlanID    int64
}

// Plan is a price plan.
type Plan struct {
	ID    int64
	Quota int64
	Price int64
}

// Charge is the amount billed for a usage.
type Charge struct {
	UsageID int64
	Amount  int64
}
