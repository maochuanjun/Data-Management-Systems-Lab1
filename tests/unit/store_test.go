package unit_test

import (
	"context"
	"questionnaire/internal/domain"
	"questionnaire/internal/store"
	"testing"
)

func TestStatsRejectsInvalidPagination(t *testing.T) {
	// Invalid parameters must be rejected before any database access.
	repo := &store.Store{}
	for _, page := range [][2]int{{-1, 20}, {0, -1}, {0, 51}} {
		if _, err := repo.Stats(context.Background(), &domain.Survey{}, page[0], page[1]); err == nil {
			t.Errorf("accepted offset=%d limit=%d", page[0], page[1])
		}
	}
}
