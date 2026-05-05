package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
)

func addPatientCmd() *cobra.Command {
	var (
		firstName, lastName, dob, phone, email string
		street, aptSuite, city, state, zip     string
		sex, insurance                         string
		subscriberName, subscriberNum          string
		office                                 string
	)

	cmd := &cobra.Command{
		Use:   "add-patient",
		Short: "Create a new patient in AdvancedMD",
		Example: `  amd add-patient --first Jane --last Doe --dob 2000-03-01 --phone 555-123-4567 \
    --email jane@example.com --street "123 Main St" --city Miami --state FL \
    --zip 33101 --sex F --insurance "Florida Blue" --subscriber-name "Jane Doe" \
    --subscriber-num ABC123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate required fields
			missing := []string{}
			for _, pair := range []struct{ name, val string }{
				{"--first", firstName}, {"--last", lastName}, {"--dob", dob},
				{"--phone", phone}, {"--street", street},
				{"--city", city}, {"--state", state}, {"--zip", zip},
				{"--sex", sex}, {"--insurance", insurance},
				{"--subscriber-name", subscriberName}, {"--subscriber-num", subscriberNum},
			} {
				if pair.val == "" {
					missing = append(missing, pair.name)
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
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

			// Normalize inputs
			normalizedDOB := domain.NormalizeDOB(dob)
			formattedPhone := domain.FormatPhone(phone)
			normalizedSex := domain.NormalizeSex(sex)
			normalizedFirst := domain.StripDiacritics(firstName)
			normalizedLast := domain.StripDiacritics(lastName)

			tokenData := getToken()

			rawPatientID, respPartyID, patientName, err := app.amdClient.AddPatient(cmd.Context(), tokenData, clients.AddPatientParams{
				FirstName: normalizedFirst,
				LastName:  normalizedLast,
				DOB:       normalizedDOB,
				Phone:     formattedPhone,
				Email:     email,
				Street:    street,
				AptSuite:  aptSuite,
				City:      city,
				State:     strings.ToUpper(state),
				Zip:       zip,
				Sex:       normalizedSex,
				ProfileID: officeConfig.DefaultProfileID,
			})
			if err != nil {
				if strings.Contains(err.Error(), "Duplicate name/DOB") {
					printJSON(map[string]string{
						"status":  "error",
						"message": "A patient with this name and DOB already exists. Use 'verify' instead.",
					})
					return nil
				}
				return fmt.Errorf("failed to create patient: %w", err)
			}

			strippedID := domain.StripPatientPrefix(rawPatientID)

			insEntry, ok := domain.LookupInsuranceForCoverageAtOffice(insurance, domain.InsuranceModeMedical, officeConfig)
			if !ok {
				printJSON(map[string]interface{}{
					"status":    "partial",
					"patientId": strippedID,
					"name":      patientName,
					"dob":       normalizedDOB,
					"message":   fmt.Sprintf("Patient created but insurance %q not recognized.", insurance),
				})
				return nil
			}

			if insEntry.Routing == domain.RoutingNotAccepted {
				printJSON(map[string]interface{}{
					"status":    "partial",
					"patientId": strippedID,
					"name":      patientName,
					"dob":       normalizedDOB,
					"message":   fmt.Sprintf("%s is not accepted at %s.", insurance, officeConfig.DisplayName),
				})
				return nil
			}

			if err := app.amdClient.AddInsurance(cmd.Context(), tokenData, rawPatientID, respPartyID, insEntry.CarrierID, subscriberNum); err != nil {
				printJSON(map[string]interface{}{
					"status":    "partial",
					"patientId": strippedID,
					"name":      patientName,
					"dob":       normalizedDOB,
					"message":   "Patient created but insurance failed: " + err.Error(),
				})
				return nil
			}

			routing := insEntry.Routing
			if domain.IsMinor(normalizedDOB) && routing != domain.RoutingNotAccepted {
				routing = officeConfig.PediatricRouting
			}

			printJSON(map[string]interface{}{
				"status":           "created",
				"patientId":        strippedID,
				"name":             patientName,
				"dob":              normalizedDOB,
				"routing":          string(routing),
				"allowedProviders": officeConfig.ProvidersForRouting(routing),
				"preauthRequired":  insEntry.PreauthRequired,
				"message":          "Patient created and insurance attached successfully",
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&firstName, "first", "", "First name (required)")
	cmd.Flags().StringVar(&lastName, "last", "", "Last name (required)")
	cmd.Flags().StringVar(&dob, "dob", "", "Date of birth (required)")
	cmd.Flags().StringVar(&phone, "phone", "", "Phone number (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email address (optional)")
	cmd.Flags().StringVar(&street, "street", "", "Street address (required)")
	cmd.Flags().StringVar(&aptSuite, "apt", "", "Apartment/suite (optional)")
	cmd.Flags().StringVar(&city, "city", "", "City (required)")
	cmd.Flags().StringVar(&state, "state", "", "State abbreviation (required)")
	cmd.Flags().StringVar(&zip, "zip", "", "ZIP code (required)")
	cmd.Flags().StringVar(&sex, "sex", "", "Sex: M, F, or U (required)")
	cmd.Flags().StringVar(&insurance, "insurance", "", "Insurance plan name (required)")
	cmd.Flags().StringVar(&subscriberName, "subscriber-name", "", "Subscriber name (required)")
	cmd.Flags().StringVar(&subscriberNum, "subscriber-num", "", "Subscriber/member number (required)")
	cmd.Flags().StringVar(&office, "office", "", "Office name (e.g., spring_hill)")

	return cmd
}
