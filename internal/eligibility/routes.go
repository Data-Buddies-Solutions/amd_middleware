package eligibility

import "advancedmd-token-management/internal/domain"

type payerRoute struct {
	payer  string
	review string
}

// Explicit product routes for the Florida office catalogs. Never derive these
// from AMD carrier IDs or office acceptance/network rules. Directory support was
// verified on 2026-09-16 against Stedi's public export (subset in testdata):
// https://payers.us.stedi.com/2024-04-01/public/payers/csv
var insurancePayers = map[string]payerRoute{
	// Newly cataloged products remain blocked until their eligibility route is verified.
	"avmed entrust":                   {"", "plan_route_review"},
	"avmed jackson first network hmo": {"", "plan_route_review"},
	"cigna florida connect epo":       {"", "plan_route_review"},
	"cigna sure fit":                  {"", "plan_route_review"},
	"clear spring health":             {"", "plan_route_review"},
	"humana":                          {"", "payer_product_required"},
	"humana care plus":                {"", "payer_product_required"},
	"leon health plan":                {"", "plan_route_review"},
	"molina":                          {"", "payer_product_required"},
	"seminole tribe":                  {"", "plan_route_review"},
	"southbay medical center":         {"", "plan_route_review"},
	"tricare":                         {"", "payer_product_required"},
	"united healthcare medicare":      {"", "plan_route_review"},
	// Exact spelling variants of products already verified below.
	"children's medical services":           {"68069", ""},
	"miami children's health plan":          {"82832", "payer_eligibility_not_supported"},
	"florida blue select":                   {"BCBSF", ""},
	"simply health":                         {"SMPLY", ""},
	"simply medicare (medical)":             {"SMPLY", ""},
	"uhc dual complete":                     {"87726", ""},
	"ambetter from sunshine health":         {"68069", ""},
	"united health care":                    {"87726", ""},
	"aetna":                                 {"60054", ""},
	"aetna better health":                   {"128FL", ""},
	"aetna better health of florida":        {"128FL", ""},
	"aetna commercial":                      {"60054", ""},
	"aetna commercial hmo":                  {"60054", ""},
	"aetna commercial ppo":                  {"60054", ""},
	"aetna epo":                             {"60054", ""},
	"aetna epo north broward":               {"60054", ""},
	"aetna epo university of miami":         {"60054", ""},
	"aetna healthy kids":                    {"128FL", ""},
	"aetna hmo":                             {"60054", ""},
	"aetna managed choice":                  {"60054", ""},
	"aetna medicare":                        {"60054", ""},
	"aetna medicare commercial":             {"60054", ""},
	"aetna medicare hmo":                    {"60054", ""},
	"aetna medicare ppo":                    {"60054", ""},
	"aetna medicare signature ppo":          {"60054", ""},
	"aetna ppo":                             {"60054", ""},
	"aetna qhp individual exchange":         {"60054", ""},
	"alivi":                                 {"ALIVI", "payer_eligibility_not_supported"},
	"ambetter":                              {"68069", ""},
	"ambetter premier":                      {"68069", ""},
	"ambetter select":                       {"68069", ""},
	"ambetter value":                        {"68069", ""},
	"avmed":                                 {"59274", ""},
	"avmed medicare advantage":              {"59274", ""},
	"avmed select":                          {"59274", ""},
	"care health plus":                      {"", "payer_product_required"},
	"care plus":                             {"95092", ""},
	"careplus medicare medical":             {"95092", ""},
	"childrens medical services":            {"68069", ""},
	"cigna":                                 {"62308", ""},
	"cigna hmo":                             {"62308", ""},
	"cigna local plus":                      {"62308", ""},
	"cigna medicare advantage":              {"63092", ""},
	"cigna medicare advantage healthspring": {"63092", ""},
	"cigna medicare advantage hmo":          {"63092", ""},
	"cigna medicare advantage ppo":          {"63092", ""},
	"cigna miami dade public schools":       {"62308", ""},
	"cigna open access":                     {"62308", ""},
	"cigna ppo":                             {"62308", ""},
	"community care plan":                   {"", "payer_product_required"},
	"davis":                                 {"00157", ""},
	"devoted medicare hmo":                  {"DEVOT", ""},
	"devoted medicare ppo":                  {"DEVOT", ""},
	"doctors health medicare":               {"DRHCP", ""},
	"envolve":                               {"46278", ""},
	"envolve vision":                        {"46278", ""},
	"eye america aao":                       {"", "assistance_program_not_insurance"},
	"eye care health solutions":             {"", "underlying_payer_required"},
	"eyemed":                                {"31165", "payer_eligibility_not_supported"},
	"florida blue":                          {"BCBSF", ""},
	"florida blue hmo":                      {"BCBSF", ""},
	"florida blue medicare hmo":             {"FBM01", ""},
	"florida blue medicare ppo":             {"FBM01", ""},
	"florida blue ppo federal employee":     {"BCBSF", ""},
	"florida blue ppo out of state":         {"", "payer_product_required"},
	"florida blue steward tier 1":           {"BCBSF", ""},
	"florida blueselect":                    {"BCBSF", ""},
	"florida community care":                {"FLCCR", "payer_eligibility_not_supported"},
	"florida complete care":                 {"FLCPC", "payer_eligibility_not_supported"},
	"florida medicaid":                      {"77027", ""},
	"florida medicare":                      {"CMS", "original_medicare_enrollment_and_traceability_required"},
	"freedom health medicare":               {"41212", ""},
	"guardian":                              {"64246", ""},
	"humana gold plus":                      {"61101", ""},
	"humana healthy horizons":               {"61101", ""},
	"humana hmo":                            {"61101", ""},
	"humana medicaid":                       {"61101", ""},
	"humana medicaid hmo":                   {"61101", ""},
	"humana medicare":                       {"61101", ""},
	"humana medicare hmo":                   {"61101", ""},
	"humana medicare ppo":                   {"61101", ""},
	"humana ppo":                            {"61101", ""},
	"humana ppo pos":                        {"61101", ""},
	"humana premier hmo":                    {"61101", ""},
	"icare":                                 {"26054", "payer_eligibility_not_supported"},
	"imagine health":                        {"", "underlying_payer_required"},
	"medicaid":                              {"", "payer_product_required"},
	"medicare":                              {"CMS", "original_medicare_enrollment_and_traceability_required"},
	"meritain health":                       {"41124", ""},
	"miami childrens health plan":           {"82832", "payer_eligibility_not_supported"},
	"molina marketplace":                    {"51062", ""},
	"molina medicaid":                       {"51062", ""},
	"molina medicare":                       {"51062", ""},
	"multiplan phcs":                        {"", "underlying_payer_required"},
	"nva":                                   {"NVADM", "payer_eligibility_not_supported"},
	"optimum healthcare":                    {"20133", ""},
	"original medicare":                     {"CMS", "original_medicare_enrollment_and_traceability_required"},
	"oscar":                                 {"OSCAR", ""},
	"oscar health":                          {"OSCAR", ""},
	"partners direct health":                {"", "underlying_payer_required"},
	"preferred care network":                {"78857", ""},
	"preferred care partners":               {"65088", ""},
	"preferred care partners medical":       {"65088", ""},
	"premier":                               {"65054", "payer_eligibility_not_supported"},
	"self pay":                              {"", "self_pay"},
	"simply medicaid":                       {"SMPLY", ""},
	"simply medicare":                       {"SMPLY", ""},
	"solis medicare":                        {"SOLIS", ""},
	"solstice":                              {"76578", "payer_eligibility_not_supported"},
	"spectera":                              {"00773", ""},
	"staywell medicare":                     {"", "current_plan_card_required"},
	"sunhealth":                             {"", "discount_plan_not_insurance"},
	"sunhealth discount plan":               {"", "discount_plan_not_insurance"},
	"sunshine medicaid":                     {"68069", ""},
	"tricare for life":                      {"TDFIC", ""},
	"tricare forever":                       {"", "payer_product_required"},
	"tricare prime":                         {"99727", ""},
	"tricare select":                        {"99727", ""},
	"uhc community plan":                    {"04567", ""},
	"umr":                                   {"39026", ""},
	"united healthcare":                     {"87726", ""},
	"united healthcare aarp medicare":       {"87726", ""},
	"united healthcare all savers":          {"81400", ""},
	"united healthcare choice":              {"87726", ""},
	"united healthcare community plan":      {"04567", ""},
	"united healthcare dual complete":       {"87726", ""},
	"united healthcare global":              {"", "payer_product_required"},
	"united healthcare global medical":      {"", "payer_product_required"},
	"united healthcare golden rule":         {"37602", ""},
	"united healthcare hmo":                 {"87726", ""},
	"united healthcare individual exchange": {"87726", ""},
	"united healthcare nhp":                 {"87726", ""},
	"united healthcare nhp hmo access":      {"87726", ""},
	"united healthcare nhp hmo only":        {"87726", ""},
	"united healthcare oxford":              {"06111", ""},
	"united healthcare shared services":     {"39026", ""},
	"united healthcare student resources":   {"74227", ""},
	"united healthcare surest":              {"25463", ""},
	"us health group":                       {"62324", ""},
	"vivida":                                {"", "current_plan_card_required"},
	"vsp":                                   {"94163", "payer_eligibility_not_supported"},
	"wellcare":                              {"14163", ""},
	"wellcare medicaid":                     {"", "current_plan_card_required"},
	"wellcare medicare lppo":                {"14163", ""},
}

// Route returns the Stedi payer identifier even when eligibility is unsupported,
// plus the reason that prevents dispatch. Aliases are exact, never fuzzy matches.
func Route(plan, serviceDate string) (payer, review string) {
	if !validDOB(serviceDate) {
		return "", "invalid_service_date"
	}
	key := domain.NormalizeForLookup(plan)
	// Scheduling aliases can collapse products that require different payer routes.
	switch key {
	case "blue cross", "bcbs", "bcbs medicare hmo", "preferred care", "tricare", "united health one", "staywell",
		"preferred care network preferred care partners",
		"preferred care network preferred care partners (medical)",
		"preferred care network preferred care partners medical":
		return "", "payer_product_required"
	}
	route, ok := insurancePayers[key]
	if !ok {
		canonical, recognized := domain.CanonicalInsuranceName(plan)
		if !recognized {
			return "", "unrecognized_plan"
		}
		key = canonical
		route, ok = insurancePayers[key]
	}
	if !ok {
		return "", "plan_route_review"
	}
	// AHCA moves both CMS Plan populations from Sunshine to Molina on 2026-10-01.
	// https://ahca.myflorida.com/medicaid/statewide-medicaid-managed-care/2025-2030-smmc-plans/cms-plan-transition.html
	// Select on the requested service day, never the execution date. Historical
	// pre-Sunshine records need the actual plan/card rather than a guessed route.
	if key == "childrens medical services" || key == "children's medical services" {
		if serviceDate < "20211001" {
			return "", "current_plan_card_required"
		}
		if serviceDate >= "20261001" {
			return "51062", ""
		}
	}
	return route.payer, route.review
}
