package domain

import "testing"

func TestResolveCarrierIDUsesTheSelectedEnvironmentsCarriers(t *testing.T) {
	cases := []struct {
		env     Environment
		carrier string
		want    string
	}{
		{Prod, "car40887", "car40887"},
		{Prod, "car40897", "car40897"},
		{Sandbox, "car40887", "car7551"},
		{Sandbox, "car40897", "car7077"},
		{Sandbox, "car40895", "car40895"},
	}
	t.Cleanup(func() { InitRegistry(Prod) })
	for _, c := range cases {
		InitRegistry(c.env)
		if got := ResolveCarrierID(c.carrier); got != c.want {
			t.Errorf("%s %s = %s, want %s", c.env, c.carrier, got, c.want)
		}
	}
}
