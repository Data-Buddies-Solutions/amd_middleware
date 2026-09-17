# Insurance products → Stedi eligibility payers

Verified **2026-09-16** from the live [Stedi payer directory](https://www.stedi.com/healthcare/network)
and its [public CSV export](https://payers.us.stedi.com/2024-04-01/public/payers/csv).
All **133 distinct product labels** in the general medical, Hollywood/Sweetwater
medical, and routine-vision catalogs have an explicit disposition: **103 can be
routed**, **30 require review or are not insurance**. Existing aliases also work;
ambiguous scheduling aliases are deliberately blocked. This is a Florida-office
crosswalk, not a national mapping of generic Medicaid or Blue Cross names.

The executable source is `internal/eligibility/routes.go`. Tests check catalog
completeness and payer support against the small public directory snapshot in
`internal/eligibility/testdata/stedi-payers-20260916.csv`. No patient data or
credentials are in that snapshot. Primary payer IDs are strings, retaining
leading zeroes, and are valid Stedi `tradingPartnerServiceId` values.

All executable routes below support eligibility without transaction enrollment
in the verified directory. Every outgoing request still uses **STC 30 only**.
Eligibility support does not change an office's acceptance policy or establish
medical/vision visit coverage, provider network participation, or copay.

## Executable product mappings

| Our product labels | Stedi payer | Payer ID |
| --- | --- | --- |
| aetna, aetna commercial, aetna commercial hmo, aetna commercial ppo, aetna epo, aetna epo north broward, aetna epo university of miami, aetna hmo, aetna managed choice, aetna medicare, aetna medicare commercial, aetna medicare hmo, aetna medicare ppo, aetna medicare signature ppo, aetna ppo, aetna qhp individual exchange | [Aetna](https://www.stedi.com/healthcare/network/HPQRS) | `60054` |
| aetna better health, aetna better health of florida, aetna healthy kids | [Aetna Better Health Florida](https://www.stedi.com/healthcare/network/POYUE) | `128FL` |
| united healthcare all savers | [All Savers](https://www.stedi.com/healthcare/network/AFLEJ) | `81400` |
| avmed, avmed medicare advantage, avmed select | [AvMed](https://www.stedi.com/healthcare/network/NOGQI) | `59274` |
| care plus, careplus medicare medical | [CarePlus](https://www.stedi.com/healthcare/network/LVFMN) | `95092` |
| ambetter, ambetter premier, ambetter select, ambetter value, childrens medical services, sunshine medicaid | [Centene (Medical)](https://www.stedi.com/healthcare/network/IMJGY) | `68069` |
| envolve, envolve vision | [Centene Dental and Vision Services](https://www.stedi.com/healthcare/network/CTWJH) | `46278` |
| cigna, cigna hmo, cigna local plus, cigna miami dade public schools, cigna open access, cigna ppo | [Cigna](https://www.stedi.com/healthcare/network/HGJLR) | `62308` |
| davis | [Davis Vision](https://www.stedi.com/healthcare/network/QKZWY) | `00157` |
| devoted medicare hmo, devoted medicare ppo | [Devoted Health](https://www.stedi.com/healthcare/network/ZRSYL) | `DEVOT` |
| doctors health medicare | [Doctors HealthCare Plans](https://www.stedi.com/healthcare/network/ASQFX) | `DRHCP` |
| florida blue, florida blue hmo, florida blue ppo federal employee, florida blue steward tier 1, florida blueselect | [Florida Blue](https://www.stedi.com/healthcare/network/ULXRI) | `BCBSF` |
| florida blue medicare hmo, florida blue medicare ppo | [Florida Blue Medicare](https://www.stedi.com/healthcare/network/TRQGK) | `FBM01` |
| freedom health medicare | [Freedom Health](https://www.stedi.com/healthcare/network/KCEJC) | `41212` |
| us health group | [Freedom Life Insurance of America](https://www.stedi.com/healthcare/network/FXORF) | `62324` |
| united healthcare golden rule | [Golden Rule](https://www.stedi.com/healthcare/network/INRAO) | `37602` |
| guardian | [Guardian](https://www.stedi.com/healthcare/network/EOFHN) | `64246` |
| cigna medicare advantage, cigna medicare advantage healthspring, cigna medicare advantage hmo, cigna medicare advantage ppo | [HealthSpring](https://www.stedi.com/healthcare/network/QRPMU) | `63092` |
| humana gold plus, humana healthy horizons, humana hmo, humana medicaid, humana medicaid hmo, humana medicare, humana medicare hmo, humana medicare ppo, humana ppo, humana ppo pos, humana premier hmo | [Humana](https://www.stedi.com/healthcare/network/UYORK) | `61101` |
| florida medicaid | [Medicaid Florida](https://www.stedi.com/healthcare/network/ZDEIS) | `77027` |
| meritain health | [Meritain Health](https://www.stedi.com/healthcare/network/NPFPE) | `41124` |
| molina marketplace, molina medicaid, molina medicare | [Molina Healthcare Florida](https://www.stedi.com/healthcare/network/IPMKW) | `51062` |
| optimum healthcare | [Optimum Healthcare](https://www.stedi.com/healthcare/network/MNYSB) | `20133` |
| spectera | [Optum Health Vision](https://www.stedi.com/healthcare/network/ZNIZJ) | `00773` |
| oscar, oscar health | [Oscar Health](https://www.stedi.com/healthcare/network/TAZWT) | `OSCAR` |
| united healthcare oxford | [Oxford](https://www.stedi.com/healthcare/network/CYRGF) | `06111` |
| preferred care network | [Preferred Care Network](https://www.stedi.com/healthcare/network/NSJHP) | `78857` |
| preferred care partners, preferred care partners medical | [Preferred Care Partners](https://www.stedi.com/healthcare/network/PDERF) | `65088` |
| simply medicaid, simply medicare | [Simply Healthcare](https://www.stedi.com/healthcare/network/POEXK) | `SMPLY` |
| solis medicare | [Solis Health Plans](https://www.stedi.com/healthcare/network/PCOFI) | `SOLIS` |
| united healthcare student resources | [Student Resources (UnitedHealthcare)](https://www.stedi.com/healthcare/network/WVPHC) | `74227` |
| united healthcare surest | [Surest](https://www.stedi.com/healthcare/network/QSOCK) | `25463` |
| tricare prime, tricare select | [TRICARE East](https://www.stedi.com/healthcare/network/IYHIG) | `99727` |
| tricare for life | [TRICARE for Life](https://www.stedi.com/healthcare/network/EPIVM) | `TDFIC` |
| umr, united healthcare shared services | [UMR](https://www.stedi.com/healthcare/network/UWTOI) | `39026` |
| united healthcare, united healthcare aarp medicare, united healthcare choice, united healthcare dual complete, united healthcare hmo, united healthcare individual exchange, united healthcare nhp, united healthcare nhp hmo access, united healthcare nhp hmo only | [UnitedHealthcare](https://www.stedi.com/healthcare/network/KMQTZ) | `87726` |
| wellcare, wellcare medicare lppo | [Wellcare](https://www.stedi.com/healthcare/network/EGASA) | `14163` |

**Children's Medical Services is date-dependent:** `68069` applies from
2021-10-01 through 2026-09-30; `51062` (Molina Healthcare Florida) applies from
2026-10-01. Earlier service dates require card review. This is Florida's pediatric
CMS Plan, **not Original Medicare**. The rule uses the requested service date.
[AHCA's transition notice](https://ahca.myflorida.com/medicaid/statewide-medicaid-managed-care/2025-2030-smmc-plans/cms-plan-transition.html)
confirms both plan populations move to Molina on October 1; [Sunshine's earlier transition guidance](https://www.sunshinehealth.com/newsroom/Guidelines-for-proper-claims-submissions.html)
identifies the Sunshine/CMS transition starting October 2021. The same guidance
confirms Florida WellCare Medicaid transferred to Sunshine; its legacy label
requires the current card, while WellCare Medicare retains its separate route.

Additional exact card labels `united healthcare community plan` and
`uhc community plan` route to `04567`; they do not inherit the generic UHC route.

## Review / no-request dispositions

Known IDs below remain visible as `payerId` in the response, but middleware sends
nothing when a review reason is present.

| Our product labels | Known payer ID | Reason |
| --- | --- | --- |
| eye america aao | — | Assistance program, not insurance |
| staywell medicare, vivida, wellcare medicaid | — | Legacy product; obtain the current plan card before selecting a successor |
| sunhealth, sunhealth discount plan | — | Discount plan, not insurance |
| care health plus, community care plan, florida blue ppo out of state, medicaid, tricare forever, united healthcare global, united healthcare global medical | — | Identify the actual product/state/administrator on the card |
| self pay | — | Self-pay; no payer request |
| eye care health solutions, imagine health, multiplan phcs, partners direct health | — | Network or administrator name is insufficient; identify the underlying payer |
| icare | `26054` | Stedi lists this payer but does not support its eligibility transaction |
| eyemed | `31165` | Stedi lists this payer but does not support its eligibility transaction |
| premier | `65054` | Stedi lists this payer but does not support its eligibility transaction |
| solstice | `76578` | Stedi lists this payer but does not support its eligibility transaction |
| miami childrens health plan | `82832` | Stedi lists this payer but does not support its eligibility transaction |
| vsp | `94163` | Stedi lists this payer but does not support its eligibility transaction |
| alivi | `ALIVI` | Stedi lists this payer but does not support its eligibility transaction |
| florida medicare, medicare | `CMS` | Original Medicare: provider enrollment and CMS traceability are not enabled |
| florida community care | `FLCCR` | Stedi lists this payer but does not support its eligibility transaction |
| florida complete care | `FLCPC` | Stedi lists this payer but does not support its eligibility transaction |
| nva | `NVADM` | Stedi lists this payer but does not support its eligibility transaction |

The local `icare` is **iCare Health Options TPA (`26054`)**, not the unrelated
supported insurer called iCare (`11695`). Community Care Plan has multiple
unsupported directory records (`59064`, `59065`, `FHKC1`); its unspecific local
label is not assigned one arbitrarily. Do not substitute similar supported payers
for unsupported vision administrators such as EyeMed, VSP, or Premier Eye Care.

Generic `blue cross`, `bcbs`, `bcbs medicare hmo`, `preferred care`, `tricare`,
`united health one`, `staywell`, and the
combined Preferred Care Network/Partners label remain review despite scheduling
aliases. Bare Medicaid, out-of-state Blue Cross, and UHC Global also need the
actual card product. Carrier IDs and fuzzy payer search results are never inputs
to this crosswalk. Vision billing aliases are not payer aliases: `United Health
Care` stays on the medical UHC route and `Ambetter from Sunshine Health` stays
on Centene medical. Only explicit administrator names such as Davis Vision or
Spectera Vision select those vision payers. Generic Versant/MetLife and
health-plan-to-vision-administrator substitutions require the actual card.

## Product identity evidence beyond the directory

- [Aetna Better Health Florida](https://www.aetnabetterhealth.com/florida/providers/index.html)
  operates the local Medicaid and Florida Healthy Kids products (`128FL`).
- [HealthSpring's 2026 brand announcement](https://www.healthspring.com/newsroom/healthspring-plans-offer-customers-many-options-for-2026)
  identifies the former Cigna Medicare business; these MA products use the
  directory's HealthSpring route (`63092`), distinct from Cigna commercial (`62308`).
- [Simply's Medicare provider manual](https://provider.simplyhealthcareplans.com/docs/gpp/FLFL_SMH_MedicareAdvantageManual.pdf?v=202601271917)
  identifies the Medicare product as Simply Healthcare (`SMPLY`).
- [UHC's 2026 provider guide](https://www.uhcprovider.com/content/dam/provider/docs/public/admin-guides/2026-UHC-Administrative-Guide.pdf)
  identifies Individual Exchange under `87726`; Stedi independently confirms
  eligibility support for the UHC route. Claims IDs alone are not evidence of
  eligibility support.
- [Simply's Vivida acquisition notice](https://provider.simplyhealthcareplans.com/docs/gpp/FL_SHC_CHA_ProviderNews_Nov2022.pdf?v=202211011616)
  confirms Vivida stopped operating as an active Florida MMA plan in 2022.
  Its old name requires the current card rather than an assumed member transfer.

No paid eligibility checks were used to build or verify the crosswalk. Durable
post-booking dispatch, result retention, configured practice credentials, and
production activation retain the boundaries in [the integration contract](eligibility-local-experiment.md).
