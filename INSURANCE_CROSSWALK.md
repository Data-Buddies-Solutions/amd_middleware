# Insurance Crosswalk — Spring Hill Location

Source: Abita Insurance List - SpringHill Location rev 9.4.2025
Source of truth: `internal/domain/insurance.go`

## How It Works

The LLM has a fixed list of insurance names in its TOOLS prompt. When a patient says their insurance, the LLM picks the matching name and sends it as a string. The middleware maps name → carrier ID + routing rule.

- **Existing patient**: `verify-patient` pulls carrier ID from AMD demographics → middleware looks up routing → returns allowed providers
- **New patient**: LLM sends insurance name → middleware maps to carrier ID (for `addpatient`) + routing (for scheduling)
- **Scheduling**: routing param on availability endpoint → middleware filters columns

## Routing Rules

| Rule | Columns | Providers |
|------|---------|-----------|
| **Not Accepted** | none | Self-pay only |
| **Bach Only** | 1513 | Dr. Bach |
| **Bach + Licht** | 1513, 1551 | Dr. Bach, Dr. Licht |
| **All 3** (default) | 1513, 1551, 1550 | Dr. Bach, Dr. Licht, Dr. Noel |

---

## Carrier ID Groupings

85 insurance names consolidate down to **22 carrier IDs**. The 8 major networks cover 68 plans; 14 standalone carriers cover 1 plan each; 3 additional plans are hard-rejected with no carrier ID.

---

### iCare — car40907 (14 plans)

| Insurance Name | Routing |
|---------------|---------|
| Aetna Better Health | All 3 |
| Aetna Better Health of Florida | All 3 |
| Aetna Healthy Kids | All 3 |
| Aetna HMO | All 3 |
| Aetna Medicare HMO | All 3 |
| Community Care Plan | All 3 |
| Eye Care Health Solutions | All 3 |
| Florida Community Care | All 3 |
| Florida Complete Care | All 3 |
| iCare | All 3 |
| Miami Children's Health Plan | All 3 |
| Simply Medicaid | All 3 |
| Vivida | All 3 |
| Doctors Health Medicare | **Not Accepted** |

---

### United Healthcare — car40923 (14 plans)

| Insurance Name | Routing |
|---------------|---------|
| United Healthcare | All 3 |
| United Healthcare AARP Medicare | All 3 |
| United Healthcare All Savers | All 3 |
| United Healthcare Choice | All 3 |
| United Healthcare Dual Complete | All 3 |
| United Healthcare Golden Rule | All 3 |
| United Healthcare HMO | All 3 |
| United Healthcare NHP | All 3 |
| United Healthcare Shared Services | All 3 |
| United Healthcare Student Resources | All 3 |
| United Healthcare Surest | All 3 |
| UMR | All 3 |
| United Healthcare Individual Exchange | Bach + Licht |
| Preferred Care Partners | **Not Accepted** |

---

### Envolve — car281245 (9 plans)

| Insurance Name | Routing |
|---------------|---------|
| Ambetter | All 3 |
| Ambetter Premier | All 3 |
| Ambetter Select | All 3 |
| Ambetter Value | All 3 |
| Children's Medical Services | All 3 |
| Envolve Vision | All 3 |
| Staywell Medicare | All 3 |
| Sunshine Medicaid | All 3 |
| Wellcare | All 3 |

---

### Humana Consolidated — car308175 (10 plans)

| Insurance Name | Routing |
|---------------|---------|
| Humana Gold Plus | Bach Only |
| Humana Healthy Horizons | Bach Only |
| Humana Medicaid | Bach Only |
| Humana Medicare | Bach Only |
| Humana PPO | Bach Only |
| Molina Medicare | Bach Only |
| Cigna Medicare Advantage | Bach + Licht |
| Humana HMO | **Not Accepted** |
| Humana Premier HMO | **Not Accepted** |
| Molina Marketplace | **Not Accepted** |

---

### Florida Blue — car40897 (7 plans)

| Insurance Name | Routing |
|---------------|---------|
| Florida Blue | All 3 |
| Florida Blue Medicare HMO | All 3 |
| Florida Blue Medicare PPO | All 3 |
| Florida Blue PPO Federal Employee | All 3 |
| Florida Blue PPO Out of State | All 3 |
| Florida Blue Steward Tier 1 | Bach Only |
| Florida BlueSelect | **Not Accepted** |

---

### Cigna — car301345 (5 plans)

| Insurance Name | Routing |
|---------------|---------|
| Cigna HMO | All 3 |
| Cigna Miami-Dade Public Schools | All 3 |
| Cigna Open Access | All 3 |
| Cigna PPO | All 3 |
| Cigna Local Plus | Bach Only |

---

### Aetna — car40887 (5 plans)

| Insurance Name | Routing |
|---------------|---------|
| Aetna | All 3 |
| Aetna Medicare Signature PPO | All 3 |
| Aetna QHP Individual Exchange | All 3 |
| Aetna EPO North Broward | Bach Only |
| Aetna EPO University of Miami | **Not Accepted** |

---

### Tricare — car40921 (4 plans)

| Insurance Name | Routing |
|---------------|---------|
| Tricare Prime | Bach + Licht |
| Tricare Select | Bach + Licht |
| Tricare for Life | Bach + Licht |
| Tricare Forever | Bach + Licht |

---

### Standalone Carriers (14 plans, 1 each)

| Insurance Name | Carrier ID | Routing |
|---------------|-----------|---------|
| AvMed Medicare Advantage | car301737 | **Not Accepted** |
| Florida Blue HMO | car280750 | **Not Accepted** |
| Eye America AAO | car308627 | Bach Only |
| Meritain Health | car301578 | Bach Only |
| AvMed | car40890 | Bach + Licht |
| Oscar Health | car284233 | Bach + Licht |
| Florida Medicaid | car40899 | All 3 |
| Florida Medicare | car40900 | All 3 |
| Imagine Health | car308142 | All 3 |
| Medicaid | car303033 | All 3 |
| Molina Medicaid | car40912 | All 3 |
| Multiplan PHCS | car301648 | All 3 |
| SunHealth | car308086 | All 3 |
| United Healthcare Global | car284971 | All 3 |

---

### Not Accepted — No Carrier ID (3 plans)

Hard-rejected by name before any AMD lookup. No carrier ID is stored because these plans are never attached to patients at Spring Hill.

| Insurance Name | Routing |
|---------------|---------|
| Care Plus | **Not Accepted** |
| Care Health Plus | **Not Accepted** |
| Optimum Healthcare | **Not Accepted** |

---

## Ambiguous Carriers (Existing Patients)

These 5 carrier IDs appear across multiple routing tiers in the name map. When we get one from an existing patient's demographics, we can't determine the specific plan — so we default to **All 3** and set `routingAmbiguous: true` so the agent asks a clarifying question.

| Carrier ID | Label | Plans Spanning |
|-----------|-------|---------------|
| car40887 | AETNA | Not Accepted + Bach Only + All 3 |
| car40897 | FLORIDA BLUE SHIELD | Not Accepted + Bach Only + All 3 |
| car40923 | UNITED HEALTHCARE | Not Accepted + Bach + Licht + All 3 |
| car301345 | CIGNA HMO | Bach Only + All 3 |
| car40912 | MOLINA HEALTHCARE OF FLORIDA | All 3 (ambiguous historically) |

---

## Carrier Routing Map (Existing Patients)

For existing patients, these unambiguous carrier IDs map to a fixed routing rule when found in demographics. Anything not listed defaults to All 3.

| Carrier ID | Label | Routing |
|-----------|-------|---------|
| car281648 | DOCTORS HEALTHCARE PLANS INC | Not Accepted |
| car40916 | PREFERRED CARE PARTNERS | Not Accepted |
| car301737 | EYE MANAGEMENT INC (AvMed Medicare) | Not Accepted |
| car280750 | EYE MANAGEMENT INC (FL Blue HMO) | Not Accepted |
| car303061 | HUMANA PREMIER HMO | Not Accepted |
| car303033 | HUMANA MEDICAID | Bach Only |
| car40906 | HUMANA MEDICARE | Bach Only |
| car303062 | HUMANA PPO POS | Bach Only |
| car308175 | HUMANA GOLD PLUS | Bach Only |
| car308627 | EYECARE AMERICA AAO | Bach Only |
| car301578 | MERITAIN HEALTH | Bach Only |
| car40890 | AVMED | Bach + Licht |
| car302890 | CIGNA MEDICARE ADVTG HEALTHSPRING | Bach + Licht |
| car284233 | OSCAR INSURANCE CO OF FLORIDA | Bach + Licht |
| car284327 | TRICARE EAST | Bach + Licht |
| car40921 | TRICARE FOR LIFE | Bach + Licht |
| car40922 | TRICARE NORTH AND SOUTH REGIONS | Bach + Licht |

---

## Edge Cases

- **Patient doesn't know insurance** → agent books without filtering, office verifies at check-in
- **Insurance not in the list** → agent tells patient it may not be accepted, offers self-pay or suggests calling the office
- **Existing patient has ambiguous carrier** → agent asks clarifying question about plan type (e.g., "I see you have Aetna — is that a regular PPO, an EPO, or a Medicare plan?")
- **Existing patient's carrier ID not in CarrierRoutingMap** → default to All 3 (most permissive)

---

## Implementation

See `internal/domain/insurance.go` for:
- `InsuranceNameMap` — name → carrier ID + routing (new patients)
- `CarrierRoutingMap` — carrier ID → routing (existing patients)
- `AmbiguousCarriers` — carrier IDs that span multiple tiers
- `ColumnsForRouting()` — routing rule → scheduler column IDs
- `ProvidersForRouting()` — routing rule → display names
- `LookupInsurance()` — normalized name lookup
- `RoutingForCarrierID()` — demographics carrier lookup with ambiguity flag
