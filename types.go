package toggleflow

import "encoding/json"

type FlagType string

const (
	FlagTypeBoolean FlagType = "boolean"
	FlagTypeString  FlagType = "string"
	FlagTypeNumber  FlagType = "number"
	FlagTypeJSON    FlagType = "json"
)

type Variation struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type Condition struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Values    []any  `json:"values"`
	Segment   string `json:"segment"`
}

type RolloutStep struct {
	Variation int `json:"variation"`
	Weight    int `json:"weight"`
}

type Rule struct {
	Conditions []Condition  `json:"conditions"`
	Serve      *int         `json:"serve"`
	Rollout    []RolloutStep `json:"rollout"`
}

type FlagConfig struct {
	Key              string      `json:"key"`
	FlagType         FlagType    `json:"flag_type"`
	Enabled          bool        `json:"enabled"`
	Variations       []Variation `json:"variations"`
	DefaultVariation int         `json:"default_variation"`
	Rules            []Rule      `json:"rules"`
}

// UserContext holds the attributes used for flag evaluation.
// Key is required — it is used for percentage rollout bucketing.
type UserContext struct {
	Key        string
	Attributes map[string]any
}

// Options configures the SDK client.
type Options struct {
	SDKKey       string
	BaseURL      string
	// PollInterval is how often to re-fetch flags. Default: 30s. Set to 0 to disable.
	PollInterval  int // seconds
}
