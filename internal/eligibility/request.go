package eligibility

type Encounter struct {
	ServiceTypeCodes []string `json:"serviceTypeCodes"`
}

type Request struct {
	Payer      string    `json:"tradingPartnerServiceId"`
	Provider   Provider  `json:"provider"`
	Subscriber Person    `json:"subscriber"`
	Encounter  Encounter `json:"encounter"`
}
