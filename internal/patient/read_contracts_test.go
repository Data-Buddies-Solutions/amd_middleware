package patient_test

import (
	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"context"
	"testing"
)

func TestResolveIncompleteAppointmentsCannotProveAbsence(t *testing.T) {
	records := advancedmdtest.NewAdapter()
	records.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Read: advancedmd.AppointmentRead{Complete: false}}
	result, err := patient.New(domain.NewOfficeCatalog(""), records).Resolve(context.Background(), patient.ResolveCommand{PatientID: "123", OfficeID: "spring_hill"})
	if err != nil {
		t.Fatal(err)
	}
	if result.AppointmentsStatus != patient.AppointmentsError || result.Observation.AppointmentOutcome != "incomplete" {
		t.Fatalf("incomplete appointment read reported %+v", result)
	}
}
