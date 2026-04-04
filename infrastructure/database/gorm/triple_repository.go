package gorm

import (
	"context"
	"fmt"

	"weos/domain/repositories"
	"weos/infrastructure/models"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

type TripleRepository struct {
	db *gorm.DB
}

type TripleRepositoryResult struct {
	fx.Out
	Repository repositories.TripleRepository
}

func ProvideTripleRepository(params struct {
	fx.In
	DB *gorm.DB
}) (TripleRepositoryResult, error) {
	return TripleRepositoryResult{
		Repository: &TripleRepository{db: params.DB},
	}, nil
}

func (r *TripleRepository) SaveTriple(
	ctx context.Context, subject, predicate, object string,
) error {
	t := models.Triple{
		Subject:   subject,
		Predicate: predicate,
		Object:    object,
	}
	if err := r.db.WithContext(ctx).
		Where("subject = ? AND predicate = ? AND object = ?", subject, predicate, object).
		FirstOrCreate(&t).Error; err != nil {
		return fmt.Errorf("failed to save triple: %w", err)
	}
	return nil
}

func (r *TripleRepository) DeleteTriple(
	ctx context.Context, subject, predicate, object string,
) error {
	if err := r.db.WithContext(ctx).
		Where("subject = ? AND predicate = ? AND object = ?", subject, predicate, object).
		Delete(&models.Triple{}).Error; err != nil {
		return fmt.Errorf("failed to delete triple: %w", err)
	}
	return nil
}

func (r *TripleRepository) DeleteBySubject(ctx context.Context, subject string) error {
	if err := r.db.WithContext(ctx).
		Where("subject = ?", subject).
		Delete(&models.Triple{}).Error; err != nil {
		return fmt.Errorf("failed to delete triples by subject: %w", err)
	}
	return nil
}

func (r *TripleRepository) DeleteBySubjectAndPredicate(
	ctx context.Context, subject, predicate string,
) error {
	if err := r.db.WithContext(ctx).
		Where("subject = ? AND predicate = ?", subject, predicate).
		Delete(&models.Triple{}).Error; err != nil {
		return fmt.Errorf("failed to delete triples: %w", err)
	}
	return nil
}

func (r *TripleRepository) FindBySubject(
	ctx context.Context, subject string,
) ([]repositories.Triple, error) {
	var triples []models.Triple
	if err := r.db.WithContext(ctx).
		Where("subject = ?", subject).
		Find(&triples).Error; err != nil {
		return nil, fmt.Errorf("failed to find triples by subject: %w", err)
	}
	return toTriples(triples), nil
}

func (r *TripleRepository) FindByObject(
	ctx context.Context, object string,
) ([]repositories.Triple, error) {
	var triples []models.Triple
	if err := r.db.WithContext(ctx).
		Where("object = ?", object).
		Find(&triples).Error; err != nil {
		return nil, fmt.Errorf("failed to find triples by object: %w", err)
	}
	return toTriples(triples), nil
}

func (r *TripleRepository) FindBySubjectAndPredicate(
	ctx context.Context, subject, predicate string,
) ([]repositories.Triple, error) {
	var triples []models.Triple
	if err := r.db.WithContext(ctx).
		Where("subject = ? AND predicate = ?", subject, predicate).
		Find(&triples).Error; err != nil {
		return nil, fmt.Errorf("failed to find triples: %w", err)
	}
	return toTriples(triples), nil
}

func (r *TripleRepository) FindByPredicateAndObject(
	ctx context.Context, predicate, object string,
) ([]repositories.Triple, error) {
	var triples []models.Triple
	if err := r.db.WithContext(ctx).
		Where("predicate = ? AND object = ?", predicate, object).
		Find(&triples).Error; err != nil {
		return nil, fmt.Errorf("failed to find triples: %w", err)
	}
	return toTriples(triples), nil
}

func toTriples(models []models.Triple) []repositories.Triple {
	result := make([]repositories.Triple, len(models))
	for i, m := range models {
		result[i] = repositories.Triple{
			Subject:   m.Subject,
			Predicate: m.Predicate,
			Object:    m.Object,
			CreatedAt: m.CreatedAt,
		}
	}
	return result
}
