package advancedmdtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/session"
)

const (
	providerXMLRPCHost = "xmlrpc.advancedmd.test"
	providerRESTHost   = "rest.advancedmd.test"
	rowLayout          = "2006-01-02T15:04:05"
)

type HoldColumn struct {
	ID int `json:"id"`
}

type HoldRecurrence struct {
	RecurrenceType int `json:"recurrencetype"`
}

type HoldRow struct {
	ID            int            `json:"id"`
	StartDateTime string         `json:"startdatetime"`
	EndDateTime   string         `json:"enddatetime"`
	Duration      int            `json:"duration"`
	Column        HoldColumn     `json:"column"`
	Recurrence    HoldRecurrence `json:"recurrence"`
}

func (row HoldRow) Span() domain.Interval {
	start, _ := time.Parse(rowLayout, row.StartDateTime)
	if row.Recurrence.RecurrenceType > 0 {
		return domain.Interval{Start: start, End: start.Add(time.Duration(row.Duration) * time.Minute)}
	}
	end, _ := time.Parse(rowLayout, row.EndDateTime)
	return domain.Interval{Start: start, End: end}
}

type Response struct {
	Status int
	Body   string
}

type Provider struct {
	Appointments []clients.AMDAppointmentResponse
	Holds        []HoldRow
	Responses    map[string][]Response

	mu       sync.Mutex
	requests []string
}

func (p *Provider) Adapter(now func() time.Time) *advancedmd.Adapter {
	client := &http.Client{Transport: p}
	return advancedmd.NewAdapter(
		providerSession{},
		clients.NewAdvancedMDClient(client, now),
		clients.NewAdvancedMDRestClient(client),
		now,
	)
}

func (p *Provider) Requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.requests)
}

func (p *Provider) Unused() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var routes []string
	for route, queue := range p.Responses {
		if len(queue) > 0 {
			routes = append(routes, route)
		}
	}
	slices.Sort(routes)
	return routes
}

func (p *Provider) RoundTrip(request *http.Request) (*http.Response, error) {
	route, err := requestRoute(request)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.requests = append(p.requests, route)
	scripted, ok := p.nextResponse(route)
	p.mu.Unlock()
	if ok {
		return respond(request, scripted.Status, []byte(scripted.Body)), nil
	}
	body, err := p.rows(request, route)
	if err != nil {
		return nil, err
	}
	return respond(request, http.StatusOK, body), nil
}

func (p *Provider) nextResponse(route string) (Response, bool) {
	queue := p.Responses[route]
	if len(queue) == 0 {
		return Response{}, false
	}
	p.Responses[route] = queue[1:]
	if queue[0].Status == 0 {
		queue[0].Status = http.StatusOK
	}
	return queue[0], true
}

func (p *Provider) rows(request *http.Request, route string) ([]byte, error) {
	if route == "getpatientvisits" {
		return p.patientVisits(request)
	}
	query := request.URL.Query()
	if request.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected AdvancedMD request %s", route)
	}
	columns := strings.Split(query.Get("columnId"), "-")
	first, err := time.Parse("2006-01-02", query.Get("startDate"))
	if err != nil {
		return nil, err
	}
	window := domain.Interval{Start: first, End: first.AddDate(0, 0, 1)}
	if query.Get("forView") == "month" {
		window = domain.Interval{Start: first.AddDate(0, 0, -7), End: first.AddDate(0, 1, 7)}
	}
	switch path.Base(request.URL.Path) {
	case "appointments":
		served := []clients.AMDAppointmentResponse{}
		for _, row := range p.Appointments {
			start, _ := time.Parse(rowLayout, row.StartDateTime)
			if slices.Contains(columns, strconv.Itoa(row.ColumnID)) && !start.Before(window.Start) && start.Before(window.End) {
				served = append(served, row)
			}
		}
		return json.Marshal(served)
	case "blockholds":
		served := []HoldRow{}
		for _, row := range p.Holds {
			span := row.Span()
			if !span.End.After(span.Start) {
				span.End = span.Start.Add(time.Nanosecond)
			}
			if slices.Contains(columns, strconv.Itoa(row.Column.ID)) && span.Overlaps(window) {
				served = append(served, row)
			}
		}
		return json.Marshal(served)
	default:
		return nil, fmt.Errorf("unexpected AdvancedMD request %s", route)
	}
}

func (p *Provider) patientVisits(request *http.Request) ([]byte, error) {
	var message struct {
		PPMDMsg struct {
			PatientID string `json:"@patientid"`
		} `json:"ppmdmsg"`
	}
	if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
		return nil, err
	}
	visits := []map[string]string{}
	for _, row := range p.Appointments {
		if strconv.Itoa(row.PatientID) == message.PPMDMsg.PatientID {
			visits = append(visits, map[string]string{
				"@id":                 strconv.Itoa(row.ID),
				"@visitstartdatetime": row.StartDateTime,
				"@apptstatus":         "0",
			})
		}
	}
	var visitList any
	if len(visits) > 0 {
		visitList = map[string]any{"visit": visits}
	}
	return json.Marshal(map[string]any{"PPMDResults": map[string]any{
		"Results": map[string]any{"@visitcount": strconv.Itoa(len(visits)), "visitlist": visitList},
		"Error":   nil,
	}})
}

func requestRoute(request *http.Request) (string, error) {
	if request.URL.Host == providerXMLRPCHost {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return "", err
		}
		var message struct {
			PPMDMsg struct {
				Action string `json:"@action"`
			} `json:"ppmdmsg"`
		}
		if err := json.Unmarshal(body, &message); err != nil {
			return "", err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		return message.PPMDMsg.Action, nil
	}
	switch {
	case request.Method == http.MethodPost && strings.EqualFold(request.URL.Path, "/scheduler/appointments"):
		return "book", nil
	case request.Method == http.MethodPut && strings.HasSuffix(request.URL.Path, "/cancel"):
		return "cancel", nil
	}
	query := request.URL.Query()
	columns := strings.Split(query.Get("columnId"), "-")
	slices.Sort(columns)
	return strings.Join([]string{path.Base(request.URL.Path), query.Get("forView"), query.Get("startDate"), strings.Join(columns, "-")}, " "), nil
}

func respond(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

type providerSession struct{}

func (providerSession) Get(context.Context) (*session.TokenData, error) {
	return &session.TokenData{
		Token:       "Bearer provider-test",
		CookieToken: "token=provider-test",
		XmlrpcURL:   providerXMLRPCHost + "/xmlrpc/processrequest.aspx",
		RestApiBase: providerRESTHost,
	}, nil
}

func (providerSession) Maintain(context.Context) error {
	return nil
}

func (providerSession) Status() session.SessionStatus {
	return session.SessionStatus{}
}
