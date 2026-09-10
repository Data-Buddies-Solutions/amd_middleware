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

func TestLookupPatientCandidatesReadsLaterPages(t *testing.T) {
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
	read, err := client.LookupPatientCandidates(context.Background(), token, "Jane")
	if err != nil {
		t.Fatal(err)
	}
	if !read.Complete {
		t.Fatal("all pages should be complete")
	}
	patients := read.Patients
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

func TestLookupPatientRetainsRecordsWhenReportedCountIsTooLow(t *testing.T) {
	for _, pages := range []int{1, 2} {
		for _, lookup := range []string{"phone", "first_name"} {
			t.Run(fmt.Sprintf("%s_%d_pages", lookup, pages), func(t *testing.T) {
				reads := 0
				client, token, cleanup := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request AMDLookupRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					reads++
					if request.PPMDMsg.Page != reads || reads > pages {
						t.Fatalf("unexpected page %d", request.PPMDMsg.Page)
					}
					first, last := 1, 5
					if pages == 2 {
						if reads == 1 {
							last = 3
						} else {
							first = 4
						}
					}
					rows := []map[string]any{}
					for id := first; id <= last; id++ {
						rows = append(rows, map[string]any{"@id": fmt.Sprintf("pat%d", id), "@name": "EXAMPLE,JANE", "@dob": "01/15/1980"})
					}
					fixture := lookupPageFixture(reads, pages, 4, "unused")
					fixture["PPMDResults"].(map[string]any)["Results"].(map[string]any)["patientlist"].(map[string]any)["patient"] = rows
					json.NewEncoder(w).Encode(fixture)
				}))
				defer cleanup()
				if lookup == "phone" {
					patients, err := client.LookupPatientByPhone(context.Background(), token, "5551234567")
					if err != nil {
						t.Fatal(err)
					}
					if len(patients) != 5 || patients[4].ID != "5" {
						t.Fatal("phone lookup lost returned candidates")
					}
				} else {
					read, err := client.LookupPatientCandidates(context.Background(), token, "Jane")
					if err != nil {
						t.Fatal(err)
					}
					if len(read.Patients) != 5 || read.Patients[4].ID != "5" {
						t.Fatal("first-name lookup lost returned candidates")
					}
					if read.Complete {
						t.Fatal("inconsistent count must not prove completeness for first-name resolution")
					}
				}
				if reads != pages {
					t.Fatalf("read %d pages, want %d", reads, pages)
				}
			})
		}
	}
}
