package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"advancedmd-token-management/internal/domain"
)

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

type AdvancedMDRestClient struct {
	httpClient *http.Client
}

func NewAdvancedMDRestClient(httpClient *http.Client) *AdvancedMDRestClient {
	return &AdvancedMDRestClient{httpClient: httpClient}
}

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

func (c *AdvancedMDRestClient) GetAppointments(ctx context.Context, tokenData *domain.TokenData, columnID string, startDate string) (appointmentsResult []domain.Appointment, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_appointments")
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/appointments?columnId=%s&forView=day&isLegacy=true&startDate=%s",
		tokenData.RestApiBase, columnID, startDate)

	body, err := c.getResponseBody(ctx, tokenData, url, "appointments")
	if err != nil {
		return nil, err
	}

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
			return nil, fmt.Errorf("failed to parse appointment start time: %w", err)
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

func (c *AdvancedMDRestClient) GetBlockHolds(ctx context.Context, tokenData *domain.TokenData, columnID string, startDate string) (holdsResult []domain.BlockHold, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_block_holds")
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/blockholds?columnId=%s&forView=day&startDate=%s",
		tokenData.RestApiBase, columnID, startDate)

	body, err := c.getResponseBody(ctx, tokenData, url, "block holds")
	if err != nil {
		return nil, err
	}

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
			return nil, fmt.Errorf("failed to parse block hold start time: %w", err)
		}

		endTime, err := ParseDateTime(h.EndDateTime)
		if err != nil {
			endTime = startTime.Add(time.Duration(h.Duration) * time.Minute)
		}
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

func (c *AdvancedMDRestClient) GetAppointmentsByMonth(ctx context.Context, tokenData *domain.TokenData, columnIDs string, startDate string) (appointmentsResult []AMDAppointmentResponse, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_appointments_by_month")
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/appointments?columnId=%s&forView=month&isLegacy=true&startDate=%s",
		tokenData.RestApiBase, columnIDs, startDate)

	body, err := c.getResponseBody(ctx, tokenData, url, "monthly appointments")
	if err != nil {
		return nil, err
	}

	var appts []AMDAppointmentResponse
	if err := json.Unmarshal(body, &appts); err != nil {
		return nil, fmt.Errorf("failed to parse appointments: %w", err)
	}

	return appts, nil
}

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
	Comments   string `json:"comments,omitempty"`
}

type BookAppointmentResponse struct {
	ID int `json:"id"`
}

func (c *AdvancedMDRestClient) BookAppointment(ctx context.Context, tokenData *domain.TokenData, params BookAppointmentParams) (appointmentIDResult int, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "book_appointment")
	defer func() { finish(resultErr) }()
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
		return 0, newMutationError(
			MutationDispositionAmbiguous,
			fmt.Errorf("request failed: %w", err),
		)
	}
	defer resp.Body.Close()
	observeProviderStatus(ctx, resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, newMutationError(
			mutationDispositionForStatus(resp.StatusCode),
			&HTTPStatusError{StatusCode: resp.StatusCode},
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, newMutationError(
			MutationDispositionAmbiguous,
			fmt.Errorf("failed to read response: %w", err),
		)
	}

	var result BookAppointmentResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, newMutationError(
			MutationDispositionAmbiguous,
			fmt.Errorf("failed to parse response: %w", err),
		)
	}
	if result.ID <= 0 {
		return 0, newMutationError(
			MutationDispositionAmbiguous,
			fmt.Errorf("invalid booking response"),
		)
	}

	return result.ID, nil
}

func (c *AdvancedMDRestClient) CancelAppointment(ctx context.Context, tokenData *domain.TokenData, appointmentID int) (resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "cancel_appointment")
	defer func() { finish(resultErr) }()
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
		return newMutationError(
			MutationDispositionAmbiguous,
			fmt.Errorf("request failed: %w", err),
		)
	}
	defer resp.Body.Close()
	observeProviderStatus(ctx, resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newMutationError(
			mutationDispositionForStatus(resp.StatusCode),
			&HTTPStatusError{StatusCode: resp.StatusCode},
		)
	}

	return nil
}

func (c *AdvancedMDRestClient) getResponseBody(ctx context.Context, tokenData *domain.TokenData, url, operation string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	observeProviderStatus(ctx, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf(
			"WARNING: provider=advancedmd operation=%q upstream_http_status=%d",
			operation, resp.StatusCode,
		)
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return body, nil
}
