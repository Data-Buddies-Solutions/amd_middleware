package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func appointmentsCmd() *cobra.Command {
	var patientID, office string

	cmd := &cobra.Command{
		Use:   "appointments",
		Short: "Get upcoming appointments for a patient",
		Example: `  amd appointments --patient-id 12345
  amd appointments --patient-id 12345 --office spring_hill`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if patientID == "" {
				return fmt.Errorf("--patient-id is required")
			}

			patientIDInt, err := strconv.Atoi(patientID)
			if err != nil {
				return fmt.Errorf("patient-id must be numeric")
			}

			if err := mustBootstrap(); err != nil {
				return err
			}

			// Resolve office config
			officeConfig := domain.DefaultOffice()
			if office != "" {
				oc, ok := domain.LookupOffice(office)
				if !ok {
					return fmt.Errorf("unknown office %q — valid: %s", office, strings.Join(domain.ValidOfficeNames(), ", "))
				}
				officeConfig = oc
			}

			tokenData := getToken()

			// Build column ID string for office's allowed columns
			columnIDStr := strings.Join(officeConfig.AllowedColumnIDs(), "-")

			// Fetch current + next 3 months concurrently (4 months total)
			now := time.Now().In(eastern)
			thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, eastern)

			type monthResult struct {
				appts []clients.AMDAppointmentResponse
				err   error
			}
			ch := make(chan monthResult, 4)

			for i := 0; i < 4; i++ {
				m := thisMonth.AddDate(0, i, 0)
				go func() {
					appts, err := app.amdRestClient.GetAppointmentsByMonth(cmd.Context(), tokenData, columnIDStr, m.Format("2006-01-02"))
					ch <- monthResult{appts, err}
				}()
			}

			var allAppts []clients.AMDAppointmentResponse
			for range 4 {
				r := <-ch
				if r.err != nil {
					return fmt.Errorf("failed to retrieve appointments: %w", r.err)
				}
				allAppts = append(allAppts, r.appts...)
			}
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, eastern)

			type apptDetail struct {
				ID        int    `json:"id"`
				Date      string `json:"date"`
				Time      string `json:"time"`
				Provider  string `json:"provider,omitempty"`
				Type      string `json:"type,omitempty"`
				Facility  string `json:"facility,omitempty"`
			}

			var details []apptDetail
			for _, a := range allAppts {
				if a.PatientID != patientIDInt {
					continue
				}

				startTime, err := time.Parse("2006-01-02T15:04:05", a.StartDateTime)
				if err != nil {
					startTime, err = time.Parse("2006-01-02T15:04", a.StartDateTime)
					if err != nil {
						continue
					}
				}

				if startTime.Before(today) {
					continue
				}

				typeName := ""
				if len(a.AppointmentTypes) > 0 {
					if name, ok := officeConfig.AppointmentTypeName(a.AppointmentTypes[0]); ok {
						typeName = name
					}
				}

				details = append(details, apptDetail{
					ID:        a.ID,
					Date:      startTime.Format("Monday, January 2, 2006"),
					Time:      startTime.Format("3:04 PM"),
					Provider:  officeConfig.FriendlyProviderName(a.Provider),
					Type:      typeName,
					Facility:  friendlyFacilityName(a.Facility),
				})
			}

			log.Printf("found %d upcoming appointments for patient %s (scanned %d total)", len(details), patientID, len(allAppts))

			if len(details) == 0 {
				printJSON(map[string]interface{}{
					"status":    "no_appointments",
					"patientId": patientID,
					"message":   "No upcoming appointments found",
				})
				return nil
			}

			printJSON(map[string]interface{}{
				"status":       "found",
				"patientId":    patientID,
				"appointments": details,
				"message":      fmt.Sprintf("Found %d upcoming appointment(s)", len(details)),
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&patientID, "patient-id", "", "Patient ID (required)")
	cmd.Flags().StringVar(&office, "office", "", "Office name (e.g., spring_hill)")

	return cmd
}

// friendlyFacilityName cleans up AMD facility names to title case.
func friendlyFacilityName(amdName string) string {
	if amdName == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ToLower(amdName))
}
