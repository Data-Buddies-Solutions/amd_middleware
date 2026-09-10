package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func lookupPageFixture(page, pages, total int, id string) map[string]any {
	return map[string]any{"PPMDResults": map[string]any{"Results": map[string]any{"patientlist": map[string]any{
		"@page": fmt.Sprint(page), "@pagecount": fmt.Sprint(pages), "@itemcount": fmt.Sprint(total),
		"patient": map[string]any{"@id": "pat" + id, "@name": "EXAMPLE,JANE", "@dob": "01/15/1980"},
	}}}}
}

func TestLookupPatientReadsLaterPages(t *testing.T) {
	reads := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Msg map[string]any `json:"ppmdmsg"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		page := 1
		if value, ok := req.Msg["@page"].(float64); ok {
			page = int(value)
		}
		if req.Msg["@name"] != ",Jane" {
			t.Errorf("first-name query changed")
		}
		reads++
		json.NewEncoder(w).Encode(lookupPageFixture(page, 2, 2, fmt.Sprint(page)))
	})
	client, token, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()
	patients, err := client.LookupPatient(context.Background(), token, "", "Jane")
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || len(patients) != 2 || patients[1].ID != "2" {
		t.Fatalf("reads=%d patients=%d; later page missing", reads, len(patients))
	}
}

func TestLookupPatientRejectsIncompletePages(t *testing.T) {
	for _, mode := range []string{"page_failure", "repeated_page", "duplicate_patient", "changed_total", "truncated", "too_many_pages", "legacy_truncated", "legacy_duplicate"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads++
				page := reads
				pages, total := 2, 2
				id := fmt.Sprint(page)
				if mode == "too_many_pages" {
					pages = 101
					total = 101
				}
				if page == 2 {
					switch mode {
					case "page_failure":
						w.WriteHeader(http.StatusBadGateway)
						return
					case "repeated_page":
						page = 1
					case "duplicate_patient":
						id = "1"
					case "changed_total":
						total = 3
					case "truncated":
						total = 3
					}
				}
				if mode == "truncated" {
					total = 3
				}
				fixture := lookupPageFixture(page, pages, total, id)
				if mode == "legacy_truncated" || mode == "legacy_duplicate" {
					list := fixture["PPMDResults"].(map[string]any)["Results"].(map[string]any)["patientlist"].(map[string]any)
					delete(list, "@page")
					delete(list, "@pagecount")
					if mode == "legacy_duplicate" {
						list["patient"] = []any{list["patient"], list["patient"]}
					}
				}
				json.NewEncoder(w).Encode(fixture)
			})
			client, token, cleanup := newTestXMLRPCClient(t, handler)
			defer cleanup()
			patients, err := client.LookupPatient(context.Background(), token, "", "Jane")
			if err == nil || len(patients) != 0 {
				t.Fatalf("incomplete lookup returned patients: count=%d err=%v", len(patients), err)
			}
		})
	}
}
