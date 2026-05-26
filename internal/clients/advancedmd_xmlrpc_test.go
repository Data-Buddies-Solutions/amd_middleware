package clients

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
)

// newTestXMLRPCClient creates a TLS test server and XMLRPC client wired together.
func newTestXMLRPCClient(t *testing.T, handler http.Handler) (*AdvancedMDClient, *domain.TokenData, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Strip "https://" to match XmlrpcURL format (doXMLRPCRequest adds it back)
	xmlrpcURL := server.URL[8:]

	tokenData := &domain.TokenData{
		Token:       "Bearer test-token",
		CookieToken: "token=test-token",
		XmlrpcURL:   xmlrpcURL,
	}

	return NewAdvancedMDClient(httpClient), tokenData, server.Close
}

func TestAdvancedMDClient_LookupPatient_SingleResult(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "token=test-token" {
			t.Errorf("Expected Cookie 'token=test-token', got %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"@itemcount": "1",
						"patient": {
							"@id": "pat123",
							"@name": "SMITH,JOHN",
							"@dob": "01/15/1980",
							"@gender": "M",
							"@chart": "12345",
							"contactinfo": {
								"@homephone": "555-123-4567"
							}
						}
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	patients, err := client.LookupPatient(context.Background(), tokenData, "Smith", "")
	if err != nil {
		t.Fatalf("LookupPatient failed: %v", err)
	}

	if len(patients) != 1 {
		t.Fatalf("Expected 1 patient, got %d", len(patients))
	}

	p := patients[0]
	if p.ID != "123" {
		t.Errorf("Expected ID '123' (stripped), got %q", p.ID)
	}
	if p.FullName != "SMITH,JOHN" {
		t.Errorf("Expected FullName 'SMITH,JOHN', got %q", p.FullName)
	}
	if p.FirstName != "JOHN" {
		t.Errorf("Expected FirstName 'JOHN', got %q", p.FirstName)
	}
	if p.DOB != "01/15/1980" {
		t.Errorf("Expected DOB '01/15/1980', got %q", p.DOB)
	}
	if p.Phone != "555-123-4567" {
		t.Errorf("Expected Phone '555-123-4567', got %q", p.Phone)
	}
}

func TestAdvancedMDClient_LookupPatient_MultipleResults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"@itemcount": "2",
						"patient": [
							{
								"@id": "pat123",
								"@name": "SMITH,JOHN",
								"@dob": "01/15/1980",
								"contactinfo": {"@homephone": "555-111-1111"}
							},
							{
								"@id": "pat456",
								"@name": "SMITH,JANE",
								"@dob": "01/15/1980",
								"contactinfo": {"@homephone": "555-222-2222"}
							}
						]
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	patients, err := client.LookupPatient(context.Background(), tokenData, "Smith", "")
	if err != nil {
		t.Fatalf("LookupPatient failed: %v", err)
	}

	if len(patients) != 2 {
		t.Fatalf("Expected 2 patients, got %d", len(patients))
	}

	if patients[0].FirstName != "JOHN" {
		t.Errorf("Expected first patient FirstName 'JOHN', got %q", patients[0].FirstName)
	}
	if patients[1].FirstName != "JANE" {
		t.Errorf("Expected second patient FirstName 'JANE', got %q", patients[1].FirstName)
	}
}

func TestAdvancedMDClient_LookupPatient_NoResults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"@itemcount": "0"
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	patients, err := client.LookupPatient(context.Background(), tokenData, "NoSuchName", "")
	if err != nil {
		t.Fatalf("LookupPatient failed: %v", err)
	}

	if len(patients) != 0 {
		t.Errorf("Expected 0 patients, got %d", len(patients))
	}
}

func TestAdvancedMDClient_LookupPatient_RequestPayload(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload AMDLookupRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if payload.PPMDMsg.Action != "lookuppatient" {
			t.Errorf("Expected action 'lookuppatient', got %q", payload.PPMDMsg.Action)
		}
		if payload.PPMDMsg.Class != "api" {
			t.Errorf("Expected class 'api', got %q", payload.PPMDMsg.Class)
		}
		if payload.PPMDMsg.Name != "Smith" {
			t.Errorf("Expected name 'Smith', got %q", payload.PPMDMsg.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Results":{"patientlist":{"@itemcount":"0"}}}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	_, err := client.LookupPatient(context.Background(), tokenData, "Smith", "")
	if err != nil {
		t.Fatalf("LookupPatient failed: %v", err)
	}
}

func TestAdvancedMDClient_LookupPatient_HTTPError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`upstream unavailable`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	_, err := client.LookupPatient(context.Background(), tokenData, "Smith", "")
	if err == nil {
		t.Fatal("Expected error for non-2xx XMLRPC response, got nil")
	}
}

func TestAdvancedMDClient_LookupPatient_MalformedResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Results":{"patientlist":{}}}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	_, err := client.LookupPatient(context.Background(), tokenData, "Smith", "")
	if err == nil {
		t.Fatal("Expected error for malformed lookup response, got nil")
	}
}

func TestAdvancedMDClient_AddPatient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat789",
							"@name": "DOE,JANE",
							"@respparty": "resp123"
						}
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	patientID, respPartyID, name, err := client.AddPatient(context.Background(), tokenData, AddPatientParams{
		FirstName: "Jane",
		LastName:  "Doe",
		DOB:       "03/20/1990",
		Phone:     "(555)123-4567",
		Email:     "jane@example.com",
		Street:    "123 Main St",
		City:      "Springfield",
		State:     "FL",
		Zip:       "33333",
		Sex:       "F",
	})
	if err != nil {
		t.Fatalf("AddPatient failed: %v", err)
	}

	if patientID != "pat789" {
		t.Errorf("Expected patientID 'pat789', got %q", patientID)
	}
	if respPartyID != "resp123" {
		t.Errorf("Expected respPartyID 'resp123', got %q", respPartyID)
	}
	if name != "DOE,JANE" {
		t.Errorf("Expected name 'DOE,JANE', got %q", name)
	}
}

func TestAdvancedMDClient_AddPatient_AMDError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Error": {"@message": "Duplicate patient detected"}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	_, _, _, err := client.AddPatient(context.Background(), tokenData, AddPatientParams{
		FirstName: "Jane",
		LastName:  "Doe",
		DOB:       "03/20/1990",
		Phone:     "(555)123-4567",
		Email:     "jane@example.com",
		Street:    "123 Main St",
		City:      "Springfield",
		State:     "FL",
		Zip:       "33333",
		Sex:       "F",
	})

	if err == nil {
		t.Fatal("Expected error for AMD error response, got nil")
	}
}

func TestAdvancedMDClient_AddInsurance(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Results":{}}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	err := client.AddInsurance(context.Background(), tokenData, "pat123", "resp123", "car40906", "SUB12345")
	if err != nil {
		t.Fatalf("AddInsurance failed: %v", err)
	}
}

func TestAdvancedMDClient_AddInsurance_Error(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Error":"Insurance attachment failed"}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	err := client.AddInsurance(context.Background(), tokenData, "pat123", "resp123", "car40906", "SUB12345")
	if err == nil {
		t.Fatal("Expected error for failed insurance attachment, got nil")
	}
}

func TestAdvancedMDClient_SavePatientNote(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		msg := payload["ppmdmsg"].(map[string]interface{})
		if msg["@action"] != "savepatientnotes" {
			t.Errorf("action = %q, want savepatientnotes", msg["@action"])
		}
		if msg["@class"] != "api" {
			t.Errorf("class = %q, want api", msg["@class"])
		}
		if msg["@id"] != "123" {
			t.Errorf("id = %q, want stripped patient ID 123", msg["@id"])
		}
		if msg["@useclienttime"] != "1" {
			t.Errorf("useclienttime = %q, want 1", msg["@useclienttime"])
		}

		masterfile := msg["masterfile"].(map[string]interface{})
		expected := map[string]string{
			"@uid":         "",
			"@patientfid":  "123",
			"@profilefid":  "620",
			"@notetypefid": "532",
			"@note":        "Patient called to reschedule.",
		}
		for key, want := range expected {
			if got := masterfile[key]; got != want {
				t.Errorf("masterfile[%s] = %q, want %q", key, got, want)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"@newid":"3135521","record":{"@uid":"3135521"}}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	noteID, err := client.SavePatientNote(context.Background(), tokenData, SavePatientNoteParams{
		PatientID: "pat123",
		ProfileID: "620",
		Note:      "Patient called to reschedule.",
	})
	if err != nil {
		t.Fatalf("SavePatientNote failed: %v", err)
	}
	if noteID != "3135521" {
		t.Errorf("noteID = %q, want 3135521", noteID)
	}
}

func TestAdvancedMDClient_SavePatientNote_Error(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Error": {
					"Fault": {
						"faultstring": "Server Error",
						"detail": {
							"description": "The INSERT statement conflicted with the FOREIGN KEY constraint"
						}
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	_, err := client.SavePatientNote(context.Background(), tokenData, SavePatientNoteParams{
		PatientID: "123",
		ProfileID: "620",
		Note:      "Patient called.",
	})
	if err == nil {
		t.Fatal("Expected error for failed note save, got nil")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint") {
		t.Errorf("Expected FK error detail, got %v", err)
	}
}

func TestAdvancedMDClient_GetDemographic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat123",
							"@respparty": "resp456",
							"@dob": "08/18/2000",
							"insplanlist": {
								"insplan": {
									"@id": "ins789",
									"@carrier": "car40906",
									"@subscriber": "resp456",
									"@enddate": "",
									"@coverage": "1"
								}
							}
						}
					},
					"carrierlist": {
						"carrier": {
							"@id": "car40906",
							"@name": "HUMANA MEDICARE"
						}
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	result, err := client.GetDemographic(context.Background(), tokenData, "pat123")
	if err != nil {
		t.Fatalf("GetDemographic failed: %v", err)
	}

	if result.CarrierName != "HUMANA MEDICARE" {
		t.Errorf("Expected carrier name 'HUMANA MEDICARE', got %q", result.CarrierName)
	}
	if result.CarrierID != "car40906" {
		t.Errorf("Expected carrier ID 'car40906', got %q", result.CarrierID)
	}
	if result.InsPlanID != "ins789" {
		t.Errorf("Expected insplan ID 'ins789', got %q", result.InsPlanID)
	}
	if result.RespPartyID != "resp456" {
		t.Errorf("Expected resp party ID 'resp456', got %q", result.RespPartyID)
	}
}

func TestAdvancedMDClient_GetDemographic_NoInsurance(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat123",
							"@respparty": "resp456"
						}
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	result, err := client.GetDemographic(context.Background(), tokenData, "pat123")
	if err != nil {
		t.Fatalf("GetDemographic failed: %v", err)
	}

	if result.CarrierName != "" {
		t.Errorf("Expected empty carrier name, got %q", result.CarrierName)
	}
	if result.CarrierID != "" {
		t.Errorf("Expected empty carrier ID, got %q", result.CarrierID)
	}
	if result.RespPartyID != "resp456" {
		t.Errorf("Expected resp party ID 'resp456' from patient, got %q", result.RespPartyID)
	}
}

func TestAdvancedMDClient_GetDemographic_MultipleCarriers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat123",
							"@respparty": "resp456",
							"insplanlist": {
								"insplan": {"@id": "ins100", "@carrier": "car40906", "@enddate": "", "@coverage": "1", "@subscriber": "resp456"}
							}
						}
					},
					"carrierlist": {
						"carrier": [
							{"@id": "car40887", "@name": "AETNA"},
							{"@id": "car40906", "@name": "HUMANA MEDICARE"}
						]
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	result, err := client.GetDemographic(context.Background(), tokenData, "pat123")
	if err != nil {
		t.Fatalf("GetDemographic failed: %v", err)
	}

	if result.CarrierName != "HUMANA MEDICARE" {
		t.Errorf("Expected carrier name 'HUMANA MEDICARE', got %q", result.CarrierName)
	}
	if result.CarrierID != "car40906" {
		t.Errorf("Expected carrier ID 'car40906', got %q", result.CarrierID)
	}
}

func TestAdvancedMDClient_GetDemographic_MultiplePlansPicksActive(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat123",
							"@respparty": "resp456",
							"insplanlist": {
								"insplan": [
									{"@id": "ins100", "@carrier": "car40887", "@enddate": "04/08/2026", "@coverage": "1", "@subscriber": "resp456"},
									{"@id": "ins200", "@carrier": "car40906", "@enddate": "", "@coverage": "1", "@subscriber": "resp456"}
								]
							}
						}
					},
					"carrierlist": {
						"carrier": [
							{"@id": "car40887", "@name": "AETNA"},
							{"@id": "car40906", "@name": "HUMANA MEDICARE"}
						]
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	result, err := client.GetDemographic(context.Background(), tokenData, "pat123")
	if err != nil {
		t.Fatalf("GetDemographic failed: %v", err)
	}

	if result.InsPlanID != "ins200" {
		t.Errorf("Expected active insplan ID 'ins200', got %q", result.InsPlanID)
	}
	if result.CarrierID != "car40906" {
		t.Errorf("Expected active carrier ID 'car40906', got %q", result.CarrierID)
	}
	if result.CarrierName != "HUMANA MEDICARE" {
		t.Errorf("Expected carrier name 'HUMANA MEDICARE', got %q", result.CarrierName)
	}
}

func TestAdvancedMDClient_EndDateInsurance(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Results":{"@success":"1","patient":{"@insorder":""}}}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	err := client.EndDateInsurance(context.Background(), tokenData, "pat123", "ins789")
	if err != nil {
		t.Fatalf("EndDateInsurance failed: %v", err)
	}
}

func TestAdvancedMDClient_EndDateInsurance_Error(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"PPMDResults":{"Error":"Insurance record not found"}}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	err := client.EndDateInsurance(context.Background(), tokenData, "pat123", "ins789")
	if err == nil {
		t.Fatal("Expected error for failed end-date, got nil")
	}
}

func TestAdvancedMDClient_GetSchedulerSetup(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"columnlist": {
						"column": [
							{
								"@id": "col1513",
								"@name": "DR. BACH - BP",
								"@profile": "prof620",
								"@facility": "fac1568",
								"columnsetting": {
									"@start": "0800",
									"@end": "1700",
									"@interval": "15",
									"@maxapptsperslot": "0",
									"@workweek": "1111100"
								}
							}
						]
					},
					"profilelist": {
						"profile": [
							{"@id": "prof620", "@code": "ABCH", "@name": "BACH, AUSTIN"}
						]
					},
					"facilitylist": {
						"facility": [
							{"@id": "fac1568", "@code": "ABSPR", "@name": "ABITA EYE GROUP SPRING HILL"}
						]
					}
				}
			}
		}`))
	})

	client, tokenData, cleanup := newTestXMLRPCClient(t, handler)
	defer cleanup()

	setup, err := client.GetSchedulerSetup(context.Background(), tokenData)
	if err != nil {
		t.Fatalf("GetSchedulerSetup failed: %v", err)
	}

	if len(setup.Columns) != 1 {
		t.Fatalf("Expected 1 column, got %d", len(setup.Columns))
	}
	col := setup.Columns[0]
	if col.ID != "1513" {
		t.Errorf("Expected column ID '1513' (stripped), got %q", col.ID)
	}
	if col.ProfileID != "620" {
		t.Errorf("Expected profile ID '620' (stripped), got %q", col.ProfileID)
	}
	if col.FacilityID != "1568" {
		t.Errorf("Expected facility ID '1568' (stripped), got %q", col.FacilityID)
	}
	if col.StartTime != "08:00" {
		t.Errorf("Expected start time '08:00', got %q", col.StartTime)
	}
	if col.EndTime != "17:00" {
		t.Errorf("Expected end time '17:00', got %q", col.EndTime)
	}
	if col.Interval != 15 {
		t.Errorf("Expected interval 15, got %d", col.Interval)
	}

	if len(setup.Profiles) != 1 {
		t.Fatalf("Expected 1 profile, got %d", len(setup.Profiles))
	}
	if setup.Profiles[0].ID != "620" {
		t.Errorf("Expected profile ID '620', got %q", setup.Profiles[0].ID)
	}
	if setup.Profiles[0].Name != "BACH, AUSTIN" {
		t.Errorf("Expected profile name 'BACH, AUSTIN', got %q", setup.Profiles[0].Name)
	}

	if len(setup.Facilities) != 1 {
		t.Fatalf("Expected 1 facility, got %d", len(setup.Facilities))
	}
	if setup.Facilities[0].ID != "1568" {
		t.Errorf("Expected facility ID '1568', got %q", setup.Facilities[0].ID)
	}
}

func TestConvertPatients(t *testing.T) {
	amdPatients := []AMDPatient{
		{
			ID:          "pat100",
			Name:        "DOE,JANE",
			DOB:         "03/20/1990",
			ContactInfo: AMDContactInfo{HomePhone: "555-999-8888"},
		},
	}

	patients := convertPatients(amdPatients)

	if len(patients) != 1 {
		t.Fatalf("Expected 1 patient, got %d", len(patients))
	}

	p := patients[0]
	if p.ID != "100" {
		t.Errorf("Expected ID '100' (stripped), got %q", p.ID)
	}
	if p.FullName != "DOE,JANE" {
		t.Errorf("Expected FullName 'DOE,JANE', got %q", p.FullName)
	}
	if p.FirstName != "JANE" {
		t.Errorf("Expected FirstName 'JANE', got %q", p.FirstName)
	}
	if p.DOB != "03/20/1990" {
		t.Errorf("Expected DOB '03/20/1990', got %q", p.DOB)
	}
	if p.Phone != "555-999-8888" {
		t.Errorf("Expected Phone '555-999-8888', got %q", p.Phone)
	}
}

func TestConvertPatients_PrefersCellPhone(t *testing.T) {
	amdPatients := []AMDPatient{
		{
			ID:   "pat100",
			Name: "DOE,JANE",
			DOB:  "03/20/1990",
			ContactInfo: AMDContactInfo{
				HomePhone: "555-999-8888",
				CellPhone: "555-111-2222",
			},
		},
	}

	patients := convertPatients(amdPatients)

	if got := patients[0].Phone; got != "555-111-2222" {
		t.Errorf("Phone = %q, want cell phone", got)
	}
}
