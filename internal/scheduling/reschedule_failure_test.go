package scheduling

import (
	"errors"
	"strings"
	"testing"
)

func TestFailedReplacementReceiptClaimsOnlyWhatTheCategoryProves(t *testing.T) {
	for name, c := range map[string]struct {
		err          error
		status       string
		outcome      Category
		claimsOrigin bool
	}{
		"unclassified":  {errors.New("untyped"), "uncertain", CategoryUnclassified, false},
		"indeterminate": {categorizedError(CategoryIndeterminateWrite, "may have booked."), "uncertain", CategoryIndeterminateWrite, true},
		"slot taken":    {slotUnavailableError(), "failed", CategorySlotUnavailable, true},
	} {
		t.Run(name, func(t *testing.T) {
			receipt := failedReplacementReceipt(c.err)
			if receipt.Status != c.status || receipt.Outcome != string(c.outcome) || receipt.Failure != c.err {
				t.Fatalf("receipt = %+v", receipt)
			}
			if got := strings.Contains(receipt.Message, "original appointment was not cancelled"); got != c.claimsOrigin {
				t.Fatalf("message = %q, claims the original survived = %v, want %v", receipt.Message, got, c.claimsOrigin)
			}
		})
	}
}
