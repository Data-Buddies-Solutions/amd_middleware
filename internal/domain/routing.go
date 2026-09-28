package domain

type RoutingRule string

const (
	RoutingNotAccepted RoutingRule = "not_accepted"
	RoutingBachOnly    RoutingRule = "bach_only"
	RoutingBachLicht   RoutingRule = "bach_licht"
	RoutingAll         RoutingRule = "all_three"
	RoutingOpticalOnly RoutingRule = "optical_only"
)

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
