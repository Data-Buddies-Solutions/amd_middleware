package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"advancedmd-token-management/internal/domain"
)

// ParseDateTime parses an AMD datetime string trying multiple known formats.
// Returns timezone-stripped wall-clock time for consistent comparison with slot times.
func ParseDateTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty datetime string")
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(),
				t.Hour(), t.Minute(), t.Second(), 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse datetime %q", s)
}

// AdvancedMDRestClient handles REST API calls to AdvancedMD.
type AdvancedMDRestClient struct {
	httpClient *http.Client
}

// NewAdvancedMDRestClient creates a new AdvancedMD REST client.
func NewAdvancedMDRestClient(httpClient *http.Client) *AdvancedMDRestClient {
	return &AdvancedMDRestClient{httpClient: httpClient}
}

// AMDAppointmentResponse represents a single appointment from the REST API.
type AMDAppointmentResponse struct {
	ID               int     `json:"id"`
	StartDateTime    string  `json:"startdatetime"`
	Duration         int     `json:"duration"`
	ColumnID         int     `json:"columnid"`
	ProfileID        int     `json:"profileid"`
	Provider         string  `json:"provider"`
	Heading          string  `json:"heading"`
	Facility         string  `json:"facility"`
	FacilityID       int     `json:"facilityid"`
	AppointmentTypes []int   `json:"appointmenttypeids"`
	PatientID        int     `json:"patientid"`
	FirstName        string  `json:"firstname"`
	LastName         string  `json:"lastname"`
	ConfirmDate      *string `json:"confirmdate"`
	ConfirmMethod    *string `json:"confirmmethod"`
}

// GetAppointments fetches appointments for a column within a date range.
// startDate should be in YYYY-MM-DD format.
func (c *AdvancedMDRestClient) GetAppointments(ctx context.Context, tokenData *domain.TokenData, columnID string, startDate string) ([]domain.Appointment, error) {
	url := fmt.Sprintf("https://%s/scheduler/appointments?columnId=%s&forView=day&isLegacy=true&startDate=%s",
		tokenData.RestApiBase, columnID, startDate)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle AMD single-vs-array response quirk
	var amdAppts []AMDAppointmentResponse
	if err := json.Unmarshal(body, &amdAppts); err != nil {
		var single AMDAppointmentResponse
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			return nil, fmt.Errorf("failed to parse appointments (array: %v, single: %v)", err, err2)
		}
		amdAppts = []AMDAppointmentResponse{single}
	}

	var appointments []domain.Appointment
	for _, a := range amdAppts {
		startTime, err := ParseDateTime(a.StartDateTime)
		if err != nil {
			log.Printf("WARNING: skipping appointment %d in column %s — %v", a.ID, columnID, err)
			continue
		}

		appointments = append(appointments, domain.Appointment{
			ID:            a.ID,
			StartDateTime: startTime,
			Duration:      a.Duration,
			ColumnID:      a.ColumnID,
			ProfileID:     a.ProfileID,
			PatientID:     a.PatientID,
		})
	}

	return appointments, nil
}

// GetAppointmentsForColumns fetches appointments for multiple columns concurrently.
// Per-column errors are logged and the column is omitted from results (callers should
// check key presence before using data — absent key means fetch failed).
func (c *AdvancedMDRestClient) GetAppointmentsForColumns(ctx context.Context, tokenData *domain.TokenData, columnIDs []string, startDate string) map[string][]domain.Appointment {
	result := make(map[string][]domain.Appointment)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, colID := range columnIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			appts, err := c.GetAppointments(ctx, tokenData, id, startDate)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("WARNING: failed to get appointments for column %s: %v", id, err)
				return
			}
			result[id] = appts
		}(colID)
	}

	wg.Wait()
	return result
}

// AMDBlockHoldResponse represents a block hold from the REST API.
type AMDBlockHoldResponse struct {
	ID            int    `json:"id"`
	StartDateTime string `json:"startdatetime"`
	EndDateTime   string `json:"enddatetime"`
	Duration      int    `json:"duration"`
	ColumnID      int    `json:"columnid"`
	Note          string `json:"note"`
	Recurrence    struct {
		RecurrenceType int `json:"recurrencetype"`
	} `json:"recurrence"`
}

// GetBlockHolds fetches block holds for a column within a date range.
// startDate should be in YYYY-MM-DD format.
func (c *AdvancedMDRestClient) GetBlockHolds(ctx context.Context, tokenData *domain.TokenData, columnID string, startDate string) ([]domain.BlockHold, error) {
	url := fmt.Sprintf("https://%s/scheduler/blockholds?columnId=%s&forView=day&startDate=%s",
		tokenData.RestApiBase, columnID, startDate)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle AMD single-vs-array response quirk
	var amdHolds []AMDBlockHoldResponse
	if err := json.Unmarshal(body, &amdHolds); err != nil {
		var single AMDBlockHoldResponse
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			return nil, fmt.Errorf("failed to parse block holds (array: %v, single: %v)", err, err2)
		}
		amdHolds = []AMDBlockHoldResponse{single}
	}

	var holds []domain.BlockHold
	for _, h := range amdHolds {
		startTime, err := ParseDateTime(h.StartDateTime)
		if err != nil {
			log.Printf("WARNING: skipping block hold %d — %v", h.ID, err)
			continue
		}

		endTime, err := ParseDateTime(h.EndDateTime)
		if err != nil {
			endTime = startTime.Add(time.Duration(h.Duration) * time.Minute)
		}
		// For recurring holds, AMD's enddatetime is the recurrence series end,
		// not the end of this day's occurrence.
		if h.Recurrence.RecurrenceType > 0 && h.Duration > 0 {
			endTime = startTime.Add(time.Duration(h.Duration) * time.Minute)
		}

		holds = append(holds, domain.BlockHold{
			ID:            h.ID,
			StartDateTime: startTime,
			EndDateTime:   endTime,
			ColumnID:      h.ColumnID,
			Note:          h.Note,
		})
	}

	return holds, nil
}

// GetBlockHoldsForColumns fetches block holds for multiple columns concurrently.
// Per-column errors are logged and the column is omitted from results.
func (c *AdvancedMDRestClient) GetBlockHoldsForColumns(ctx context.Context, tokenData *domain.TokenData, columnIDs []string, startDate string) map[string][]domain.BlockHold {
	result := make(map[string][]domain.BlockHold)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, colID := range columnIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			holds, err := c.GetBlockHolds(ctx, tokenData, id, startDate)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("WARNING: failed to get block holds for column %s: %v", id, err)
				return
			}
			result[id] = holds
		}(colID)
	}

	wg.Wait()
	return result
}

// GetAppointmentsByMonth fetches all appointments for the given columns for a full month.
// columnIDs should be dash-separated (e.g., "1513-1550-1551").
// startDate should be the first of the month in YYYY-MM-DD format.
func (c *AdvancedMDRestClient) GetAppointmentsByMonth(ctx context.Context, tokenData *domain.TokenData, columnIDs string, startDate string) ([]AMDAppointmentResponse, error) {
	url := fmt.Sprintf("https://%s/scheduler/appointments?columnId=%s&forView=month&isLegacy=true&startDate=%s",
		tokenData.RestApiBase, columnIDs, startDate)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var appts []AMDAppointmentResponse
	if err := json.Unmarshal(body, &appts); err != nil {
		return nil, fmt.Errorf("failed to parse appointments: %w", err)
	}

	return appts, nil
}

// BookAppointmentParams holds the parameters for booking an appointment.
type BookAppointmentParams struct {
	PatientID       int    `json:"patientid"`
	ColumnID        int    `json:"columnid"`
	ProfileID       int    `json:"profileid"`
	StartDatetime   string `json:"startdatetime"`
	Duration        int    `json:"duration"`
	AppointmentType []struct {
		ID int `json:"id"`
	} `json:"type"`
	EpisodeID  int    `json:"episodeid"`
	FacilityID int    `json:"facilityid"`
	Color      string `json:"color"`
	Force      int    `json:"force,omitempty"`
}

// BookAppointmentResponse represents the AMD response after booking.
type BookAppointmentResponse struct {
	ID int `json:"id"`
}

// BookAppointment creates an appointment via AMD's REST API.
// Returns the appointment ID on success.
func (c *AdvancedMDRestClient) BookAppointment(ctx context.Context, tokenData *domain.TokenData, params BookAppointmentParams) (int, error) {
	url := fmt.Sprintf("https://%s/scheduler/Appointments", tokenData.RestApiBase)

	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		return 0, fmt.Errorf("conflict: %s", string(body))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result BookAppointmentResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.ID, nil
}

// CancelAppointment cancels an appointment via AMD's REST API.
func (c *AdvancedMDRestClient) CancelAppointment(ctx context.Context, tokenData *domain.TokenData, appointmentID int) error {
	url := fmt.Sprintf("https://%s/scheduler/appointments/%d/cancel",
		tokenData.RestApiBase, appointmentID)

	reqBody := map[string]interface{}{
		"id": appointmentID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
