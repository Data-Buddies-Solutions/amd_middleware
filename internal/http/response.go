package http

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolResponse[T any] struct {
	IsError           bool          `json:"isError"`
	Content           []TextContent `json:"content"`
	StructuredContent T             `json:"structuredContent"`
}

func OK[T any](summary string, structured T) ToolResponse[T] {
	return ToolResponse[T]{
		IsError: false,
		Content: []TextContent{{
			Type: "text",
			Text: summary,
		}},
		StructuredContent: structured,
	}
}

func Err[T any](summary string, structured T) ToolResponse[T] {
	return ToolResponse[T]{
		IsError: true,
		Content: []TextContent{{
			Type: "text",
			Text: summary,
		}},
		StructuredContent: structured,
	}
}

type InsuranceSummary struct {
	Carrier          string   `json:"carrier"`
	Routing          string   `json:"routing"`
	RoutingAmbiguous bool     `json:"routingAmbiguous"`
	AmbiguousOptions []string `json:"ambiguousOptions"`
	PreauthRequired  bool     `json:"preauthRequired"`
}

type VerifyPatientStructured struct {
	Outcome          string            `json:"outcome"`
	PatientID        string            `json:"patientId,omitempty"`
	Name             string            `json:"name,omitempty"`
	DOB              string            `json:"dob,omitempty"`
	Phone            string            `json:"phone,omitempty"`
	Insurance        *InsuranceSummary `json:"insurance,omitempty"`
	AllowedProviders []string          `json:"allowedProviders,omitempty"`
	Matches          []PatientMatch    `json:"matches,omitempty"`
	Disambiguation   string            `json:"disambiguation,omitempty"`
}

type AddPatientStructured struct {
	Outcome          string            `json:"outcome"`
	PatientID        string            `json:"patientId,omitempty"`
	Name             string            `json:"name,omitempty"`
	DOB              string            `json:"dob,omitempty"`
	Insurance        *InsuranceSummary `json:"insurance,omitempty"`
	AllowedProviders []string          `json:"allowedProviders,omitempty"`
}

type PatientLookupStructured struct {
	Outcome          string                    `json:"outcome"`
	PatientID        string                    `json:"patientId,omitempty"`
	Name             string                    `json:"name,omitempty"`
	DOB              string                    `json:"dob,omitempty"`
	Phone            string                    `json:"phone,omitempty"`
	Insurance        *InsuranceSummary         `json:"insurance,omitempty"`
	AllowedProviders []string                  `json:"allowedProviders,omitempty"`
	Appointments     []PatientAppointmentEntry `json:"appointments,omitempty"`
	Matches          []PatientMatch            `json:"matches,omitempty"`
}

type AvailabilityStructured struct {
	Outcome      string                       `json:"outcome"`
	SearchedDate string                       `json:"searchedDate"`
	ActualDate   string                       `json:"actualDate,omitempty"`
	DateShifted  bool                         `json:"dateShifted"`
	ShiftReason  string                       `json:"shiftReason,omitempty"`
	Location     string                       `json:"location"`
	Providers    []ProviderAvailabilityResult `json:"providers"`
}

type ProviderAvailabilityResult struct {
	Name           string                `json:"name"`
	SlotDuration   int                   `json:"slotDuration"`
	TotalAvailable int                   `json:"totalAvailable"`
	FirstAvailable string                `json:"firstAvailable,omitempty"`
	LastAvailable  string                `json:"lastAvailable,omitempty"`
	Slots          []AvailableSlotResult `json:"slots"`
}

type AvailableSlotResult struct {
	Time         string `json:"time"`
	BookingToken string `json:"bookingToken"`
}

type BookAppointmentStructured struct {
	Outcome                 string `json:"outcome"`
	AppointmentID           int    `json:"appointmentId,omitempty"`
	Date                    string `json:"date,omitempty"`
	Time                    string `json:"time,omitempty"`
	Provider                string `json:"provider,omitempty"`
	Location                string `json:"location,omitempty"`
	ValidAppointmentTypeIDs []int  `json:"validAppointmentTypeIds,omitempty"`
}

type UpdateInsuranceStructured struct {
	Outcome          string            `json:"outcome"`
	PatientID        string            `json:"patientId,omitempty"`
	OldInsurance     string            `json:"oldInsurance,omitempty"`
	Insurance        *InsuranceSummary `json:"insurance,omitempty"`
	AllowedProviders []string          `json:"allowedProviders,omitempty"`
}

type PatientAppointmentsStructured struct {
	Outcome      string                    `json:"outcome"`
	PatientID    string                    `json:"patientId,omitempty"`
	Appointments []PatientAppointmentEntry `json:"appointments,omitempty"`
}

type CancelAppointmentStructured struct {
	Outcome       string `json:"outcome"`
	AppointmentID int    `json:"appointmentId,omitempty"`
}

type PatientAppointmentEntry struct {
	AppointmentID int    `json:"appointmentId"`
	Date          string `json:"date"`
	Time          string `json:"time"`
	Provider      string `json:"provider"`
	Type          string `json:"type"`
	Location      string `json:"location"`
	IsSchedulable bool   `json:"isSchedulable"`
	Confirmed     bool   `json:"confirmed"`
}
