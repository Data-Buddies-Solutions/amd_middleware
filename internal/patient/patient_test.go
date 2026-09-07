package patient_test

import (
	"bytes"
	"context"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
)

func TestResolveReturnsCompletePatientForPhoneLookup(t *testing.T) {
	domain.InitRegistry("")
	office, ok := domain.LookupOffice("Spring Hill")
	if !ok {
		t.Fatal("Spring Hill office is not configured")
	}

	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[search] = []domain.Patient{{
		ID:        "123",
		FirstName: "JANE",
		LastName:  "DOE",
		FullName:  "DOE,JANE",
		DOB:       "01/15/1980",
		Phone:     "850-373-3869",
	}}
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierName: "HUMANA MEDICARE",
		CarrierID:   "car40906",
		InsPlanID:   "ins789",
		RespPartyID: "resp456",
		DOB:         "01/15/1980",
	}
	amd.AppointmentResults["123"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:                9570263,
				Start:             time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC),
				Provider:          "Dr. Austin Bach",
				Type:              "Established Adult Medical (Follow Up)",
				AppointmentTypeID: 1007,
				Facility:          "Abita Eye Group Spring Hill",
				OfficeID:          "spring_hill",
				Office:            "Spring Hill",
			}},
			Complete: true,
		},
	}

	resolver := patient.New(amd)
	got, err := resolver.Resolve(context.Background(), patient.ResolveCommand{
		Phone:    "(954) 287-2010",
		OfficeID: office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := patient.ResolveResult{
		Status:             patient.StatusVerified,
		PatientID:          "123",
		Name:               "DOE,JANE",
		DOB:                "01/15/1980",
		Phone:              "850-373-3869",
		InsuranceCarrier:   "HUMANA MEDICARE",
		InsuranceCarrierID: "car40906",
		InsPlanID:          "ins789",
		RespPartyID:        "resp456",
		Routing:            domain.RoutingBachOnly,
		AllowedProviders:   []string{"Dr. Bach"},
		AppointmentsStatus: patient.AppointmentsFound,
		Appointments: []patient.Appointment{{
			ID:                9570263,
			Date:              "Wednesday, March 18, 2026",
			Time:              "12:00 PM",
			Provider:          "Dr. Austin Bach",
			Type:              "Established Adult Medical (Follow Up)",
			AppointmentTypeID: 1007,
			Facility:          "Abita Eye Group Spring Hill",
			OfficeID:          "spring_hill",
			Office:            "Spring Hill",
		}},
		Message: "Patient verified with 1 appointment(s)",
	}
	assertResolveResult(t, got, want)

	var _ advancedmd.PatientRecords = amd
}

func TestCreateReturnsExistingSuccessContract(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.CreatedPatient = domain.CreatedPatient{
		ID:          "123",
		RespPartyID: "resp456",
		Name:        "DOE,JANE",
	}

	got := patient.New(amd).Create(context.Background(), patient.CreateCommand{
		FirstName:      "Jäne",
		LastName:       "Döe",
		DOB:            "1980-01-15",
		Phone:          "9542872010",
		Email:          " jane@example.com ",
		Street:         "123 Main St",
		City:           "Spring Hill",
		State:          "fl",
		Zip:            "34609",
		Sex:            "female",
		Insurance:      "Humana Medicare",
		SubscriberName: "Jane Doe",
		SubscriberNum:  "H123",
		Office:         "Spring Hill",
	})

	want := patient.CreateResult{
		Status:           patient.CreateStatusCreated,
		PatientID:        "123",
		Name:             "DOE,JANE",
		DOB:              "01/15/1980",
		Routing:          domain.RoutingBachOnly,
		AllowedProviders: []string{"Dr. Bach"},
		Message:          "Patient created and insurance attached successfully",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Create() = %+v, want %+v", got, want)
	}
}

func TestCreateOwnsValidationAndOfficeResolution(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()

	command := validCreateCommand()
	command.Office = "Unknown"
	command.FirstName = ""
	got := patient.New(amd).Create(context.Background(), command)

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationValidationFailed {
		t.Fatalf("Create() = %+v, want validation failure", got)
	}
	if got.Message != `unknown office: "Unknown". Valid options: Crystal River, Hollywood, North Miami Beach Optical, Spring Hill, Sweetwater` {
		t.Fatalf("Message = %q", got.Message)
	}
	if amd.CreatePatientCalls != 0 {
		t.Fatalf("CreatePatient calls = %d, want none", amd.CreatePatientCalls)
	}
}

func TestCreateReturnsStableRejectionWithoutRetry(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewError(safeerrors.CategoryRejected)

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationRejected {
		t.Fatalf("Create() = %+v, want stable rejection", got)
	}
	if amd.CreatePatientCalls != 1 {
		t.Fatalf("CreatePatient calls = %d, want one", amd.CreatePatientCalls)
	}
	if amd.SearchPatientCalls != 1 {
		t.Fatalf("SearchPatients calls = %d, want one pre-write baseline read", amd.SearchPatientCalls)
	}
}

func TestCreateReconcilesAmbiguousWriteAfterTransientReadFailure(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearchSequence[search] = []advancedmdtest.PatientSearchStep{
		{},
		{Err: advancedmd.NewError(safeerrors.CategoryNetwork)},
		{Patients: []domain.Patient{{
			ID:        "123",
			FirstName: "JANE",
			FullName:  "DOE,JANE",
			DOB:       "01/15/1980",
			Phone:     "(954)287-2010",
		}}},
	}
	amd.Demographics["123"] = domain.PatientDemographics{RespPartyID: "resp456"}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusCreated || got.Outcome != patient.MutationReconciledSuccess {
		t.Fatalf("Create() = %+v, want reconciled success", got)
	}
	if amd.CreatePatientCalls != 1 {
		t.Fatalf("CreatePatient calls = %d, want one", amd.CreatePatientCalls)
	}
	if amd.SearchPatientCalls != 3 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus transient read and retry", amd.SearchPatientCalls)
	}
	if amd.AddInsuranceCalls != 1 {
		t.Fatalf("AddPatientInsurance calls = %d, want one after reconciliation", amd.AddInsuranceCalls)
	}
}

func TestCreateKeepsEmptyReconciliationLookupIndeterminate(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("Create() = %+v, want indeterminate write", got)
	}
	if amd.CreatePatientCalls != 1 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 1/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 4 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus three bounded polls", amd.SearchPatientCalls)
	}
	if !strings.Contains(got.Message, "Do not retry automatically") {
		t.Fatalf("Message = %q, want no-retry guidance", got.Message)
	}
}

func TestCreatePollsUntilNewPatientBecomesVisible(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearchSequence[search] = []advancedmdtest.PatientSearchStep{
		{},
		{},
		{Patients: []domain.Patient{{
			ID:        "123",
			FirstName: "JANE",
			FullName:  "DOE,JANE",
			DOB:       "01/15/1980",
			Phone:     "(954)287-2010",
		}}},
	}
	amd.Demographics["123"] = domain.PatientDemographics{RespPartyID: "resp456"}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusCreated || got.Outcome != patient.MutationReconciledSuccess {
		t.Fatalf("Create() = %+v, want reconciled success after visibility delay", got)
	}
	if amd.SearchPatientCalls != 3 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus two post-write polls", amd.SearchPatientCalls)
	}
	if amd.AddInsuranceCalls != 1 {
		t.Fatalf("AddPatientInsurance calls = %d, want one", amd.AddInsuranceCalls)
	}
}

func TestCreateKeepsUnidentifiablePostWriteMatchIndeterminate(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearchSequence[search] = []advancedmdtest.PatientSearchStep{
		{},
		{Patients: []domain.Patient{
			{
				ID:        "123",
				FirstName: "JANE",
				FullName:  "DOE,JANE",
				DOB:       "01/15/1980",
				Phone:     "(954)287-2010",
			},
			{
				FirstName: "JANE",
				FullName:  "DOE,JANE",
				DOB:       "01/15/1980",
				Phone:     "(954)287-2010",
			},
		}},
	}
	amd.Demographics["123"] = domain.PatientDemographics{RespPartyID: "resp456"}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("Create() = %+v, want unidentifiable post-write match to remain indeterminate", got)
	}
	if amd.CreatePatientCalls != 1 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 1/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 2 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus one unsafe result", amd.SearchPatientCalls)
	}
}

func TestCreateDoesNotAdoptPreexistingPatientAfterAmbiguousWrite(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	existing := domain.Patient{
		ID:        "123",
		FirstName: "JANE",
		FullName:  "DOE,JANE",
		DOB:       "01/15/1980",
		Phone:     "(954)287-2010",
	}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearches[search] = []domain.Patient{existing}
	amd.Demographics["123"] = domain.PatientDemographics{RespPartyID: "resp456"}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("Create() = %+v, want pre-existing match to remain indeterminate", got)
	}
	if amd.CreatePatientCalls != 1 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 1/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 4 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus three bounded polls", amd.SearchPatientCalls)
	}
}

func TestCreateDoesNotAdoptPreexistingPatientWhoseLookupDetailsChanged(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	existing := domain.Patient{
		ID:        "123",
		FirstName: "JANE",
		FullName:  "DOE,JANE",
		DOB:       "01/15/1980",
		Phone:     "(954)287-2010",
	}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearchSequence[search] = []advancedmdtest.PatientSearchStep{{
		Patients: []domain.Patient{{
			ID:        "123",
			FirstName: "JANE",
		}},
	}}
	amd.PatientSearches[search] = []domain.Patient{existing}
	amd.Demographics["123"] = domain.PatientDemographics{RespPartyID: "resp456"}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("Create() = %+v, want changed pre-existing match to remain indeterminate", got)
	}
	if amd.CreatePatientCalls != 1 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 1/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 4 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus three bounded polls", amd.SearchPatientCalls)
	}
}

func TestCreateDoesNotWriteWithAnUnidentifiableBaselinePatient(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.CreatedPatient = domain.CreatedPatient{
		ID:          "123",
		RespPartyID: "resp456",
		Name:        "DOE,JANE",
	}
	amd.PatientSearches[search] = []domain.Patient{{
		FirstName: "JANE",
		FullName:  "DOE,JANE",
		DOB:       "01/15/1980",
		Phone:     "(954)287-2010",
	}}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationFailed {
		t.Fatalf("Create() = %+v, want safe pre-write failure", got)
	}
	if amd.CreatePatientCalls != 0 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 0/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 1 {
		t.Fatalf("SearchPatients calls = %d, want one baseline read", amd.SearchPatientCalls)
	}
}

func TestCreateDoesNotWriteWithoutAReconciliationBaseline(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.PatientErrors[search] = advancedmd.NewError(safeerrors.CategoryNetwork)

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationFailed {
		t.Fatalf("Create() = %+v, want safe pre-write failure", got)
	}
	if amd.CreatePatientCalls != 0 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 0/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 3 {
		t.Fatalf("SearchPatients calls = %d, want bounded baseline retries", amd.SearchPatientCalls)
	}
}

func TestCreateReturnsIndeterminateWhenReconciliationCannotProveOutcome(t *testing.T) {
	domain.InitRegistry("")
	search := domain.PatientSearch{Phone: "9542872010"}
	amd := advancedmdtest.NewAdapter()
	amd.CreatePatientError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.PatientSearchSequence[search] = []advancedmdtest.PatientSearchStep{
		{},
		{Err: advancedmd.NewError(safeerrors.CategoryNetwork)},
		{Err: advancedmd.NewError(safeerrors.CategoryNetwork)},
		{Err: advancedmd.NewError(safeerrors.CategoryNetwork)},
	}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("Create() = %+v, want indeterminate write", got)
	}
	if amd.CreatePatientCalls != 1 || amd.AddInsuranceCalls != 0 {
		t.Fatalf("mutation calls = create:%d insurance:%d, want 1/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
	}
	if amd.SearchPatientCalls != 4 {
		t.Fatalf("SearchPatients calls = %d, want baseline plus bounded three attempts", amd.SearchPatientCalls)
	}
}

func TestCreateReconcilesAmbiguousInsuranceAttachment(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.CreatedPatient = domain.CreatedPatient{
		ID:          "123",
		RespPartyID: "resp456",
		Name:        "DOE,JANE",
	}
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierName:         "HUMANA MEDICARE",
		CarrierID:           "car308175",
		InsPlanID:           "ins456",
		RespPartyID:         "resp456",
		SubscriberNum:       "H123",
		InsuranceStateKnown: true,
	}

	got := patient.New(amd).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusCreated || got.Outcome != patient.MutationReconciledSuccess {
		t.Fatalf("Create() = %+v, want reconciled insurance success", got)
	}
	if amd.AddInsuranceCalls != 1 || amd.DemographicCalls != 1 {
		t.Fatalf("calls = add:%d demographics:%d, want 1/1", amd.AddInsuranceCalls, amd.DemographicCalls)
	}
}

func TestUpdateInsuranceReturnsExistingSuccessContract(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()

	got := patient.New(amd).UpdateInsurance(context.Background(), patient.UpdateInsuranceCommand{
		PatientID:      "123",
		DOB:            "01/15/1980",
		InsPlanID:      "ins123",
		RespPartyID:    "resp123",
		OldInsurance:   "Old",
		Insurance:      "Humana Medicare",
		SubscriberName: "Jane Doe",
		SubscriberNum:  "H123",
		Office:         "Spring Hill",
	})

	want := patient.UpdateInsuranceResult{
		Status:           patient.UpdateInsuranceStatusUpdated,
		PatientID:        "123",
		OldInsurance:     "Old",
		NewInsurance:     "Humana Medicare",
		Routing:          domain.RoutingBachOnly,
		AllowedProviders: []string{"Dr. Bach"},
		Message:          "Insurance updated successfully",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpdateInsurance() = %+v, want %+v", got, want)
	}
}

func TestUpdateInsuranceReturnsStableRejectionWithoutRetry(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewError(safeerrors.CategoryRejected)

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusError || got.Outcome != patient.MutationRejected {
		t.Fatalf("UpdateInsurance() = %+v, want stable rejection", got)
	}
	if amd.AddInsuranceCalls != 1 {
		t.Fatalf("AddPatientInsurance calls = %d, want one", amd.AddInsuranceCalls)
	}
}

func TestUpdateInsuranceReconcilesAmbiguousWriteAfterTransientReadFailure(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.DemographicErrorSequence["123"] = []error{
		advancedmd.NewError(safeerrors.CategoryTimeout),
		nil,
	}
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierName:         "HUMANA MEDICARE",
		CarrierID:           "car308175",
		InsPlanID:           "ins456",
		RespPartyID:         "resp123",
		SubscriberNum:       "H123",
		InsuranceStateKnown: true,
	}

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusUpdated || got.Outcome != patient.MutationReconciledSuccess {
		t.Fatalf("UpdateInsurance() = %+v, want reconciled success", got)
	}
	if amd.AddInsuranceCalls != 1 {
		t.Fatalf("AddPatientInsurance calls = %d, want one", amd.AddInsuranceCalls)
	}
	if amd.DemographicCalls != 2 {
		t.Fatalf("GetPatientDemographics calls = %d, want transient read plus retry", amd.DemographicCalls)
	}
}

func TestUpdateInsuranceReconcilesAmbiguousEndDateBeforeAddingReplacement(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.EndInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.Demographics["123"] = domain.PatientDemographics{
		RespPartyID:         "resp123",
		InsuranceStateKnown: true,
	}

	got := patient.New(amd).UpdateInsurance(context.Background(), validUpdateInsuranceCommand())

	if got.Status != patient.UpdateInsuranceStatusUpdated || got.Outcome != patient.MutationReconciledSuccess {
		t.Fatalf("UpdateInsurance() = %+v, want reconciled success", got)
	}
	if amd.EndInsuranceCalls != 1 || amd.AddInsuranceCalls != 1 {
		t.Fatalf("mutation calls = end:%d add:%d, want 1/1", amd.EndInsuranceCalls, amd.AddInsuranceCalls)
	}
}

func TestUpdateInsuranceReturnsReconciledFailureWhenDemographicsProveNoWrite(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierName:         "AETNA",
		CarrierID:           "car40887",
		InsPlanID:           "ins123",
		InsuranceStateKnown: true,
	}

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusError || got.Outcome != patient.MutationReconciledFailure {
		t.Fatalf("UpdateInsurance() = %+v, want reconciled failure", got)
	}
	if amd.AddInsuranceCalls != 1 {
		t.Fatalf("AddPatientInsurance calls = %d, want one", amd.AddInsuranceCalls)
	}
}

func TestUpdateInsuranceDoesNotAcceptPreexistingSameCarrierAsReconciledSuccess(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierID:           "car308175",
		InsPlanID:           "ins456",
		RespPartyID:         "resp123",
		SubscriberNum:       "OLD-MEMBER",
		InsuranceStateKnown: true,
	}

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusError || got.Outcome != patient.MutationReconciledFailure {
		t.Fatalf("UpdateInsurance() = %+v, want same-carrier mismatch failure", got)
	}
}

func TestUpdateInsuranceReturnsIndeterminateWhenDemographicsCannotProveOutcome(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.DemographicErrors["123"] = advancedmd.NewError(safeerrors.CategoryNetwork)

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("UpdateInsurance() = %+v, want indeterminate write", got)
	}
	if amd.AddInsuranceCalls != 1 || amd.DemographicCalls != 3 {
		t.Fatalf("calls = add:%d demographics:%d, want 1/3", amd.AddInsuranceCalls, amd.DemographicCalls)
	}
}

func TestUpdateInsuranceReturnsIndeterminateWhenInsuranceStateIsIncomplete(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUnavailable)
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierID: "car308175",
		InsPlanID: "ins456",
	}

	command := validUpdateInsuranceCommand()
	command.InsPlanID = ""
	got := patient.New(amd).UpdateInsurance(context.Background(), command)

	if got.Status != patient.UpdateInsuranceStatusError || got.Outcome != patient.MutationIndeterminateWrite {
		t.Fatalf("UpdateInsurance() = %+v, want incomplete-state indeterminate write", got)
	}
}

func TestResolveReturnsLightweightCandidatesWithoutHydrationForMultipleMatches(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")

	search := domain.PatientSearch{Phone: "5552223333"}
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[search] = []domain.Patient{
		{ID: "123", FirstName: "JANE", FullName: "DOE,JANE", DOB: "01/15/1980", Phone: "5552223333"},
		{ID: "456", FirstName: "JOHN", FullName: "DOE,JOHN", DOB: "03/20/1982", Phone: "5552223333"},
	}
	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		Phone:    "5552223333",
		OfficeID: office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got.Status != patient.StatusMultipleMatches {
		t.Fatalf("Status = %q, want %q", got.Status, patient.StatusMultipleMatches)
	}
	if got.Message != "Found 2 patients for this phone number. Ask the caller to confirm their name." {
		t.Fatalf("Message = %q", got.Message)
	}
	if got.Appointments == nil || len(got.Appointments) != 0 {
		t.Fatalf("top-level Appointments = %#v, want non-nil empty slice", got.Appointments)
	}
	if len(got.Matches) != 2 {
		t.Fatalf("Matches = %+v, want two candidates", got.Matches)
	}
	if got.Matches[0].Status != "candidate" ||
		got.Matches[0].PatientID != "123" ||
		got.Matches[0].FirstName != "JANE" ||
		got.Matches[0].LastName != "DOE" ||
		got.Matches[0].DOB != "01/15/1980" {
		t.Fatalf("first match = %+v", got.Matches[0])
	}
	if got.Matches[1].Status != "candidate" ||
		got.Matches[1].PatientID != "456" ||
		got.Matches[1].FirstName != "JOHN" ||
		got.Matches[1].LastName != "DOE" ||
		got.Matches[1].DOB != "03/20/1982" {
		t.Fatalf("second match = %+v", got.Matches[1])
	}
	if amd.DemographicCalls != 0 || amd.AppointmentReadCalls != 0 {
		t.Fatalf(
			"hydration calls = demographics:%d appointments:%d, want 0/0",
			amd.DemographicCalls,
			amd.AppointmentReadCalls,
		)
	}
}

func TestResolveDoesNotHydrateAmbiguousFirstNameCandidates(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	search := domain.PatientSearch{Phone: "5552223333"}
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[search] = []domain.Patient{
		{ID: "123", FirstName: "JANE", FullName: "DOE,JANE", DOB: "01/15/1980"},
		{ID: "456", FirstName: "JANET", FullName: "DOE,JANET", DOB: "03/20/1982"},
	}

	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		Phone:     "5552223333",
		FirstName: "Ja",
		OfficeID:  office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != patient.StatusMultipleMatches || len(got.Matches) != 2 {
		t.Fatalf("Resolve() = %+v, want two ambiguous candidates", got)
	}
	if amd.DemographicCalls != 0 || amd.AppointmentReadCalls != 0 {
		t.Fatalf(
			"hydration calls = demographics:%d appointments:%d, want 0/0",
			amd.DemographicCalls,
			amd.AppointmentReadCalls,
		)
	}
}

func TestResolvePreservesMalformedAndTimeoutSearchErrors(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	search := domain.PatientSearch{Phone: "9542872010"}
	tests := []struct {
		name     string
		category safeerrors.Category
	}{
		{name: "malformed provider response", category: safeerrors.CategoryInvalidResponse},
		{name: "provider timeout", category: safeerrors.CategoryTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			amd := advancedmdtest.NewAdapter()
			amd.PatientErrors[search] = advancedmd.NewError(test.category)

			got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
				Phone:    "9542872010",
				OfficeID: office.ID,
			})
			if err == nil || advancedmd.CategoryOf(err) != test.category {
				t.Fatalf("Resolve() error = %v, want category %q", err, test.category)
			}
			if got.Status != "" || got.Observation.CandidateCountBucket != "unknown" {
				t.Fatalf("Resolve() = %+v, want unresolved compatible error", got)
			}
			if amd.DemographicCalls != 0 || amd.AppointmentReadCalls != 0 {
				t.Fatalf(
					"hydration calls = demographics:%d appointments:%d, want 0/0",
					amd.DemographicCalls,
					amd.AppointmentReadCalls,
				)
			}
		})
	}
}

func validCreateCommand() patient.CreateCommand {
	return patient.CreateCommand{
		FirstName:      "Jane",
		LastName:       "Doe",
		DOB:            "01/15/1980",
		Phone:          "9542872010",
		Street:         "123 Main St",
		City:           "Spring Hill",
		State:          "FL",
		Zip:            "34609",
		Sex:            "female",
		Insurance:      "Humana Medicare",
		SubscriberName: "Jane Doe",
		SubscriberNum:  "H123",
		Office:         "Spring Hill",
	}
}

func validUpdateInsuranceCommand() patient.UpdateInsuranceCommand {
	return patient.UpdateInsuranceCommand{
		PatientID:      "123",
		DOB:            "01/15/1980",
		InsPlanID:      "ins123",
		RespPartyID:    "resp123",
		OldInsurance:   "Old",
		Insurance:      "Humana Medicare",
		SubscriberName: "Jane Doe",
		SubscriberNum:  "H123",
		Office:         "Spring Hill",
	}
}

func TestResolveSelectsPatientByVerifiedDemographics(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Hollywood")
	candidates := []domain.Patient{
		{ID: "123", FirstName: "JANE", LastName: "DOE", FullName: "DOE,JANE", DOB: "01/15/1980"},
		{ID: "456", FirstName: "JANET", LastName: "DOE", FullName: "DOE,JANET", DOB: "03/20/1982"},
	}

	tests := []struct {
		name    string
		command patient.ResolveCommand
		search  domain.PatientSearch
		wantID  string
	}{
		{
			name:    "phone and first name",
			command: patient.ResolveCommand{Phone: "555-222-3333", FirstName: "Janet", OfficeID: office.ID},
			search:  domain.PatientSearch{Phone: "5552223333"},
			wantID:  "456",
		},
		{
			name:    "phone and DOB",
			command: patient.ResolveCommand{Phone: "555-222-3333", DOB: "1/15/1980", OfficeID: office.ID},
			search:  domain.PatientSearch{Phone: "5552223333"},
			wantID:  "123",
		},
		{
			name:    "phone first name and DOB",
			command: patient.ResolveCommand{Phone: "555-222-3333", FirstName: "Jane", DOB: "01-15-1980", OfficeID: office.ID},
			search:  domain.PatientSearch{Phone: "5552223333"},
			wantID:  "123",
		},
		{
			name:    "last name and DOB",
			command: patient.ResolveCommand{LastName: "Döe", DOB: "03/20/1982", OfficeID: office.ID},
			search:  domain.PatientSearch{LastName: "Doe"},
			wantID:  "456",
		},
		{
			name:    "last name first name and DOB",
			command: patient.ResolveCommand{LastName: "Döe", FirstName: "Ja", DOB: "01/15/1980", OfficeID: office.ID},
			search:  domain.PatientSearch{LastName: "Doe", FirstName: "Ja"},
			wantID:  "123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			amd := advancedmdtest.NewAdapter()
			amd.PatientSearches[test.search] = candidates

			got, err := patient.New(amd).Resolve(context.Background(), test.command)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Status != patient.StatusVerified || got.PatientID != test.wantID {
				t.Fatalf("Resolve() = %+v, want verified patient %s", got, test.wantID)
			}
		})
	}
}

func TestResolveReturnsNoMatchWithoutHydratingPatientData(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	amd := advancedmdtest.NewAdapter()

	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		Phone:    "9542872010",
		OfficeID: office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != patient.StatusNotFound {
		t.Fatalf("Status = %q, want not_found", got.Status)
	}
	if got.Message != "No patient found for that phone number" {
		t.Fatalf("Message = %q", got.Message)
	}
	if got.Appointments == nil || len(got.Appointments) != 0 {
		t.Fatalf("Appointments = %#v, want non-nil empty slice", got.Appointments)
	}
}

func TestResolveRefreshesKnownPatientByID(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	amd := advancedmdtest.NewAdapter()
	amd.Demographics["123"] = domain.PatientDemographics{
		FullName:    "DOE,JANE",
		CarrierName: "HUMANA MEDICARE",
		CarrierID:   "car40906",
		DOB:         "01/15/1980",
	}
	amd.AppointmentResults["123"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:       9570263,
				Start:    time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC),
				OfficeID: "spring_hill",
				Office:   "Spring Hill",
			}},
			Complete: true,
		},
	}

	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		PatientID: "123",
		OfficeID:  office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != patient.StatusVerified || got.PatientID != "123" {
		t.Fatalf("Resolve() = %+v, want verified patient 123", got)
	}
	if got.Name != "DOE,JANE" {
		t.Fatalf("Name = %q, want DOE,JANE", got.Name)
	}
	if got.DOB != "01/15/1980" || got.Routing != domain.RoutingBachOnly {
		t.Fatalf("demographics = DOB %q routing %q", got.DOB, got.Routing)
	}
	if got.AppointmentsStatus != patient.AppointmentsFound || len(got.Appointments) != 1 {
		t.Fatalf("appointments = %q %+v", got.AppointmentsStatus, got.Appointments)
	}
	if amd.SearchPatientCalls != 0 || amd.DemographicCalls != 1 || amd.AppointmentReadCalls != 1 {
		t.Fatalf(
			"provider calls = search:%d demographics:%d appointments:%d, want 0/1/1",
			amd.SearchPatientCalls,
			amd.DemographicCalls,
			amd.AppointmentReadCalls,
		)
	}
}

func TestResolveStartsDemographicsAndAppointmentsConcurrently(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	demographicsStarted := make(chan struct{}, 1)
	appointmentsStarted := make(chan struct{}, 1)
	demographicsRelease := make(chan struct{})
	appointmentsRelease := make(chan struct{})
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[domain.PatientSearch{Phone: "9542872010"}] = []domain.Patient{{
		ID:        "123",
		FirstName: "JANE",
		FullName:  "DOE,JANE",
		DOB:       "01/15/1980",
	}}
	amd.DemographicsStarted = demographicsStarted
	amd.DemographicsRelease = demographicsRelease
	amd.AppointmentsStarted = appointmentsStarted
	amd.AppointmentsRelease = appointmentsRelease

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	type resolveResponse struct {
		result patient.ResolveResult
		err    error
	}
	resolved := make(chan resolveResponse, 1)
	go func() {
		result, err := patient.New(amd).Resolve(ctx, patient.ResolveCommand{
			Phone:    "9542872010",
			OfficeID: office.ID,
		})
		resolved <- resolveResponse{result: result, err: err}
	}()

	select {
	case <-demographicsStarted:
	case <-ctx.Done():
		t.Fatal("demographic read did not start")
	}
	select {
	case <-appointmentsStarted:
	case <-ctx.Done():
		t.Fatal("appointment read did not start while demographics was blocked")
	}
	close(demographicsRelease)
	close(appointmentsRelease)

	response := <-resolved
	if response.err != nil {
		t.Fatalf("Resolve() error = %v", response.err)
	}
	if response.result.Status != patient.StatusVerified {
		t.Fatalf("Resolve() = %+v, want verified patient", response.result)
	}
}

func TestResolveKeepsVerifiedPatientWhenAppointmentsFail(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[domain.PatientSearch{Phone: "9542872010"}] = []domain.Patient{{
		ID: "123", FullName: "DOE,JANE", DOB: "01/15/1980",
	}}
	amd.AppointmentResults["123"] = advancedmdtest.AppointmentResult{
		Err: advancedmd.NewError(safeerrors.CategoryUpstreamStatus),
	}

	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		Phone:    "9542872010",
		OfficeID: office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != patient.StatusVerified {
		t.Fatalf("Status = %q, want verified", got.Status)
	}
	if got.AppointmentsStatus != patient.AppointmentsError {
		t.Fatalf("AppointmentsStatus = %q, want error", got.AppointmentsStatus)
	}
	if got.ProviderFailure != safeerrors.CategoryUpstreamStatus {
		t.Fatalf("ProviderFailure = %q, want upstream_status", got.ProviderFailure)
	}
	if got.AppointmentsMessage != "Failed to retrieve appointments from AdvancedMD. Please try again." {
		t.Fatalf("AppointmentsMessage = %q", got.AppointmentsMessage)
	}
	if got.Message != "Patient verified, appointment lookup unavailable" {
		t.Fatalf("Message = %q", got.Message)
	}
	if got.Appointments == nil || len(got.Appointments) != 0 {
		t.Fatalf("Appointments = %#v, want non-nil empty slice", got.Appointments)
	}
	for _, forbidden := range []string{"patientId=123", "token=secret", "status 500"} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("logs exposed %q: %s", forbidden, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "patient-resolve: failed to get appointments category=upstream_status") {
		t.Fatalf("logs = %q, want safe classified failure", logs.String())
	}
}

func TestResolveKnownPatientReturnsUnavailableWhenAdvancedMDCannotAuthenticate(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	amd := advancedmdtest.NewAdapter()
	amd.DemographicErrors["123"] = advancedmd.NewError(safeerrors.CategoryUnavailable)

	_, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		PatientID: "123",
		OfficeID:  office.ID,
	})
	if err == nil {
		t.Fatal("Resolve() error = nil, want unavailable")
	}
	if advancedmd.CategoryOf(err) != safeerrors.CategoryUnavailable {
		t.Fatalf("Resolve() error category = %q", advancedmd.CategoryOf(err))
	}
	if err.Error() != "unavailable" {
		t.Fatalf("Resolve() error = %q, want redacted category", err)
	}
}

func TestResolveKeepsVerifiedPatientWhenDemographicsProviderReadFails(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")
	amd := advancedmdtest.NewAdapter()
	amd.DemographicErrors["123"] = advancedmd.NewError(safeerrors.CategoryAuthentication)

	got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
		PatientID: "123",
		OfficeID:  office.ID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != patient.StatusVerified || got.PatientID != "123" {
		t.Fatalf("Resolve() = %+v, want verified patient 123", got)
	}
	if got.AppointmentsStatus != patient.AppointmentsNone {
		t.Fatalf("AppointmentsStatus = %q, want none", got.AppointmentsStatus)
	}
	if got.ProviderFailure != safeerrors.CategoryAuthentication {
		t.Fatalf("ProviderFailure = %q, want authentication", got.ProviderFailure)
	}
}

func TestResolveAppliesPreauthorizationAndPediatricProviderPolicy(t *testing.T) {
	domain.InitRegistry("")
	office, _ := domain.LookupOffice("Spring Hill")

	tests := []struct {
		name             string
		demographics     domain.PatientDemographics
		patientDOB       string
		wantRouting      domain.RoutingRule
		wantPreauth      bool
		wantAmbiguous    bool
		wantProviderList bool
	}{
		{
			name: "preauthorization carrier",
			demographics: domain.PatientDemographics{
				CarrierName: "AETNA HMO",
				CarrierID:   "car40907",
			},
			patientDOB:       "01/01/1980",
			wantRouting:      domain.RoutingAll,
			wantPreauth:      true,
			wantAmbiguous:    false,
			wantProviderList: true,
		},
		{
			name: "minor uses pediatric routing",
			demographics: domain.PatientDemographics{
				CarrierName: "AETNA",
				CarrierID:   "car40887",
			},
			patientDOB:       "01/01/2015",
			wantRouting:      office.PediatricRouting,
			wantPreauth:      false,
			wantAmbiguous:    false,
			wantProviderList: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			amd := advancedmdtest.NewAdapter()
			amd.PatientSearches[domain.PatientSearch{Phone: "9542872010"}] = []domain.Patient{{
				ID: "123", FullName: "DOE,JANE", DOB: test.patientDOB,
			}}
			amd.Demographics["123"] = test.demographics

			got, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{
				Phone:    "9542872010",
				OfficeID: office.ID,
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Routing != test.wantRouting ||
				got.PreauthRequired != test.wantPreauth ||
				got.RoutingAmbiguous != test.wantAmbiguous {
				t.Fatalf("policy result = %+v", got)
			}
			if (len(got.AllowedProviders) > 0) != test.wantProviderList {
				t.Fatalf("AllowedProviders = %v", got.AllowedProviders)
			}
		})
	}
}

func assertResolveResult(t *testing.T, got, want patient.ResolveResult) {
	t.Helper()
	if got.Status != want.Status ||
		got.PatientID != want.PatientID ||
		got.Name != want.Name ||
		got.DOB != want.DOB ||
		got.Phone != want.Phone ||
		got.InsuranceCarrier != want.InsuranceCarrier ||
		got.InsuranceCarrierID != want.InsuranceCarrierID ||
		got.InsPlanID != want.InsPlanID ||
		got.RespPartyID != want.RespPartyID ||
		got.Routing != want.Routing ||
		got.RoutingAmbiguous != want.RoutingAmbiguous ||
		got.PreauthRequired != want.PreauthRequired ||
		got.AppointmentsStatus != want.AppointmentsStatus ||
		got.AppointmentsMessage != want.AppointmentsMessage ||
		got.Message != want.Message {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
	if len(got.AllowedProviders) != len(want.AllowedProviders) {
		t.Fatalf("AllowedProviders = %v, want %v", got.AllowedProviders, want.AllowedProviders)
	}
	for i := range want.AllowedProviders {
		if got.AllowedProviders[i] != want.AllowedProviders[i] {
			t.Fatalf("AllowedProviders = %v, want %v", got.AllowedProviders, want.AllowedProviders)
		}
	}
	if len(got.Appointments) != len(want.Appointments) {
		t.Fatalf("Appointments = %+v, want %+v", got.Appointments, want.Appointments)
	}
	for i := range want.Appointments {
		if got.Appointments[i] != want.Appointments[i] {
			t.Fatalf("Appointments = %+v, want %+v", got.Appointments, want.Appointments)
		}
	}
}
