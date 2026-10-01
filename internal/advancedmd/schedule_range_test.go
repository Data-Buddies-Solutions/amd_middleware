package advancedmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/session"
)

type monthScheduleServer struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
	failures map[string]int
}

func (s *monthScheduleServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	key := strings.TrimPrefix(r.URL.Path, "/scheduler/") + " " + query.Get("startDate")
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.Path+" columnId="+query.Get("columnId")+" forView="+query.Get("forView")+" startDate="+query.Get("startDate"))
	s.mu.Unlock()
	if status := s.failures[key]; status != 0 {
		w.WriteHeader(status)
		return
	}
	body, ok := s.bodies[key]
	if !ok {
		body = "[]"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(body))
}

func newMonthScheduleAdapter(t *testing.T, server *monthScheduleServer) *Adapter {
	t.Helper()
	httpServer := httptest.NewTLSServer(server)
	t.Cleanup(httpServer.Close)
	return NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(httpServer.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(httpServer.Client()),
	)
}

func appointmentIDs(column domain.ColumnSchedule) []int {
	var ids []int
	for _, appointment := range column.Appointments {
		ids = append(ids, appointment.ID)
	}
	return ids
}

func holdIDs(column domain.ColumnSchedule) []int {
	var ids []int
	for _, hold := range column.BlockHolds {
		ids = append(ids, hold.ID)
	}
	return ids
}

func TestReadScheduleRangeReadsEachMonthOnceAndFilesRowsByDayAndColumn(t *testing.T) {
	server := &monthScheduleServer{bodies: map[string]string{
		"appointments 2026-10-01": `[
			{"id": 1, "startdatetime": "2026-10-28T09:00:00", "duration": 15, "columnid": 1513},
			{"id": 2, "startdatetime": "2026-10-30T10:00:00", "duration": 15, "columnid": 1550},
			{"id": 3, "startdatetime": "2026-10-05T09:00:00", "duration": 15, "columnid": 1513},
			{"id": 9, "startdatetime": "2026-11-02T09:00:00", "duration": 15, "columnid": 1550}
		]`,
		"appointments 2026-11-01": `[
			{"id": 9, "startdatetime": "2026-11-02T09:00:00", "duration": 15, "columnid": 1550},
			{"id": 4, "startdatetime": "2026-11-03T11:00:00", "duration": 30, "columnid": 1513}
		]`,
		"blockholds 2026-10-01": `[
			{"id": 10, "startdatetime": "2026-10-31T00:00:00", "enddatetime": "2026-11-02T23:59:00", "column": {"id": 1550}},
			{"id": 11, "startdatetime": "2026-10-29T12:00:00", "enddatetime": "2026-10-29T13:00:00", "duration": 60, "column": {"id": 1513}, "recurrence": {"recurrencetype": 2}}
		]`,
		"blockholds 2026-11-01": `[
			{"id": 10, "startdatetime": "2026-10-31T00:00:00", "enddatetime": "2026-11-02T23:59:00", "column": {"id": 1550}},
			{"id": 11, "startdatetime": "2026-11-03T12:00:00", "enddatetime": "2026-11-03T13:00:00", "duration": 60, "column": {"id": 1513}, "recurrence": {"recurrencetype": 2}}
		]`,
	}}
	adapter := newMonthScheduleAdapter(t, server)

	schedule, err := adapter.ReadScheduleRange(context.Background(), domain.ScheduleRangeQuery{
		ColumnIDs: []string{"1513", "1550"},
		Start:     time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadScheduleRange error = %v", err)
	}

	slices.Sort(server.requests)
	wantRequests := []string{
		"/scheduler/appointments columnId=1513-1550 forView=month startDate=2026-10-01",
		"/scheduler/appointments columnId=1513-1550 forView=month startDate=2026-11-01",
		"/scheduler/blockholds columnId=1513-1550 forView=month startDate=2026-10-01",
		"/scheduler/blockholds columnId=1513-1550 forView=month startDate=2026-11-01",
	}
	if !slices.Equal(server.requests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", server.requests, wantRequests)
	}
	if len(schedule) != 7 {
		t.Fatalf("days = %d, want 7", len(schedule))
	}

	want := map[string]map[string][2][]int{
		"2026-10-28": {"1513": {{1}, nil}},
		"2026-10-29": {"1513": {nil, {11}}},
		"2026-10-30": {"1550": {{2}, nil}},
		"2026-10-31": {"1550": {nil, {10}}},
		"2026-11-01": {"1550": {nil, {10}}},
		"2026-11-02": {"1550": {{9}, {10}}},
		"2026-11-03": {"1513": {{4}, {11}}},
	}
	for date, read := range schedule {
		for _, columnID := range []string{"1513", "1550"} {
			column, ok := read.Columns[columnID]
			if !ok || !column.Complete() {
				t.Fatalf("%s column %s present=%v complete=%v", date, columnID, ok, column.Complete())
			}
			expected := want[date][columnID]
			if !slices.Equal(appointmentIDs(column), expected[0]) || !slices.Equal(holdIDs(column), expected[1]) {
				t.Fatalf("%s column %s appointments=%v holds=%v, want %v %v", date, columnID, appointmentIDs(column), holdIDs(column), expected[0], expected[1])
			}
		}
	}
}

func TestReadScheduleRangeMarksDayIncompleteWhenRowHasNoQueriedColumn(t *testing.T) {
	server := &monthScheduleServer{bodies: map[string]string{
		"blockholds 2026-10-01": `[
			{"id": 20, "startdatetime": "2026-10-15T09:00:00", "enddatetime": "2026-10-15T10:00:00"},
			{"id": 21, "startdatetime": "2026-10-16T09:00:00", "enddatetime": "2026-10-16T10:00:00", "column": {"id": 1513}}
		]`,
	}}
	adapter := newMonthScheduleAdapter(t, server)

	schedule, err := adapter.ReadScheduleRange(context.Background(), domain.ScheduleRangeQuery{
		ColumnIDs: []string{"1513", "1550"},
		Start:     time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadScheduleRange error = %v", err)
	}
	for _, columnID := range []string{"1513", "1550"} {
		if schedule["2026-10-15"].Columns[columnID].Complete() {
			t.Fatalf("2026-10-15 column %s complete despite an unattributed hold", columnID)
		}
		if !schedule["2026-10-16"].Columns[columnID].Complete() {
			t.Fatalf("2026-10-16 column %s incomplete", columnID)
		}
	}
}

func TestReadScheduleRangeFailsWhenAnyMonthReadFails(t *testing.T) {
	server := &monthScheduleServer{failures: map[string]int{"blockholds 2026-11-01": http.StatusTooManyRequests}}
	adapter := newMonthScheduleAdapter(t, server)

	_, err := adapter.ReadScheduleRange(context.Background(), domain.ScheduleRangeQuery{
		ColumnIDs: []string{"1513"},
		Start:     time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("ReadScheduleRange succeeded with a failed month read")
	}
}

func TestReadScheduleRangeKeepsHoldSharedByProvidersForEachProvider(t *testing.T) {
	server := &monthScheduleServer{bodies: map[string]string{
		"blockholds 2026-10-01": `[
			{"id": 30, "startdatetime": "2026-10-05T12:30:00", "enddatetime": "2026-10-05T13:30:00", "column": {"id": 1513}},
			{"id": 30, "startdatetime": "2026-10-05T12:30:00", "enddatetime": "2026-10-05T13:30:00", "column": {"id": 1550}}
		]`,
	}}
	adapter := newMonthScheduleAdapter(t, server)

	schedule, err := adapter.ReadScheduleRange(context.Background(), domain.ScheduleRangeQuery{
		ColumnIDs: []string{"1513", "1550"},
		Start:     time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadScheduleRange error = %v", err)
	}
	for _, columnID := range []string{"1513", "1550"} {
		if got := holdIDs(schedule["2026-10-05"].Columns[columnID]); !slices.Equal(got, []int{30}) {
			t.Fatalf("column %s holds = %v, want the shared hold", columnID, got)
		}
	}
}
