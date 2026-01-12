package prompts

import (
	"fmt"
	"strings"
)

type Validator func(Input) error

func RequireNonEmpty(field string, get func(Input) string) Validator {
	return func(in Input) error {
		if get == nil {
			return fmt.Errorf("validator for %s: getter is nil", field)
		}
		if strings.TrimSpace(get(in)) == "" {
			return fmt.Errorf("%s required", field)
		}
		return nil
	}
}

func RequireAnyNonEmpty(msg string, getter ...func(Input) string) Validator {
	return func(in Input) error {
		for _, g := range getter {
			if g == nil {
				continue
			}
			if strings.TrimSpace(g(in)) != "" {
				return nil
			}
		}
		return fmt.Errorf("%s", msg)
	}
}
