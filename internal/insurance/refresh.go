package insurance

import (
	"context"
	"log"

	"advancedmd-token-management/internal/safeerrors"
)

type CatalogReader func(ctx context.Context) (revisionID string, catalog []byte, err error)

func Refresh(ctx context.Context, read CatalogReader) error {
	current := active.Load()
	next, err := readRevision(ctx, read, current)
	if err != nil {
		log.Printf("insurance_catalog_refresh_failed current_revision=%s category=%s cause=%q", revisionID(current), safeerrors.Classify(err), err)
		return err
	}
	if next != nil && active.CompareAndSwap(current, next) {
		log.Printf("insurance_catalog_installed revision=%s previous_revision=%s", next.id, revisionID(current))
	}
	return nil
}

func readRevision(ctx context.Context, read CatalogReader, current *revision) (*revision, error) {
	id, body, err := read(ctx)
	if err != nil {
		return nil, err
	}
	if current != nil && current.id == id {
		return nil, nil
	}
	var source InsuranceCatalog
	if err := decodeStrictJSON(body, &source); err != nil {
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "revision %s is not an InsuranceCatalog: %v", id, err)
	}
	lists, errs := parseCatalog(source)
	if len(errs) > 0 {
		first := errs[0]
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "revision %s breaks %d catalog rules, first %s at region=%q table=%q plan=%q: %s", id, len(errs), first.Rule, first.Region, first.Table, first.PlanID, first.Message)
	}
	return &revision{id: id, catalog: source, lists: lists}, nil
}

func revisionID(r *revision) string {
	if r == nil {
		return "none"
	}
	return r.id
}
