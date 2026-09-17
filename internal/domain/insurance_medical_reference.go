package domain

// Medical billing identities verified in the practice AMD directory on 2026-09-17.
// Participation remains office-specific. Vision does not consume this table.
var medicalCarrierCodes = map[string]string{
	"car40887": "AET07", "car40890": "AVM01", "car40895": "CIG09",
	"car301345": "CIGN1", "car302890": "CIGN4", "car281245": "AMBE1",
	"car281317": "PREM1", "car40907": "ICA01", "car280750": "FLOR1",
	"car40897": "FLO01", "car40899": "FLO03", "car40900": "FLO02",
	"car303033": "HUM02", "car303062": "HUM PPO", "car303061": "HUMPHMO",
	"car301507": "MOLI2", "car301578": "MERI1", "car284233": "OSCA1",
	"car308142": "IMAG1", "car308627": "EYEC1", "car40916": "PRE04",
	"car284327": "TRI00", "car40921": "TRI05", "car284838": "UNIT3",
	"car40923": "UNI20", "car301427": "SEMI1", "car301672": "SELF",
}

// The July 2026 document describes office visits. Surgery/injection authorization
// exceptions do not create blanket office-visit holds. These requirements cannot
// be cleared by a caller's assertion or by merely recognizing a carrier.
func medicalRequirements(plan string) []InsuranceRequirement {
	kind, channel := "", ""
	switch insuranceNormalize(plan) {
	case "aetna healthy kids", "community care plan", "doctors health medicare",
		"florida community care", "florida complete care", "miami childrens health plan",
		"miami children s health plan", "simply medicaid", "simply medicare":
		kind, channel = "precertification", "ehealthdeck"
	case "childrens medical services", "children s medical services", "staywell medicare", "sunshine medicaid", "wellcare", "wellcare medicaid":
		kind, channel = "benefits_review", "envolve"
	case "cigna hmo", "united healthcare nhp hmo only", "ambetter value", "molina medicare", "seminole tribe":
		kind = "pcp_referral"
	case "cigna medicare advantage", "cigna medicare advantage hmo":
		kind, channel = "pcp_referral", "availity"
	case "florida blue hmo":
		kind, channel = "prior_authorization", "emi_portal"
	case "umr", "aetna epo university of miami", "aetna epo north broward", "avmed", "avmed select":
		kind = "network_review"
	case "tricare for life":
		kind = "secondary_coverage"
	}
	if kind == "" {
		return nil
	}
	return []InsuranceRequirement{requirement(kind, channel)}
}

func medicalReferenceConflict(plan string) string {
	switch insuranceNormalize(plan) {
	case "humana ppo", "humana ppo pos":
		return "The document uses HUMPPO, while AMD lists HUM PPO; staff must reconcile the exact commercial product code."
	case "united healthcare all savers":
		return "The document uses ALL1, while AMD lists ALL 1; staff must reconcile the carrier code."
	case "sunhealth":
		return "The document uses SUNHE, while AMD lists SUNHEALT for the discount plan; staff must reconcile the code and patient-pay arrangement."
	case "clear spring health", "southbay medical center":
		return "The document requires a staff-arranged medical attachment or special self-pay rate for this product."
	case "partners direct health":
		return "The document has no billing code for Partners Direct Health; the underlying carrier must be confirmed."
	case "humana healthy horizons", "tricare forever", "florida blue medicare hmo":
		return "This medical product does not have an unambiguous billing mapping in the document."
	}
	return ""
}
