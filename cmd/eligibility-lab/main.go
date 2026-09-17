// eligibility-lab reads private local cases and prints aggregate counts only.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"advancedmd-token-management/internal/eligibility"
)

type failure string

func (f failure) Error() string { return string(f) }
func category(err error) string {
	var f failure
	if errors.As(err, &f) {
		return string(f)
	}
	return "local_io_failure"
}

func save(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(b, '\n'))
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}

func execute() error {
	input := flag.String("input", "", "private JSON array of cases")
	output := flag.String("output", "", "new private output file")
	flag.Parse()
	if *input == "" || *output == "" {
		return failure("input_output_required")
	}
	data, err := os.ReadFile(*input)
	if err != nil {
		return failure("input_unreadable")
	}
	var cases []eligibility.Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return failure("invalid_case_json")
	}
	results := []eligibility.Assessment{}
	counts := map[string]int{}
	for _, c := range cases {
		a, err := eligibility.Assess(c)
		if err != nil {
			return failure("invalid_saved_response")
		}
		results = append(results, a)
		counts[a.Match.Status]++
	}
	if err := save(*output, results); err != nil {
		return failure("output_file_not_created")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"cases": len(cases), "identityStatuses": counts})
}

func main() {
	if err := execute(); err != nil {
		// Internal errors can contain paths or response data. Keep output generic;
		// exact requests and responses belong exclusively in the private journal.
		fmt.Fprintf(os.Stderr, "Eligibility lab stopped (%s); inspect private evidence. No automatic replay.\n", category(err))
		os.Exit(1)
	}
}
