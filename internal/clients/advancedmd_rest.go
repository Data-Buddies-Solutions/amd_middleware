package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
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
	ID               int    `json:"id"`
	StartDateTime    string `json:"startdatetime"`
	Duration         int    `json:"duration"`
	ColumnID         int    `json:"columnid"`
	ProfileID        int    `json:"profileid"`
	Provider         string `json:"provider"`
	Facility         string `json:"facility"`
	AppointmentTypes []int  `json:"appointmenttypeids"`
	PatientID        int    `json:"patientid"`
}

func (c *AdvancedMDRestClient) GetAppointments(ctx context.Context, tokenData *session.TokenData, columnID string, startDate string) (appointmentsResult []domain.Appointment, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_appointments")
	defer func() { finish(resultErr) }()
	amdAppts, err := c.appointmentRows(ctx, tokenData, "day", columnID, startDate, "appointments")
	if err != nil {
		return nil, err
	}
	return ScheduledAppointments(amdAppts)
}

func ScheduledAppointments(amdAppts []AMDAppointmentResponse) ([]domain.Appointment, error) {
	var appointments []domain.Appointment
	for _, a := range amdAppts {
		startTime, err := ParseDateTime(a.StartDateTime)
		if err != nil {
			return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse appointment start time: %w", err)
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

type amdBlockHoldResponse struct {
	ID            int    `json:"id"`
	StartDateTime string `json:"startdatetime"`
	EndDateTime   string `json:"enddatetime"`
	Duration      int    `json:"duration"`
	ColumnID      int    `json:"columnid"`
	Column        struct {
		ID int `json:"id"`
	} `json:"column"`
	Recurrence struct {
		RecurrenceType int `json:"recurrencetype"`
	} `json:"recurrence"`
}

func (c *AdvancedMDRestClient) GetBlockHolds(ctx context.Context, tokenData *session.TokenData, columnID string, startDate string) ([]domain.BlockHold, error) {
	return c.getBlockHolds(ctx, tokenData, "get_block_holds", columnID, "day", startDate)
}

func (c *AdvancedMDRestClient) GetBlockHoldsByMonth(ctx context.Context, tokenData *session.TokenData, columnIDs string, startDate string) ([]domain.BlockHold, error) {
	return c.getBlockHolds(ctx, tokenData, "get_block_holds_by_month", columnIDs, "month", startDate)
}

func (c *AdvancedMDRestClient) getBlockHolds(ctx context.Context, tokenData *session.TokenData, operation, columnIDs, view, startDate string) (holdsResult []domain.BlockHold, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, operation)
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/blockholds?columnId=%s&forView=%s&startDate=%s",
		tokenData.RestApiBase, columnIDs, view, startDate)

	body, err := c.getResponseBody(ctx, tokenData, url, "block holds")
	if err != nil {
		return nil, err
	}

	amdHolds, err := oneOrMany[amdBlockHoldResponse](body)
	if err != nil {
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse block holds: %w", err)
	}

	var holds []domain.BlockHold
	for _, h := range amdHolds {
		startTime, err := ParseDateTime(h.StartDateTime)
		if err != nil {
			return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse block hold start time: %w", err)
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
			ColumnID:      blockHoldColumnID(h),
		})
	}

	return holds, nil
}

func blockHoldColumnID(hold amdBlockHoldResponse) int {
	if hold.Column.ID != 0 {
		return hold.Column.ID
	}
	return hold.ColumnID
}

func (c *AdvancedMDRestClient) GetAppointmentsByMonth(ctx context.Context, tokenData *session.TokenData, columnIDs string, startDate string) (appointmentsResult []AMDAppointmentResponse, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_appointments_by_month")
	defer func() { finish(resultErr) }()
	return c.appointmentRows(ctx, tokenData, "month", columnIDs, startDate, "monthly appointments")
}

func (c *AdvancedMDRestClient) GetAppointmentsByDay(ctx context.Context, tokenData *session.TokenData, columnIDs string, date string) (appointmentsResult []AMDAppointmentResponse, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "get_appointments_by_day")
	defer func() { finish(resultErr) }()
	return c.appointmentRows(ctx, tokenData, "day", columnIDs, date, "daily appointments")
}

func (c *AdvancedMDRestClient) appointmentRows(ctx context.Context, tokenData *session.TokenData, view, columnIDs, startDate, operation string) ([]AMDAppointmentResponse, error) {
	url := fmt.Sprintf("https://%s/scheduler/appointments?columnId=%s&forView=%s&isLegacy=true&startDate=%s",
		tokenData.RestApiBase, columnIDs, view, startDate)

	body, err := c.getResponseBody(ctx, tokenData, url, operation)
	if err != nil {
		return nil, err
	}

	appts, err := oneOrMany[AMDAppointmentResponse](body)
	if err != nil {
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse appointments: %w", err)
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

type bookAppointmentResponse struct {
	ID int `json:"id"`
}

func (c *AdvancedMDRestClient) BookAppointment(ctx context.Context, tokenData *session.TokenData, params BookAppointmentParams) (appointmentIDResult int, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "book_appointment")
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/Appointments", tokenData.RestApiBase)

	resp, err := c.send(ctx, tokenData, writeRequest, http.MethodPost, url, params)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to read response: %w", err)
	}

	var result bookAppointmentResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse response: %w", err)
	}
	if result.ID <= 0 {
		return 0, safeerrors.New(safeerrors.CategoryInvalidResponse, "invalid booking response")
	}

	return result.ID, nil
}

func (c *AdvancedMDRestClient) CancelAppointment(ctx context.Context, tokenData *session.TokenData, appointmentID int) (resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "cancel_appointment")
	defer func() { finish(resultErr) }()
	url := fmt.Sprintf("https://%s/scheduler/appointments/%d/cancel",
		tokenData.RestApiBase, appointmentID)

	resp, err := c.send(ctx, tokenData, writeRequest, http.MethodPut, url, map[string]int{"id": appointmentID})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *AdvancedMDRestClient) getResponseBody(ctx context.Context, tokenData *session.TokenData, url, operation string) ([]byte, error) {
	resp, err := c.send(ctx, tokenData, readRequest, http.MethodGet, url, nil)
	if err != nil {
		var status *httpStatusError
		if errors.As(err, &status) {
			log.Printf(
				"WARNING: provider=advancedmd operation=%q upstream_http_status=%d",
				operation, status.statusCode,
			)
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to read response: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "failed to parse empty %s response", operation)
	}
	return body, nil
}

func (c *AdvancedMDRestClient) send(ctx context.Context, tokenData *session.TokenData, kind requestKind, method, url string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, safeerrors.NotSent("failed to marshal request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, safeerrors.NotSent("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", tokenData.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	observeProviderStatus(ctx, resp.StatusCode)

	accepted := resp.StatusCode >= 200 && resp.StatusCode < 300
	if kind == readRequest {
		accepted = resp.StatusCode == http.StatusOK
	}
	if !accepted {
		resp.Body.Close()
		return nil, statusError(resp.StatusCode, kind)
	}
	return resp, nil
}
