package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"advancedmd-token-management/internal/domain"
)

func verifyCmd() *cobra.Command {
	var lastName, firstName, dob, phone, office string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a patient by name or phone number and date of birth",
		Example: `  amd verify --last Doe --dob 1990-01-15
  amd verify --last Doe --first John --dob 01/15/1990
  amd verify --phone 7863344429 --dob 10/31/1996
  amd verify --last Doe --dob 1990-01-15 --office spring_hill`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if lastName == "" && phone == "" {
				return fmt.Errorf("--last or --phone is required")
			}
			if dob == "" {
				return fmt.Errorf("--dob is required")
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

			normalizedDOB := domain.NormalizeDOB(dob)

			tokenData := getToken()

			var patients []domain.Patient
			var err error
			if phone != "" {
				digits := domain.NormalizePhoneDigits(phone)
				patients, err = app.amdClient.LookupPatientByPhone(cmd.Context(), tokenData, digits)
				if err != nil {
					return fmt.Errorf("failed to lookup patient by phone: %w", err)
				}
				log.Printf("lookup returned %d patients for phone %q", len(patients), digits)
			} else {
				normalizedLastName := domain.StripDiacritics(lastName)
				normalizedFirstName := domain.StripDiacritics(firstName)
				patients, err = app.amdClient.LookupPatient(cmd.Context(), tokenData, normalizedLastName, normalizedFirstName)
				if err != nil {
					return fmt.Errorf("failed to lookup patient: %w", err)
				}
				log.Printf("lookup returned %d patients for %q", len(patients), normalizedLastName)
			}

			// Filter by DOB
			var matching []domain.Patient
			for _, p := range patients {
				if domain.NormalizeDOB(p.DOB) == normalizedDOB {
					matching = append(matching, p)
				}
			}

			switch len(matching) {
			case 0:
				printJSON(map[string]string{
					"status":  "not_found",
					"message": "No patient found with that last name and date of birth",
				})
				return nil

			case 1:
				return printVerifiedPatient(cmd.Context(), matching[0], officeConfig)

			default:
				// Multiple matches — try to disambiguate by first name
				if firstName != "" {
					upperFirstName := strings.ToUpper(firstName)
					for _, p := range matching {
						if strings.HasPrefix(p.FirstName, upperFirstName) {
							return printVerifiedPatient(cmd.Context(), p, officeConfig)
						}
					}
					printJSON(map[string]string{
						"status":  "not_found",
						"message": "No patient found matching that first name",
					})
					return nil
				}

				// Return list for disambiguation
				var names []string
				for _, p := range matching {
					names = append(names, p.FirstName)
				}
				printJSON(map[string]interface{}{
					"status":  "multiple_matches",
					"message": fmt.Sprintf("Found %d patients. Use --first to disambiguate.", len(matching)),
					"matches": names,
				})
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&lastName, "last", "", "Patient last name (required if no --phone)")
	cmd.Flags().StringVar(&firstName, "first", "", "Patient first name (for disambiguation)")
	cmd.Flags().StringVar(&dob, "dob", "", "Date of birth (e.g., 01/15/1990 or 1990-01-15)")
	cmd.Flags().StringVar(&phone, "phone", "", "Phone number (alternative to --last)")
	cmd.Flags().StringVar(&office, "office", "", "Office name (e.g., spring_hill)")

	return cmd
}

// printVerifiedPatient fetches demographics and prints the verified patient result.
func printVerifiedPatient(ctx context.Context, p domain.Patient, officeConfig *domain.OfficeConfig) error {
	tokenData := getToken()

	demoResult, err := app.amdClient.GetDemographic(ctx, tokenData, p.ID)
	if err != nil {
		log.Printf("WARNING: failed to get demographics: %v", err)
	}

	resp := map[string]interface{}{
		"status":    "verified",
		"patientId": p.ID,
		"name":      p.FullName,
		"dob":       p.DOB,
		"phone":     p.Phone,
	}

	if demoResult != nil {
		if demoResult.CarrierName != "" {
			resp["insuranceCarrier"] = demoResult.CarrierName
		}

		if demoResult.CarrierID != "" {
			resp["insuranceCarrierId"] = demoResult.CarrierID
			routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
			resp["routing"] = string(routing)
			resp["allowedProviders"] = officeConfig.ProvidersForRouting(routing)
			resp["routingAmbiguous"] = ambiguous

			// Pediatric override
			if domain.IsMinor(p.DOB) && routing != domain.RoutingNotAccepted {
				resp["routing"] = string(officeConfig.PediatricRouting)
				resp["allowedProviders"] = officeConfig.ProvidersForRouting(officeConfig.PediatricRouting)
				resp["routingAmbiguous"] = false
			}
		}

		if demoResult.InsPlanID != "" {
			resp["insPlanId"] = demoResult.InsPlanID
		}
		if demoResult.RespPartyID != "" {
			resp["respPartyId"] = demoResult.RespPartyID
		}
	}

	printJSON(resp)
	return nil
}
