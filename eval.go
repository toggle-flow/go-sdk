package toggleflow

import (
	"fmt"
	"strconv"
	"strings"
)

// evaluate returns the variation index to serve for the given flag and user.
// Port of backend/internal/eval/engine.go.
func evaluate(flag FlagConfig, user UserContext, segments map[string][]any) int {
	if !flag.Enabled {
		return flag.DefaultVariation
	}
	if len(flag.Rules) == 0 {
		return flag.DefaultVariation
	}

	for _, rule := range flag.Rules {
		if matchesAll(rule.Conditions, user, segments) {
			return serve(rule, flag.Key, user.Key, len(flag.Variations), flag.DefaultVariation)
		}
	}

	return flag.DefaultVariation
}

func serve(rule Rule, flagKey, userKey string, numVariations, defaultVariation int) int {
	if rule.Serve != nil {
		idx := *rule.Serve
		if idx >= 0 && idx < numVariations {
			return idx
		}
		return defaultVariation
	}
	if len(rule.Rollout) > 0 {
		return rollout(rule.Rollout, flagKey, userKey, numVariations, defaultVariation)
	}
	return defaultVariation
}

func rollout(steps []RolloutStep, flagKey, userKey string, numVariations, defaultVariation int) int {
	b := bucket(flagKey, userKey)
	cumulative := 0
	for _, step := range steps {
		cumulative += step.Weight
		if b < cumulative {
			if step.Variation >= 0 && step.Variation < numVariations {
				return step.Variation
			}
			return defaultVariation
		}
	}
	return defaultVariation
}

func matchesAll(conditions []Condition, user UserContext, segments map[string][]any) bool {
	for _, c := range conditions {
		if !matchCondition(c, user, segments) {
			return false
		}
	}
	return true
}

func matchCondition(c Condition, user UserContext, segments map[string][]any) bool {
	raw, ok := user.Attributes[c.Attribute]
	if !ok {
		return false
	}
	attr := fmt.Sprintf("%v", raw)

	values := c.Values
	if c.Segment != "" && (c.Operator == "in" || c.Operator == "notIn") {
		if sv, exists := segments[c.Segment]; exists {
			values = sv
		} else {
			values = nil
		}
	}

	switch c.Operator {
	case "equals":
		return len(values) > 0 && attr == fmt.Sprintf("%v", values[0])
	case "in":
		for _, v := range values {
			if attr == fmt.Sprintf("%v", v) {
				return true
			}
		}
		return false
	case "notIn":
		for _, v := range values {
			if attr == fmt.Sprintf("%v", v) {
				return false
			}
		}
		return true
	case "contains":
		return len(values) > 0 && strings.Contains(attr, fmt.Sprintf("%v", values[0]))
	case "startsWith":
		return len(values) > 0 && strings.HasPrefix(attr, fmt.Sprintf("%v", values[0]))
	case "endsWith":
		return len(values) > 0 && strings.HasSuffix(attr, fmt.Sprintf("%v", values[0]))
	case "gt", "gte", "lt", "lte":
		return compareNumeric(c.Operator, attr, values)
	}
	return false
}

func compareNumeric(op, attr string, values []any) bool {
	if len(values) == 0 {
		return false
	}
	a, errA := strconv.ParseFloat(attr, 64)
	b, errB := strconv.ParseFloat(fmt.Sprintf("%v", values[0]), 64)
	if errA != nil || errB != nil {
		return false
	}
	switch op {
	case "gt":
		return a > b
	case "gte":
		return a >= b
	case "lt":
		return a < b
	case "lte":
		return a <= b
	}
	return false
}
