package domain

import "strings"

// RoutingRule determines which providers a patient can see based on their insurance.
type RoutingRule string

const (
	RoutingNotAccepted RoutingRule = "not_accepted"
	RoutingBachOnly    RoutingRule = "bach_only"
	RoutingBachLicht   RoutingRule = "bach_licht"
	RoutingAll         RoutingRule = "all_three"
	RoutingOpticalOnly RoutingRule = "optical_only"
)

// InsuranceEntry maps an insurance name to its AMD carrier ID and routing rule.
type InsuranceEntry struct {
	CarrierID       string
	Routing         RoutingRule
	PreauthRequired bool
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
	"miami childrens health plan":    {CarrierID: "car40907", Routing: RoutingNotAccepted},
	"simply medicaid":                {CarrierID: "car40907", Routing: RoutingAll},
	"vivida":                         {CarrierID: "car40907", Routing: RoutingAll},
	"eye care health solutions":      {CarrierID: "car40907", Routing: RoutingAll},
	"icare":                          {CarrierID: "car40907", Routing: RoutingAll},
	"doctors health medicare":        {CarrierID: "car40907", Routing: RoutingNotAccepted},

	// ── United Healthcare — car40923 (12 plans) ─────────────────────────
	"united healthcare":                     {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare aarp medicare":       {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare all savers":          {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare golden rule":         {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare nhp":                 {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare shared services":     {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare student resources":   {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare hmo":                 {CarrierID: "car40923", Routing: RoutingAll, PreauthRequired: true},
	"united healthcare surest":              {CarrierID: "car40923", Routing: RoutingAll},
	"umr":                                   {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare choice":              {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare dual complete":       {CarrierID: "car40923", Routing: RoutingAll},
	"united healthcare individual exchange": {CarrierID: "car40923", Routing: RoutingBachLicht},
	"preferred care partners":               {CarrierID: "car40923", Routing: RoutingNotAccepted},

	// ── Envolve Network — car281245 (8 plans) ───────────────────────────
	"ambetter":                   {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter premier":           {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter select":            {CarrierID: "car281245", Routing: RoutingAll},
	"ambetter value":             {CarrierID: "car281245", Routing: RoutingAll},
	"childrens medical services": {CarrierID: "car281245", Routing: RoutingAll},
	"envolve vision":             {CarrierID: "car281245", Routing: RoutingAll},
	"staywell medicare":          {CarrierID: "car281245", Routing: RoutingAll},
	"sunshine medicaid":          {CarrierID: "car281245", Routing: RoutingAll},
	"wellcare":                   {CarrierID: "car281245", Routing: RoutingAll},

	// ── Humana Consolidated — car308175 (8 plans) ───────────────────────
	"humana gold plus":         {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"humana medicaid":          {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"humana medicare":          {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana ppo":               {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana healthy horizons":  {CarrierID: "car308175", Routing: RoutingBachOnly},
	"humana premier hmo":       {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"humana hmo":               {CarrierID: "car308175", Routing: RoutingNotAccepted},
	"molina medicare":          {CarrierID: "car308175", Routing: RoutingBachOnly},
	"cigna medicare advantage": {CarrierID: "car308175", Routing: RoutingBachLicht},
	"molina marketplace":       {CarrierID: "car308175", Routing: RoutingNotAccepted},

	// ── Florida Blue — car40897 (7 plans) ───────────────────────────────
	"florida blue":                      {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue medicare ppo":         {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue ppo federal employee": {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue medicare hmo":         {CarrierID: "car40897", Routing: RoutingAll, PreauthRequired: true},
	"florida blue ppo out of state":     {CarrierID: "car40897", Routing: RoutingAll},
	"florida blue steward tier 1":       {CarrierID: "car40897", Routing: RoutingNotAccepted},
	"florida blueselect":                {CarrierID: "car40897", Routing: RoutingNotAccepted},

	// ── Cigna — car301345 (5 plans) ─────────────────────────────────────
	"cigna":                           {CarrierID: "car301345", Routing: RoutingNotAccepted},
	"cigna hmo":                       {CarrierID: "car301345", Routing: RoutingAll, PreauthRequired: true},
	"cigna miami dade public schools": {CarrierID: "car301345", Routing: RoutingNotAccepted},
	"cigna open access":               {CarrierID: "car301345", Routing: RoutingAll},
	"cigna ppo":                       {CarrierID: "car301345", Routing: RoutingAll},
	"cigna local plus":                {CarrierID: "car301345", Routing: RoutingNotAccepted},

	// ── Aetna — car40887 ────────────────────────────────────────────────
	"aetna":                         {CarrierID: "car40887", Routing: RoutingAll},
	"aetna commercial":              {CarrierID: "car40887", Routing: RoutingAll},
	"aetna commercial ppo":          {CarrierID: "car40887", Routing: RoutingAll},
	"aetna managed choice":          {CarrierID: "car40887", Routing: RoutingAll},
	"aetna medicare":                {CarrierID: "car40887", Routing: RoutingAll},
	"aetna medicare ppo":            {CarrierID: "car40887", Routing: RoutingAll},
	"aetna medicare signature ppo":  {CarrierID: "car40887", Routing: RoutingAll},
	"aetna ppo":                     {CarrierID: "car40887", Routing: RoutingAll},
	"aetna qhp individual exchange": {CarrierID: "car40887", Routing: RoutingAll},
	"aetna epo":                     {CarrierID: "car40887", Routing: RoutingNotAccepted},
	"aetna epo north broward":       {CarrierID: "car40887", Routing: RoutingNotAccepted},
	"aetna epo university of miami": {CarrierID: "car40887", Routing: RoutingNotAccepted},

	// ── Tricare — car40921 (4 plans) ────────────────────────────────────
	"tricare prime":    {CarrierID: "car40921", Routing: RoutingBachLicht, PreauthRequired: true},
	"tricare select":   {CarrierID: "car40921", Routing: RoutingBachLicht},
	"tricare for life": {CarrierID: "car40921", Routing: RoutingBachLicht},
	"tricare forever":  {CarrierID: "car40921", Routing: RoutingBachLicht, PreauthRequired: true},

	// ── Standalone Carriers (1 plan each) ───────────────────────────────
	"avmed medicare advantage": {CarrierID: "car301737", Routing: RoutingNotAccepted}, // EMI
	"florida blue hmo":         {CarrierID: "car280750", Routing: RoutingNotAccepted}, // EMI
	"eye america aao":          {CarrierID: "car308627", Routing: RoutingNotAccepted},
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
// Ambiguous carriers default to RoutingAll (most permissive).
var CarrierRoutingMap = map[string]RoutingRule{
	// NOT ACCEPTED (unambiguous carriers only)
	"car281648": RoutingNotAccepted, // DOCTORS HEALTHCARE PLANS INC
	"car40916":  RoutingNotAccepted, // PREFERRED CARE PARTNERS
	"car301737": RoutingNotAccepted, // EYE MANAGEMENT INC (AvMed Medicare via EMI)
	"car280750": RoutingNotAccepted, // EYE MANAGEMENT INC (FL Blue HMO via EMI)
	"car303061": RoutingNotAccepted, // HUMANA PREMIER HMO
	"car308627": RoutingNotAccepted, // EYECARE AMERICA AAO
	// BACH ONLY
	"car303033": RoutingBachOnly, // HUMANA MEDICAID
	"car40906":  RoutingBachOnly, // HUMANA MEDICARE
	"car303062": RoutingBachOnly, // HUMANA PPO POS
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
	"car40907":  true, // ICARE / MEDICAID FAMILY
	"car40912":  true, // MOLINA HEALTHCARE OF FLORIDA
	"car40923":  true, // UNITED HEALTHCARE
	"car301345": true, // CIGNA HMO
	"car308175": true, // HUMANA CONSOLIDATED
}

// InsuranceAliases maps common shorthand names to canonical InsuranceNameMap keys.
// Catches what patients naturally say and what the LLM might truncate to.
// Only alias when ALL plans under that shorthand share the same routing.
// Do NOT alias ambiguous parent names (e.g., "molina" spans 3 routing tiers).
var InsuranceAliases = map[string]string{
	// Parent company shorthand → safest canonical name
	"oscar":                         "oscar health",
	"oscar insurance":               "oscar health",
	"humana":                        "humana ppo",
	"tricare":                       "tricare select",
	"united":                        "united healthcare",
	"uhc":                           "united healthcare",
	"uhc medicare":                  "united healthcare aarp medicare",
	"cigna miami dade":              "cigna miami dade public schools",
	"miami dade public schools":     "cigna miami dade public schools",
	"blue cross":                    "florida blue",
	"bcbs":                          "florida blue",
	"bcbs medicare hmo":             "florida blue medicare hmo",
	"fl blue hmo":                   "florida blue hmo",
	"fl blue select":                "florida blueselect",
	"fl blue steward":               "florida blue steward tier 1",
	"florida blue select":           "florida blueselect",
	"florida blue steward":          "florida blue steward tier 1",
	"medicare":                      "florida medicare",
	"sunshine":                      "sunshine medicaid",
	"sunshine health":               "sunshine medicaid",
	"staywell":                      "staywell medicare",
	"simply":                        "simply medicaid",
	"simply healthcare":             "simply medicaid",
	"simply health":                 "simply medicaid",
	"simply health plans":           "simply medicaid",
	"multiplan":                     "multiplan phcs",
	"phcs":                          "multiplan phcs",
	"imagine":                       "imagine health",
	"envolve":                       "envolve vision",
	"meritain":                      "meritain health",
	"eye america":                   "eye america aao",
	"preferred care":                "preferred care partners",
	"community care":                "community care plan",
	"doctors health":                "doctors health medicare",
	"miami dade doctors health":     "doctors health medicare",
	"miami dade doctors healthcare": "doctors health medicare",
	"miami dade ddoctors health":    "doctors health medicare",
	"miami childrens":               "miami childrens health plan",
	"miami children's":              "miami childrens health plan",
	"miami children's health plan":  "miami childrens health plan",
	"miami children":                "miami childrens health plan",
	"childrens medical":             "childrens medical services",
	"children's medical":            "childrens medical services",
	"children's medical services":   "childrens medical services",
	"sun health":                    "sunhealth",
	"duocomplete":                   "united healthcare dual complete",
	"duo complete":                  "united healthcare dual complete",
	"uhc dual complete":             "united healthcare dual complete",
	"uhc choice":                    "united healthcare choice",
	"icare health solutions":        "icare",
	"eye care health":               "eye care health solutions",
	"optimum":                       "optimum healthcare",
	"care health":                   "care health plus",
	"av med medicare":               "avmed medicare advantage",
	"av med medicare advantage":     "avmed medicare advantage",
}

func medicalBachOnly(carrierID string) InsuranceEntry {
	return InsuranceEntry{CarrierID: carrierID, Routing: RoutingBachOnly}
}

func medicalBachOnlyPreauth(carrierID string) InsuranceEntry {
	return InsuranceEntry{CarrierID: carrierID, Routing: RoutingBachOnly, PreauthRequired: true}
}

// hollywoodSweetwaterMedicalInsuranceNameMap is sourced from the Abita Eye Group
// 5/4/2026 insurance list using the A.Bach medical column. These offices only
// schedule medical visits on Dr. Austin Bach's columns.
var hollywoodSweetwaterMedicalInsuranceNameMap = map[string]InsuranceEntry{
	// Aetna / Availity
	"aetna":                         medicalBachOnly("car40887"),
	"aetna commercial":              medicalBachOnly("car40887"),
	"aetna commercial hmo":          medicalBachOnly("car40887"),
	"aetna commercial ppo":          medicalBachOnly("car40887"),
	"aetna epo":                     medicalBachOnly("car40887"),
	"aetna epo north broward":       medicalBachOnly("car40887"),
	"aetna epo university of miami": medicalBachOnly("car40887"),
	"aetna managed choice":          medicalBachOnly("car40887"),
	"aetna ppo":                     medicalBachOnly("car40887"),
	"aetna qhp individual exchange": medicalBachOnly("car40887"),

	// Aetna Medicare / Medicaid plans administered through iCare on this list.
	"aetna better health":            medicalBachOnly("car40907"),
	"aetna better health of florida": medicalBachOnly("car40907"),
	"aetna healthy kids":             medicalBachOnly("car40907"),
	"aetna hmo":                      medicalBachOnlyPreauth("car40907"),
	"aetna medicare":                 medicalBachOnly("car40907"),
	"aetna medicare hmo":             medicalBachOnly("car40907"),
	"aetna medicare ppo":             medicalBachOnly("car40907"),

	// Envolve
	"ambetter":                   medicalBachOnly("car281245"),
	"ambetter premier":           medicalBachOnly("car281245"),
	"ambetter select":            medicalBachOnly("car281245"),
	"ambetter value":             medicalBachOnly("car281245"),
	"childrens medical services": medicalBachOnly("car281245"),
	"staywell medicare":          medicalBachOnly("car281245"),
	"sunshine medicaid":          medicalBachOnly("car281245"),
	"wellcare":                   medicalBachOnly("car281245"),
	"wellcare medicaid":          medicalBachOnly("car281245"),

	// AvMed / Availity
	"avmed":        medicalBachOnly("car40890"),
	"avmed select": medicalBachOnly("car40890"),

	// Premier Eye Care medical plans
	"careplus medicare medical": medicalBachOnlyPreauth("car281317"),
	"devoted medicare hmo":      medicalBachOnly("car281317"),
	"devoted medicare ppo":      medicalBachOnly("car281317"),
	"solis medicare":            medicalBachOnlyPreauth("car281317"),
	"wellcare medicare lppo":    medicalBachOnlyPreauth("car281317"),

	// Cigna
	"cigna hmo":                       medicalBachOnlyPreauth("car301345"),
	"cigna miami dade public schools": medicalBachOnly("car301345"),
	"cigna open access":               medicalBachOnly("car301345"),
	"cigna ppo":                       medicalBachOnly("car301345"),

	// iCare
	"community care plan":       medicalBachOnly("car40907"),
	"doctors health medicare":   medicalBachOnly("car40907"),
	"florida community care":    medicalBachOnly("car40907"),
	"florida complete care":     medicalBachOnly("car40907"),
	"freedom health medicare":   medicalBachOnly("car40907"),
	"optimum healthcare":        medicalBachOnly("car40907"),
	"simply medicaid":           medicalBachOnly("car40907"),
	"simply medicare":           medicalBachOnly("car40907"),
	"eye care health solutions": medicalBachOnly("car40907"),
	"icare":                     medicalBachOnly("car40907"),

	// Florida Blue / EMI
	"florida blue":                      medicalBachOnly("car40897"),
	"florida blue hmo":                  medicalBachOnlyPreauth("car280750"),
	"florida blue medicare hmo":         medicalBachOnlyPreauth("car40897"),
	"florida blue medicare ppo":         medicalBachOnly("car40897"),
	"florida blue ppo federal employee": medicalBachOnly("car40897"),
	"florida blue ppo out of state":     medicalBachOnly("car40897"),
	"florida blue steward tier 1":       medicalBachOnly("car40897"),

	// Humana / Availity / iCare
	"humana hmo":          medicalBachOnly("car308175"),
	"humana medicaid":     medicalBachOnlyPreauth("car308175"),
	"humana medicare":     medicalBachOnly("car308175"),
	"humana medicare hmo": medicalBachOnly("car40907"),
	"humana medicare ppo": medicalBachOnly("car308175"),
	"humana ppo":          medicalBachOnly("car308175"),
	"humana ppo pos":      medicalBachOnly("car308175"),
	"humana premier hmo":  medicalBachOnly("car308175"),

	// Standalone / other medical networks already known to AMD.
	"eye america aao":                  medicalBachOnly("car308627"),
	"florida medicaid":                 medicalBachOnly("car40899"),
	"florida medicare":                 medicalBachOnly("car40900"),
	"imagine health":                   medicalBachOnly("car308142"),
	"medicaid":                         medicalBachOnly("car303033"),
	"medicare":                         medicalBachOnly("car40900"),
	"meritain health":                  medicalBachOnly("car301578"),
	"molina medicaid":                  medicalBachOnly("car40912"),
	"molina medicare":                  medicalBachOnly("car308175"),
	"multiplan phcs":                   medicalBachOnly("car301648"),
	"oscar health":                     medicalBachOnly("car284233"),
	"partners direct health":           medicalBachOnly("car308142"),
	"preferred care partners":          medicalBachOnly("car40923"),
	"preferred care network":           medicalBachOnly("car40923"),
	"sunhealth":                        medicalBachOnly("car308086"),
	"united healthcare global":         medicalBachOnly("car284971"),
	"united healthcare global medical": medicalBachOnly("car284971"),

	// Tricare
	"tricare prime":    medicalBachOnlyPreauth("car40921"),
	"tricare select":   medicalBachOnly("car40921"),
	"tricare for life": medicalBachOnly("car40921"),
	"tricare forever":  medicalBachOnlyPreauth("car40921"),

	// United Healthcare
	"preferred care partners medical":       medicalBachOnly("car40923"),
	"umr":                                   medicalBachOnly("car40923"),
	"united healthcare":                     medicalBachOnly("car40923"),
	"united healthcare aarp medicare":       medicalBachOnly("car40923"),
	"united healthcare all savers":          medicalBachOnly("car40923"),
	"united healthcare golden rule":         medicalBachOnly("car40923"),
	"united healthcare hmo":                 medicalBachOnlyPreauth("car40923"),
	"united healthcare individual exchange": medicalBachOnly("car40923"),
	"united healthcare nhp":                 medicalBachOnly("car40923"),
	"united healthcare nhp hmo access":      medicalBachOnly("car40923"),
	"united healthcare nhp hmo only":        medicalBachOnly("car40923"),
	"united healthcare oxford":              medicalBachOnly("car40923"),
	"united healthcare shared services":     medicalBachOnly("car40923"),
	"united healthcare student resources":   medicalBachOnly("car40923"),
	"united healthcare surest":              medicalBachOnly("car40923"),
	"us health group":                       medicalBachOnly("car40923"),
}

var hollywoodSweetwaterMedicalInsuranceAliases = map[string]string{
	"aetna commercial hmo and ppo":         "aetna commercial",
	"aetna commercial hmo ppo":             "aetna commercial",
	"aetna epo plan north broward":         "aetna epo north broward",
	"aetna epo plan university miami":      "aetna epo university of miami",
	"aetna epo plan university of miami":   "aetna epo university of miami",
	"aetna healthy kids kid care":          "aetna healthy kids",
	"aetna medicare ppo medical":           "aetna medicare ppo",
	"aetna qhp":                            "aetna qhp individual exchange",
	"aetna qhp individual exchange plan":   "aetna qhp individual exchange",
	"ambetter medical":                     "ambetter",
	"av med":                               "avmed",
	"care plus":                            "careplus medicare medical",
	"careplus":                             "careplus medicare medical",
	"childrens medical":                    "childrens medical services",
	"children's medical":                   "childrens medical services",
	"children's medical services":          "childrens medical services",
	"cigna miami-dade public schools":      "cigna miami dade public schools",
	"devoted":                              "devoted medicare hmo",
	"doctors health":                       "doctors health medicare",
	"fl blue hmo":                          "florida blue hmo",
	"fl blue steward":                      "florida blue steward tier 1",
	"florida blue steward":                 "florida blue steward tier 1",
	"freedom":                              "freedom health medicare",
	"freedom health":                       "freedom health medicare",
	"humana":                               "humana ppo",
	"humana premier hmo access":            "humana premier hmo",
	"medicare part b":                      "medicare",
	"meritain":                             "meritain health",
	"multiplan":                            "multiplan phcs",
	"optimum":                              "optimum healthcare",
	"optimum healthplan":                   "optimum healthcare",
	"optimum healthplan medicare":          "optimum healthcare",
	"oscar":                                "oscar health",
	"oscar health plans":                   "oscar health",
	"partners direct":                      "partners direct health",
	"phcs":                                 "multiplan phcs",
	"preferred care":                       "preferred care partners",
	"preferred care partners medical":      "preferred care partners",
	"simply":                               "simply medicaid",
	"simply healthcare":                    "simply medicaid",
	"simply medicare medical":              "simply medicare",
	"solis":                                "solis medicare",
	"straight medicaid":                    "medicaid",
	"tricare":                              "tricare select",
	"uhc":                                  "united healthcare",
	"uhc medicare":                         "united healthcare aarp medicare",
	"united":                               "united healthcare",
	"united aarp medicare complete":        "united healthcare aarp medicare",
	"united healthcare medicare advantage": "united healthcare aarp medicare",
	"united healthcare student resources medical": "united healthcare student resources",
	"united health one":                           "umr",
	"ushealth":                                    "us health group",
	"ushealth group":                              "us health group",
	"wellcare medicaid medical":                   "wellcare medicaid",
	"wellcare medicare":                           "wellcare medicare lppo",
	"wellcare medicare lppo medical":              "wellcare medicare lppo",
}

var hollywoodSweetwaterAcceptedMedicalCarrierIDs = map[string]bool{
	"car280750": true, // Florida Blue HMO via EMI
	"car281245": true, // Envolve
	"car281317": true, // Premier Eye Care
	"car284233": true, // Oscar
	"car284327": true, // Tricare East
	"car284971": true, // UHC Global
	"car301345": true, // Cigna
	"car301578": true, // Meritain
	"car301648": true, // MultiPlan / PHCS
	"car302890": true, // Cigna Medicare Advantage HealthSpring
	"car303033": true, // Medicaid / Humana Medicaid legacy bucket
	"car303062": true, // Humana PPO/POS
	"car308086": true, // SunHealth
	"car308142": true, // Imagine Health
	"car308175": true, // Humana consolidated
	"car308627": true, // Eye America AAO
	"car40887":  true, // Aetna
	"car40890":  true, // AvMed
	"car40897":  true, // Florida Blue
	"car40899":  true, // Florida Medicaid
	"car40900":  true, // Medicare
	"car40906":  true, // Humana Medicare
	"car40907":  true, // iCare
	"car40912":  true, // Molina Medicaid
	"car40916":  true, // Preferred Care Partners legacy carrier
	"car40921":  true, // Tricare
	"car40922":  true, // Tricare North/South
	"car40923":  true, // United Healthcare
}

func isHollywoodSweetwaterMedicalOffice(office *OfficeConfig) bool {
	return office != nil && (office.ID == "hollywood" || office.ID == "sweetwater")
}

// VisionInsuranceNameMap maps accepted routine-vision insurance buckets to AMD carrier IDs.
// It is used only when a request explicitly asks for routine-vision coverage.
var VisionInsuranceNameMap = map[string]InsuranceEntry{
	"vsp":                     {CarrierID: "car280695", Routing: RoutingOpticalOnly},
	"eyemed":                  {CarrierID: "car280684", Routing: RoutingOpticalOnly},
	"nva":                     {CarrierID: "car308794", Routing: RoutingOpticalOnly},
	"davis":                   {CarrierID: "car280612", Routing: RoutingOpticalOnly},
	"spectera":                {CarrierID: "car308790", Routing: RoutingOpticalOnly},
	"solstice":                {CarrierID: "car301652", Routing: RoutingOpticalOnly},
	"icare":                   {CarrierID: "car40907", Routing: RoutingOpticalOnly},
	"guardian":                {CarrierID: "car308792", Routing: RoutingOpticalOnly},
	"alivi":                   {CarrierID: "car308796", Routing: RoutingOpticalOnly},
	"premier":                 {CarrierID: "car281317", Routing: RoutingOpticalOnly},
	"envolve":                 {CarrierID: "car281245", Routing: RoutingOpticalOnly},
	"sunhealth":               {CarrierID: "car308791", Routing: RoutingOpticalOnly},
	"sunhealth discount plan": {CarrierID: "car308791", Routing: RoutingOpticalOnly},
}

// VisionInsuranceAliases maps patient-facing routine-vision plan names to the
// billing buckets above, based on the vision insurance workbook.
var VisionInsuranceAliases = map[string]string{
	"eye med":                        "eyemed",
	"eye med vision":                 "eyemed",
	"eye med vision care":            "eyemed",
	"national vision":                "nva",
	"national vision administrators": "nva",
	"davis vision":                   "davis",
	"spectera vision":                "spectera",
	"soltice":                        "solstice",
	"solstice vision":                "solstice",
	"guardian vision":                "guardian",
	"alivi health":                   "alivi",
	"envolve vision":                 "envolve",
	"sun health":                     "sunhealth",
	"sunhealth vision":               "sunhealth",

	// VSP
	"metlife":           "vsp",
	"liberty financial": "vsp",
	"lincoln financial": "vsp",
	"lincoln finacial":  "vsp",

	// EyeMed
	"humana": "eyemed",
	"aetna":  "eyemed",
	"unum":   "eyemed",
	"cigna":  "eyemed",

	// Davis
	"superior":     "davis",
	"florida blue": "davis",
	"blueview":     "davis",
	"blue view":    "davis",
	"versant":      "davis",

	// Spectera
	"united healthcare":  "spectera",
	"united health care": "spectera",
	"united vision":      "spectera",

	// iCare
	"humana gold plus":    "icare",
	"simply medicare":     "icare",
	"simply medicaid":     "icare",
	"simply medcaid":      "icare",
	"freedom":             "icare",
	"optimum":             "icare",
	"optimum healthcare":  "icare",
	"aetna better health": "icare",
	"avmed":               "icare",

	// Envolve
	"ambetter":                      "envolve",
	"ambetter from sunshine health": "envolve",
	"sunshine":                      "envolve",

	// Premier
	"amerihealth": "premier",
	"devoted":     "premier",

	// Alivi
	"careplus":  "alivi",
	"care plus": "alivi",
}

func lookupInsuranceEntry(name string, entries map[string]InsuranceEntry, aliases map[string]string) (InsuranceEntry, string, bool) {
	normalized := NormalizeForLookup(name)

	if entry, ok := entries[normalized]; ok {
		return entry, normalized, ok
	}

	if canonical, ok := aliases[normalized]; ok {
		entry, ok := entries[canonical]
		return entry, canonical, ok
	}

	return InsuranceEntry{}, "", false
}

func lookupInsuranceFromMaps(name string, entries map[string]InsuranceEntry, aliases map[string]string) (InsuranceEntry, bool) {
	entry, _, ok := lookupInsuranceEntry(name, entries, aliases)
	return entry, ok
}

// LookupInsurance looks up an insurance name and returns its entry.
// First tries exact match in InsuranceNameMap, then checks InsuranceAliases.
// Uses NormalizeForLookup for tolerance of punctuation, casing, and spacing.
func LookupInsurance(name string) (InsuranceEntry, bool) {
	return lookupInsuranceFromMaps(name, InsuranceNameMap, InsuranceAliases)
}

// LookupInsuranceForCoverage chooses the medical or routine-vision crosswalk.
func LookupInsuranceForCoverage(name string, mode InsuranceMode) (InsuranceEntry, bool) {
	if mode == InsuranceModeVision {
		return lookupInsuranceFromMaps(name, VisionInsuranceNameMap, VisionInsuranceAliases)
	}
	return LookupInsurance(name)
}

var crystalRiverRejectedMedicalPlans = map[string]bool{
	// Crystal River inherits the Spring Hill medical rejection list from
	// InsuranceNameMap and adds the plans/families below.
	"aetna better health":            true,
	"aetna better health of florida": true,
	"aetna healthy kids":             true,
	"ambetter":                       true,
	"ambetter premier":               true,
	"ambetter select":                true,
	"ambetter value":                 true,
	"community care plan":            true,
	"florida community care":         true,
	"florida complete care":          true,
	"florida medicaid":               true,
	"humana healthy horizons":        true,
	"medicaid":                       true,
	"molina medicaid":                true,
	"simply medicaid":                true,
	"staywell medicare":              true,
	"sunshine medicaid":              true,
	"vivida":                         true,
}

var crystalRiverRejectedCarrierIDs = map[string]bool{
	"car281245": true, // Ambetter / Staywell / Sunshine family
	"car303033": true, // Medicaid / Humana Medicaid legacy bucket
	"car40899":  true, // Florida Medicaid
	"car40907":  true, // iCare / Medicaid family
	"car40912":  true, // Molina Medicaid
}

var ambiguousDemographicCarrierNames = map[string]bool{
	"aetna":                  true,
	"bcbs":                   true,
	"blue cross":             true,
	"blue cross blue shield": true,
	"cigna":                  true,
	"florida blue":           true,
	"humana":                 true,
	"molina":                 true,
	"uhc":                    true,
	"united":                 true,
	"united health care":     true,
	"united healthcare":      true,
}

func applyOfficeMedicalInsurancePolicy(entry InsuranceEntry, canonicalName string, office *OfficeConfig) InsuranceEntry {
	if office == nil {
		return entry
	}
	if office.ID == "crystal_river" {
		if crystalRiverRejectedMedicalPlans[canonicalName] {
			entry.Routing = RoutingNotAccepted
			entry.PreauthRequired = false
		}
		return entry
	}
	return entry
}

// LookupInsuranceForCoverageAtOffice chooses the medical or routine-vision
// crosswalk and applies office-specific medical acceptance rules.
func LookupInsuranceForCoverageAtOffice(name string, mode InsuranceMode, office *OfficeConfig) (InsuranceEntry, bool) {
	if mode == InsuranceModeVision {
		return lookupInsuranceFromMaps(name, VisionInsuranceNameMap, VisionInsuranceAliases)
	}

	if isHollywoodSweetwaterMedicalOffice(office) {
		if entry, _, ok := lookupInsuranceEntry(name, hollywoodSweetwaterMedicalInsuranceNameMap, hollywoodSweetwaterMedicalInsuranceAliases); ok {
			return entry, true
		}
		entry, canonicalName, ok := lookupInsuranceEntry(name, InsuranceNameMap, InsuranceAliases)
		if !ok {
			return InsuranceEntry{}, false
		}
		if officeEntry, ok := hollywoodSweetwaterMedicalInsuranceNameMap[canonicalName]; ok {
			return officeEntry, true
		}
		if ambiguousDemographicCarrierNames[NormalizeForLookup(name)] || ambiguousDemographicCarrierNames[canonicalName] {
			return InsuranceEntry{}, false
		}
		if entry.Routing == RoutingNotAccepted {
			return entry, true
		}
		return InsuranceEntry{}, false
	}

	entry, canonicalName, ok := lookupInsuranceEntry(name, InsuranceNameMap, InsuranceAliases)
	if !ok {
		return InsuranceEntry{}, false
	}

	return applyOfficeMedicalInsurancePolicy(entry, canonicalName, office), true
}

// InsuranceModeForCoverage converts an agent-supplied coverage type to a middleware insurance mode.
func InsuranceModeForCoverage(coverageType string) InsuranceMode {
	switch strings.ToLower(strings.TrimSpace(coverageType)) {
	case "routine_vision", "optical_only":
		return InsuranceModeVision
	}

	switch NormalizeForLookup(coverageType) {
	case "routine vision", "vision", "optical", "optical only":
		return InsuranceModeVision
	default:
		return InsuranceModeMedical
	}
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

// RoutingForCarrierIDAtOffice applies office-specific medical acceptance rules
// to the demographics carrier-ID fallback used for existing patients.
func RoutingForCarrierIDAtOffice(carrierID string, office *OfficeConfig) (RoutingRule, bool) {
	if office != nil && office.ID == "crystal_river" && crystalRiverRejectedCarrierIDs[carrierID] {
		return RoutingNotAccepted, false
	}
	if isHollywoodSweetwaterMedicalOffice(office) {
		if hollywoodSweetwaterAcceptedMedicalCarrierIDs[carrierID] {
			return RoutingBachOnly, AmbiguousCarriers[carrierID]
		}
		return RoutingNotAccepted, false
	}
	return RoutingForCarrierID(carrierID)
}

// RoutingForDemographicInsurance prefers AMD's carrier name when available, then
// falls back to carrier ID. Carrier IDs can represent mixed accepted/rejected plans.
func RoutingForDemographicInsurance(carrierID, carrierName string, office *OfficeConfig) (RoutingRule, bool) {
	if isHollywoodSweetwaterMedicalOffice(office) {
		if entry, _, ok := lookupInsuranceEntry(carrierName, hollywoodSweetwaterMedicalInsuranceNameMap, hollywoodSweetwaterMedicalInsuranceAliases); ok {
			return entry.Routing, false
		}
		if entry, canonicalName, ok := lookupInsuranceEntry(carrierName, InsuranceNameMap, InsuranceAliases); ok {
			if officeEntry, ok := hollywoodSweetwaterMedicalInsuranceNameMap[canonicalName]; ok {
				return officeEntry.Routing, false
			}
			if ambiguousDemographicCarrierNames[NormalizeForLookup(carrierName)] || ambiguousDemographicCarrierNames[canonicalName] {
				return RoutingForCarrierIDAtOffice(carrierID, office)
			}
			if entry.Routing == RoutingNotAccepted {
				return RoutingNotAccepted, false
			}
			return RoutingNotAccepted, false
		}
		return RoutingForCarrierIDAtOffice(carrierID, office)
	}

	if entry, canonicalName, ok := lookupInsuranceEntry(carrierName, InsuranceNameMap, InsuranceAliases); ok {
		if AmbiguousCarriers[carrierID] && ambiguousDemographicCarrierNames[NormalizeForLookup(carrierName)] {
			return RoutingForCarrierIDAtOffice(carrierID, office)
		}
		entry = applyOfficeMedicalInsurancePolicy(entry, canonicalName, office)
		return entry.Routing, false
	}

	return RoutingForCarrierIDAtOffice(carrierID, office)
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
	case RoutingOpticalOnly:
		return RoutingOpticalOnly
	default:
		return RoutingAll
	}
}
