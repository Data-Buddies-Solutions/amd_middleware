# Vision eligibility payer mapping

Verified 2026-09-23 against the [Stedi public payer directory CSV](https://payers.us.stedi.com/2024-04-01/public/payers/csv), comparing `DisplayName`, `Names`, `PrimaryPayerId`, `EligibilityInquiry`, `EligibilityInquiryEnrollmentRequired`, and `CoverageTypes` with `internal/eligibility/routes.go` and `internal/domain/insurance_data/INSURANCE_SPRING_HILL_ROUTINE_VISION.json`.

Stedi distinguishes transaction support from coverage types. A payer ID or a vision label alone does not make eligibility supported. [Stedi supported-payer documentation](https://www.stedi.com/docs/healthcare/supported-payers).

| Catalog plan | Primary payer ID | Stedi record | Eligibility | Vision listed | Action |
| --- | --- | --- | --- | --- | --- |
| Davis | 00157 | [Davis Vision](https://www.stedi.com/healthcare/network/QKZWY) | Yes | Yes | Keep route. |
| Spectera | 00773 | [Optum Health Vision](https://www.stedi.com/healthcare/network/ZNIZJ) | Yes | Yes | Keep route. |
| Envolve | 46278 | [Centene Dental and Vision Services](https://www.stedi.com/healthcare/network/CTWJH) | Yes | Yes | Keep route. |
| Guardian | 64246 | [Guardian](https://www.stedi.com/healthcare/network/EOFHN) | Yes | Yes | Keep route; not dental-only. |
| VSP | 94163 | [VSP Vision Service Plan](https://www.stedi.com/healthcare/network/GWRCD) | No | Yes | Keep explicit unsupported result. |
| EyeMed | 31165 | [EyeMed](https://www.stedi.com/healthcare/network/BHOZF) | No | Yes | Keep explicit unsupported result. |
| NVA | NVADM | [National Vision Administrators](https://www.stedi.com/healthcare/network/WFWIK) | No | Yes | Keep explicit unsupported result. |
| iCare | 26054 | [iCare Health Options TPA](https://www.stedi.com/healthcare/network/KMRPU) | No | Yes | Keep explicit unsupported result. |
| Premier | 65054 | [Premier Eye Care](https://www.stedi.com/healthcare/network/HEDBA) | No | Yes | Keep explicit unsupported result. |
| Solstice | 76578 | [Solstice](https://www.stedi.com/healthcare/network/NKTIX) | No | No | Keep blocked; directory does not establish a supported vision route. |
| Alivi | ALIVI | [Alivi Health](https://www.stedi.com/healthcare/network/UQFWA) | No | No | Keep blocked; directory does not establish a supported vision route. |
| Superior | 13305 | [Superior Vision](https://www.stedi.com/healthcare/network/IPUKT) | No | Yes | Explicit unsupported route; never substitute Davis. |
| Versant | Ambiguous | Davis and Superior both list Versant Health as an alternate name | Depends | Both | Require actual product; do not guess Davis. |
| Oscar | OSCAR | [Oscar Health](https://www.stedi.com/healthcare/network/TAZWT) | Yes | No | Do not treat medical eligibility as routine-vision proof; use confirmed Davis product or require product clarification. |

All four supported vision routes above have `EligibilityInquiryEnrollmentRequired=false` in the directory. This does not prove that a particular patient/provider request succeeds or establish participation. No live patient eligibility was submitted for this research.

The other directory entry named **iCare**, payer `11695`, is Independent Care Health Plan / Humana, with medical coverage only. It is not a replacement for iCare Health Options TPA `26054`. [Stedi iCare record](https://www.stedi.com/healthcare/network/YHUHF).

## Catalog versus eligibility routing

The routine-vision catalog has standalone Superior and Versant entries and also aliases them under Davis. These cannot be used as proof that they share a Stedi eligibility route: Superior is its own unsupported payer, while Versant is an umbrella alias on both Davis and Superior. Eligibility routing should preserve this distinction without changing office participation or AMD carrier mapping solely from directory research.

SunHealth and Self Pay are already excluded from eligibility dispatch by the current route table. Nothing in this research supports enabling those routes.

## Miriam Bach

The [CMS NPPES API record for NPI 1801200977](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1801200977), fetched directly on 2026-09-23, identifies **Miriam Bach, O.D.**, individual NPI, active status, primary taxonomy **152W00000X (Optometrist)**, Florida license **OPC4918**. The record was last updated 2022-11-10. Its primary location is Hollywood and its listed secondary practice locations do not include North Miami Beach Optical. Thus CMS verifies clinician identity, while the user and existing scheduling registry supply the office association; the registry does not establish payer enrollment or live eligibility success.
