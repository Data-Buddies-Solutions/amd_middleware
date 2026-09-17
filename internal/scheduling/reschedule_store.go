package scheduling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

// ObjectRescheduleStore uses GCS create-if-absent for cross-instance claims.
// Use a private, environment-specific bucket with no automatic deletion of
// claims. Stored receipts contain patient data and require restricted access.
type ObjectRescheduleStore struct {
	objects *storage.ObjectsService
	bucket  string
}

func NewObjectRescheduleStore(ctx context.Context, bucket string) (*ObjectRescheduleStore, error) {
	service, err := storage.NewService(ctx)
	if err != nil {
		return nil, err
	}
	return &ObjectRescheduleStore{objects: service.Objects, bucket: bucket}, nil
}

func (s *ObjectRescheduleStore) Claim(ctx context.Context, key string, record RescheduleRecord) (RescheduleRecord, bool, error) {
	body, err := json.Marshal(record)
	if err != nil {
		return RescheduleRecord{}, false, err
	}
	_, err = s.objects.Insert(s.bucket, &storage.Object{Name: "reschedules/" + key, ContentType: "application/json"}).IfGenerationMatch(0).Media(bytes.NewReader(body)).Context(ctx).Do()
	if err == nil {
		return record, true, nil
	}
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != 412 {
		return RescheduleRecord{}, false, err
	}
	response, err := s.objects.Get(s.bucket, "reschedules/"+key).Context(ctx).Download()
	if err != nil {
		return RescheduleRecord{}, false, err
	}
	defer response.Body.Close()
	var existing RescheduleRecord
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&existing)
	if err == nil && (existing.Selection == "" || existing.Receipt.Status == "") {
		err = fmt.Errorf("invalid reschedule record")
	}
	return existing, false, err
}

func (s *ObjectRescheduleStore) Save(ctx context.Context, key string, record RescheduleRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	// Only the successful claimant writes. No lease expiry or worker takeover.
	_, err = s.objects.Insert(s.bucket, &storage.Object{Name: "reschedules/" + key, ContentType: "application/json"}).Media(bytes.NewReader(body)).Context(ctx).Do()
	return err
}
