package repo

import (
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	apphttp "advancedmd-token-management/internal/http"

	"github.com/go-chi/chi/v5"
)

func TestReadmeRouteTableMatchesRouter(t *testing.T) {
	served := routerRoutes(t)
	documented := readmeRoutes(t)
	for _, route := range served {
		if !slices.Contains(documented, route) {
			t.Errorf("router serves %s but README.md does not list it; update README.md's HTTP interface table", route)
		}
	}
	for _, route := range documented {
		if !slices.Contains(served, route) {
			t.Errorf("README.md lists %s but the router does not serve it; update README.md's HTTP interface table", route)
		}
	}
}

func routerRoutes(t *testing.T) []string {
	router, ok := apphttp.NewRouter(&apphttp.Handlers{}, "", nil).(chi.Routes)
	if !ok {
		t.Fatal("apphttp.NewRouter must return a chi.Routes to walk")
	}
	var routes []string
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

func readmeRoutes(t *testing.T) []string {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(readme), "\n## HTTP interface\n")
	if !found {
		t.Fatal("README.md has no \"## HTTP interface\" section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	route := regexp.MustCompile("`([A-Z]+ /[^`?]*)[^`]*`")
	var routes []string
	for line := range strings.Lines(section) {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		for _, match := range route.FindAllStringSubmatch(cells[1], -1) {
			routes = append(routes, match[1])
		}
	}
	if len(routes) == 0 {
		t.Fatal("README.md's HTTP interface section has no route table rows")
	}
	return routes
}
