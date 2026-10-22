package domain

import "errors"

type RoutingRule string

const (
	RoutingNotAccepted RoutingRule = "not_accepted"
	RoutingBachOnly    RoutingRule = "bach_only"
	RoutingBachLicht   RoutingRule = "bach_licht"
	RoutingAll         RoutingRule = "all_three"
	RoutingOpticalOnly RoutingRule = "optical_only"
)

var errUnknownRouting = errors.New("routing must be one of not_accepted, bach_only, bach_licht, all_three, optical_only")

func ParseRoutingRule(s string) (RoutingRule, error) {
	switch rule := RoutingRule(s); rule {
	case RoutingNotAccepted, RoutingBachOnly, RoutingBachLicht, RoutingAll, RoutingOpticalOnly:
		return rule, nil
	default:
		return "", errUnknownRouting
	}
}
