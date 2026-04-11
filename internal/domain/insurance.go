package domain

// RoutingRule determines which providers a patient can see based on their insurance.
type RoutingRule string

const (
	RoutingNotAccepted RoutingRule = "not_accepted"
	RoutingBachOnly    RoutingRule = "bach_only"
	RoutingBachLicht   RoutingRule = "bach_licht"
	RoutingAll         RoutingRule = "all_three"
)

// InsuranceEntry maps an insurance name to its AMD carrier ID and routing rule.
type InsuranceEntry struct {
	CarrierID        string
	Routing          RoutingRule
	PreauthRequired  bool
}

// InsuranceNameMap maps LLM-provided insurance names to carrier ID + routing.
// Keys are normalized (lowercase, no punctuation) via NormalizeForLookup.
// Grouped by carrier ID so you can see which plans share a network.
var InsuranceNameMap = map[string]InsuranceEntry{

	// ── iCare Network — car40907 (11 plans) ─────────────────────────────
	"aetna better health":            {CarrierID: "car40907", Routing: RoutingAll},
	"aetna better health of florida": {CarrierID: "car40907", Routing: RoutingAll},
	"aetna healthy kids":             {CarrierID: "car40907", Routing: RoutingAll},
	"aetna hmo":                      {CarrierID: "car40907", Routing: RoutingAll, PreauthRequired: true},
	"aetna medicare hmo":             {CarrierID: "car40907", Routing: RoutingAll},
	"community care plan":            {CarrierID: "car40907", Routing: RoutingAll},
	"florida community care":         {CarrierID: "car40907", Routing: RoutingAll},
	"florida complete care":          {CarrierID: "car40907", Routing: RoutingAll},
	"miami childrens health plan":    {CarrierID: "car40907", Routing: RoutingAll},
	"simply medicaid":                {CarrierID: "car40907", Routing: RoutingAll},
	"vivida":                         {CarrierID: "car40907", Routing: RoutingAll},
	"eye care health solutions":      {CarrierID: "car40907", Routing: RoutingAll},
	"icare":                          {CarrierID: "car40907", Routing: RoutingAll},
	"doctors health medicare":        {CarrierID: "car40907", Routing: RoutingNotAccepted},

	// ── United Healthcare — car40923 (12 plans) ─────────────────────────
	"united healthcare":                    {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare aarp medicare":      {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare all savers":         {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare golden rule":        {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare nhp":                {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare shared services":    {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare student resources":  {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare hmo":                {CarrierID: "car40923", Routing: RoutingAll, PreauthRequired: true},
	"united healthcare surest":             {CarrierID: "car40923", Routing: RoutingAll},
	"umr":                                  {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare choice":              {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare dual complete":       {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare individual exchange": {CarrierID: "car40923", Routing: RoutingBachLicht},
	"preferred care partners":              {CarrierID: "car40923", Routing: RoutingNotAccepted},

	// ── Envolve Network — car281245 (8 plans) ───────────────────────────
	"ambetter":                {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter premier":        {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter select":         {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter value":          {CarrierID: "car281245", Routing: RoutingAll},
	"childrens medical services": {CarrierID: "car281245", Routing: RoutingAll},
	"envolve vision":          {CarrierID: "car281245", Routing: RoutingAll},
	"staywell medicare":       {CarrierID: "car281245", Routing: RoutingAll},
	"sunshine medicaid":       {CarrierID: "car281245", Routing: RoutingAll},
	"wellcare":                {CarrierID: "car281245", Routing: RoutingAll},

	// ── Humana Consolidated — car308175 (8 plans) ───────────────────────
	"humana gold plus":        {CarrierID: "car308175", Routing: RoutingBachOnly, PreauthRequired: true},
	"humana medicaid":         {CarrierID: "car308175", Routing: RoutingBachOnly, PreauthRequired: true},
	"humana medicare":         {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana ppo":              {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana healthy horizons":  {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana premier hmo":      {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"humana hmo":              {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"molina medicare":         {CarrierID: "car308175", Routing: RoutingBachOnly},
	"cigna medicare advantage": {CarrierID: "car308175", Routing: RoutingBachLicht},
	"molina marketplace":      {CarrierID: "car308175", Routing: RoutingNotAccepted},

	// ── Florida Blue — car40897 (7 plans) ───────────────────────────────
	"florida blue":                      {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue medicare ppo":         {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue ppo federal employee": {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue medicare hmo":         {CarrierID: "car40897", Routing: RoutingAll, PreauthRequired: true},
	"florida blue ppo out of state":     {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue steward tier 1":       {CarrierID: "car40897", Routing: RoutingBachOnly},
	"florida blueselect":                {CarrierID: "car40897", Routing: RoutingNotAccepted},

	// ── Cigna — car301345 (5 plans) ─────────────────────────────────────
	"cigna hmo":                       {CarrierID: "car301345", Routing: RoutingAll, PreauthRequired: true},
	"cigna miami dade public schools": {CarrierID: "car301345", Routing: RoutingAll},
	"cigna open access":               {CarrierID: "car301345", Routing: RoutingAll},
	"cigna ppo":                       {CarrierID: "car301345", Routing: RoutingAll},
	"cigna local plus":                {CarrierID: "car301345", Routing: RoutingBachOnly},

	// ── Aetna — car40887 (4 plans) ──────────────────────────────────────
	"aetna":                              {CarrierID: "car40887", Routing: RoutingAll},
	"aetna medicare signature ppo":       {CarrierID: "car40887", Routing: RoutingAll},
	"aetna qhp individual exchange":      {CarrierID: "car40887", Routing: RoutingAll},
	"aetna epo north broward":       {CarrierID: "car40887", Routing: RoutingBachOnly},
	"aetna epo university of miami": {CarrierID: "car40887", Routing: RoutingNotAccepted},

	// ── Tricare — car40921 (4 plans) ────────────────────────────────────
	"tricare prime":       {CarrierID: "car40921", Routing: RoutingBachLicht, PreauthRequired: true},
	"tricare select":      {CarrierID: "car40921", Routing: RoutingBachLicht},
	"tricare for life":    {CarrierID: "car40921", Routing: RoutingBachLicht},
	"tricare forever":     {CarrierID: "car40921", Routing: RoutingBachLicht, PreauthRequired: true},

	// ── Standalone Carriers (1 plan each) ───────────────────────────────
	"avmed medicare advantage": {CarrierID: "car301737", Routing: RoutingNotAccepted},  // EMI
	"florida blue hmo":         {CarrierID: "car280750", Routing: RoutingNotAccepted},  // EMI
	"eye america aao":          {CarrierID: "car308627", Routing: RoutingBachOnly},
	"meritain health":          {CarrierID: "car301578", Routing: RoutingBachOnly},
	"avmed":                    {CarrierID: "car40890", Routing: RoutingBachLicht},
	"oscar health":             {CarrierID: "car284233", Routing: RoutingBachLicht},
	"florida medicaid":         {CarrierID: "car40899", Routing: RoutingAll},
	"florida medicare":         {CarrierID: "car40900", Routing: RoutingAll},
	"imagine health":           {CarrierID: "car308142", Routing: RoutingAll},
	"medicaid":                 {CarrierID: "car303033", Routing: RoutingAll},
	"molina medicaid":          {CarrierID: "car40912", Routing: RoutingAll},
	"multiplan phcs":           {CarrierID: "car301648", Routing: RoutingAll},
	"sunhealth":                {CarrierID: "car308086", Routing: RoutingAll},
	"united healthcare global": {CarrierID: "car284971", Routing: RoutingAll},

	// ── Not Accepted at Spring Hill (Medical) ────────────────────────────
	"care plus":          {CarrierID: "", Routing: RoutingNotAccepted},
	"optimum healthcare": {CarrierID: "", Routing: RoutingNotAccepted},
	"care health plus":   {CarrierID: "", Routing: RoutingNotAccepted},
}

// CarrierRoutingMap maps AMD carrier IDs to routing rules for existing patients.
// Used when we get the carrier ID from demographics.
// For the 5 ambiguous carriers, we default to RoutingAll (most permissive).
var CarrierRoutingMap = map[string]RoutingRule{
	// NOT ACCEPTED (unambiguous carriers only)
	"car281648": RoutingNotAccepted, // DOCTORS HEALTHCARE PLANS INC
	"car40916":  RoutingNotAccepted, // PREFERRED CARE PARTNERS
	"car301737": RoutingNotAccepted, // EYE MANAGEMENT INC (AvMed Medicare via EMI)
	"car280750": RoutingNotAccepted, // EYE MANAGEMENT INC (FL Blue HMO via EMI)
	"car303061": RoutingNotAccepted, // HUMANA PREMIER HMO
	// BACH ONLY
	"car303033": RoutingBachOnly, // HUMANA MEDICAID
	"car40906":  RoutingBachOnly, // HUMANA MEDICARE
	"car303062": RoutingBachOnly, // HUMANA PPO POS
	"car308175": RoutingBachOnly, // HUMANA GOLD PLUS
	"car308627": RoutingBachOnly, // EYECARE AMERICA AAO
	"car301578": RoutingBachOnly, // MERITAIN HEALTH
	// BACH + LICHT
	"car40890":  RoutingBachLicht, // AVMED
	"car302890": RoutingBachLicht, // CIGNA MEDICARE ADVTG HEALTHSPRING
	"car284233": RoutingBachLicht, // OSCAR INSURANCE COMPANY OF FLORIDA
	"car284327": RoutingBachLicht, // TRICARE EAST
	"car40921":  RoutingBachLicht, // TRICARE FOR LIFE
	"car40922":  RoutingBachLicht, // TRICARE NORTH AND SOUTH REGIONS
}

// AmbiguousCarriers are carrier IDs that span multiple routing tiers.
// When we get these from demographics, we default to All 3 but flag it.
var AmbiguousCarriers = map[string]bool{
	"car40887":  true, // AETNA
	"car40897":  true, // FLORIDA BLUE SHIELD
	"car40912":  true, // MOLINA HEALTHCARE OF FLORIDA
	"car40923":  true, // UNITED HEALTHCARE
	"car301345": true, // CIGNA HMO
}

// InsuranceAliases maps common shorthand names to canonical InsuranceNameMap keys.
// Catches what patients naturally say and what the LLM might truncate to.
// Only alias when ALL plans under that shorthand share the same routing.
// Do NOT alias ambiguous parent names (e.g., "molina" spans 3 routing tiers).
var InsuranceAliases = map[string]string{
	// Parent company shorthand → safest canonical name
	"oscar":          "oscar health",
	"oscar insurance": "oscar health",
	"humana":         "humana ppo",
	"tricare":        "tricare select",
	"united":         "united healthcare",
	"uhc":            "united healthcare",
	"uhc medicare":   "united healthcare aarp medicare",
	"cigna":          "cigna ppo",
	"blue cross":     "florida blue",
	"bcbs":               "florida blue",
	"bcbs medicare hmo":  "florida blue medicare hmo",
	"medicare":       "florida medicare",
	"sunshine":       "sunshine medicaid",
	"sunshine health": "sunshine medicaid",
	"staywell":       "staywell medicare",
	"simply":         "simply medicaid",
	"simply healthcare": "simply medicaid",
	"simply health":     "simply medicaid",
	"simply health plans": "simply medicaid",
	"multiplan":      "multiplan phcs",
	"phcs":           "multiplan phcs",
	"imagine":        "imagine health",
	"envolve":        "envolve vision",
	"meritain":       "meritain health",
	"eye america":    "eye america aao",
	"preferred care": "preferred care partners",
	"community care": "community care plan",
	"doctors health": "doctors health medicare",
	"miami childrens": "miami childrens health plan",
	"childrens medical": "childrens medical services",
	"sun health":          "sunhealth",
	"duocomplete":         "united healthcare dual complete",
	"duo complete":        "united healthcare dual complete",
	"uhc dual complete":   "united healthcare dual complete",
	"uhc choice":          "united healthcare choice",
	"icare health solutions": "icare",
	"eye care health":     "eye care health solutions",
	"optimum":             "optimum healthcare",
	"care health":         "care health plus",
}

// LookupInsurance looks up an insurance name and returns its entry.
// First tries exact match in InsuranceNameMap, then checks InsuranceAliases.
// Uses NormalizeForLookup for tolerance of punctuation, casing, and spacing.
func LookupInsurance(name string) (InsuranceEntry, bool) {
	normalized := NormalizeForLookup(name)

	// Exact match
	if entry, ok := InsuranceNameMap[normalized]; ok {
		return entry, ok
	}

	// Alias match
	if canonical, ok := InsuranceAliases[normalized]; ok {
		entry, ok := InsuranceNameMap[canonical]
		return entry, ok
	}

	return InsuranceEntry{}, false
}

// RoutingForCarrierID returns the routing rule for a carrier ID from demographics.
// Returns the rule and whether the carrier is ambiguous (shared across tiers).
// Unknown carrier IDs default to RoutingAll (most permissive).
func RoutingForCarrierID(carrierID string) (RoutingRule, bool) {
	ambiguous := AmbiguousCarriers[carrierID]

	if rule, ok := CarrierRoutingMap[carrierID]; ok {
		return rule, ambiguous
	}

	// Unknown or ambiguous carriers default to all three
	return RoutingAll, ambiguous
}

// ParseRoutingRule converts a string back to a typed RoutingRule.
// Used by the availability handler to parse the routing param from the request.
func ParseRoutingRule(s string) RoutingRule {
	switch RoutingRule(s) {
	case RoutingNotAccepted:
		return RoutingNotAccepted
	case RoutingBachOnly:
		return RoutingBachOnly
	case RoutingBachLicht:
		return RoutingBachLicht
	case RoutingAll:
		return RoutingAll
	default:
		return RoutingAll
	}
}
