package insurance

import "testing"

func TestNoteRulesIgnoreNegations(t *testing.T) {
	referral := noteRules[2].pattern
	for _, note := range []string{"Does Not Require Referral", "NO auths/referrals needed", "Does not require Prior Auth/Referral"} {
		if statesRule(note, referral) {
			t.Errorf("%q read as a referral rule", note)
		}
	}
	if !statesRule("REQUIRES SIMPLE REFERRAL FROM THE PCP OFFICE", referral) {
		t.Error("referral rule not found")
	}
}
