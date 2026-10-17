package services

import (
	"context"
	"errors"

	"example.com/shop/internal/models"
	"example.com/shop/internal/repository"
)

var ErrNoPlan = errors.New("usage without plan")

// RatingService computes charges.
type RatingService struct {
	Repo    *repository.UsageRepository
	Pricing *PricingClient
}

// RateUsage computes and stores the charge of a company's usage.
func (s *RatingService) RateUsage(ctx context.Context, companyID int64, period string) (models.Charge, error) {
	usage, plan, err := s.Repo.FindUsage(ctx, companyID, period)
	if err != nil {
		return models.Charge{}, err
	}
	if plan == nil {
		return models.Charge{}, ErrNoPlan
	}
	charge := models.Charge{UsageID: usage.ID, Amount: plan.Price}
	if usage.ZeroRated {
		return charge, nil
	}
	if usage.Total > plan.Quota {
		rate, err := s.Pricing.OverageRate(ctx)
		if err != nil {
			return models.Charge{}, err
		}
		charge.Amount += calculateOverage(usage.Total, plan.Quota, rate)
	}
	if err := s.Repo.SaveCharge(ctx, charge); err != nil {
		return models.Charge{}, err
	}
	return charge, nil
}

// calculateOverage prices the units above the quota.
func calculateOverage(total, quota, rate int64) int64 {
	excess := total - quota
	if excess <= 0 || rate == 0 {
		return 0
	}
	return excess * rate
}
