package scheduling

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

func TestObjectClaimsUseAtomicCreateAndReadExistingReceipt(t *testing.T) {
	record := RescheduleRecord{Selection: "slot", Receipt: RescheduleReceipt{Status: "uncertain", Message: "pending"}}
	claims, reads, saves := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			reads++
			json.NewEncoder(w).Encode(record)
			return
		}
		if r.URL.Query().Get("ifGenerationMatch") == "0" {
			claims++
			if claims > 1 {
				w.WriteHeader(http.StatusPreconditionFailed)
				w.Write([]byte(`{"error":{"code":412,"message":"exists"}}`))
				return
			}
		} else {
			saves++
		}
		w.Write([]byte(`{"name":"reschedules/key","generation":"1"}`))
	}))
	defer server.Close()
	service, err := storage.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	store := &ObjectRescheduleStore{objects: service.Objects, bucket: "private-test"}
	if _, created, err := store.Claim(context.Background(), "key", record); err != nil || !created {
		t.Fatalf("claim: %v %v", created, err)
	}
	old, created, err := store.Claim(context.Background(), "key", record)
	if err != nil || created || old.Selection != "slot" {
		t.Fatalf("replay: %+v %v %v", old, created, err)
	}
	if err := store.Save(context.Background(), "key", record); err != nil {
		t.Fatal(err)
	}
	if claims != 2 || reads != 1 || saves != 1 {
		t.Fatalf("requests: %d %d %d", claims, reads, saves)
	}
}

func TestObjectClaimStorageFailureNeverGrantsOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"code":403,"message":"denied"}}`))
	}))
	defer server.Close()
	service, err := storage.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	store := &ObjectRescheduleStore{objects: service.Objects, bucket: "private-test"}
	if _, created, err := store.Claim(context.Background(), "key", RescheduleRecord{}); err == nil || created {
		t.Fatalf("unsafe claim: %v %v", created, err)
	}
}
