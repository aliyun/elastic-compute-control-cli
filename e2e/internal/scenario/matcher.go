package scenario

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Matcher is the assertion vocabulary for one jsonpath. A bare scalar is the
// `eq` shorthand; a mapping may combine several matchers (AND).
type Matcher struct {
	Eq    any
	HasEq bool
	Ne    any
	HasNe bool

	Type        string
	Prefix      string
	HasPrefix   bool
	Suffix      string
	HasSuffix   bool
	Contains    string
	HasContains bool
	Regex       string

	OneOf    []any
	HasOneOf bool

	Gt, Ge, Lt, Le      *float64
	Len, MinLen, MaxLen *int
	Exists              *bool
	Each                *Matcher
}

// UnmarshalYAML accepts a scalar (eq shorthand) or a mapping of matchers.
func (m *Matcher) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var v any
		if err := n.Decode(&v); err != nil {
			return err
		}
		m.Eq, m.HasEq = v, true
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("matcher must be a scalar or a mapping")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		val := n.Content[i+1]
		switch key {
		case "eq":
			if err := val.Decode(&m.Eq); err != nil {
				return err
			}
			m.HasEq = true
		case "ne":
			if err := val.Decode(&m.Ne); err != nil {
				return err
			}
			m.HasNe = true
		case "type":
			m.Type = val.Value
		case "prefix":
			m.Prefix, m.HasPrefix = val.Value, true
		case "suffix":
			m.Suffix, m.HasSuffix = val.Value, true
		case "contains":
			m.Contains, m.HasContains = val.Value, true
		case "regex":
			m.Regex = val.Value
		case "oneof":
			if err := val.Decode(&m.OneOf); err != nil {
				return err
			}
			m.HasOneOf = true
		case "gt":
			m.Gt = new(float64)
			if err := val.Decode(m.Gt); err != nil {
				return err
			}
		case "ge":
			m.Ge = new(float64)
			if err := val.Decode(m.Ge); err != nil {
				return err
			}
		case "lt":
			m.Lt = new(float64)
			if err := val.Decode(m.Lt); err != nil {
				return err
			}
		case "le":
			m.Le = new(float64)
			if err := val.Decode(m.Le); err != nil {
				return err
			}
		case "len":
			m.Len = new(int)
			if err := val.Decode(m.Len); err != nil {
				return err
			}
		case "min_len":
			m.MinLen = new(int)
			if err := val.Decode(m.MinLen); err != nil {
				return err
			}
		case "max_len":
			m.MaxLen = new(int)
			if err := val.Decode(m.MaxLen); err != nil {
				return err
			}
		case "exists":
			m.Exists = new(bool)
			if err := val.Decode(m.Exists); err != nil {
				return err
			}
		case "each":
			var em Matcher
			if err := val.Decode(&em); err != nil {
				return err
			}
			m.Each = &em
		default:
			return fmt.Errorf("unknown matcher %q", key)
		}
	}
	return nil
}

// Validate checks matcher fields offline (regex compiles, type is known).
func (m *Matcher) Validate() error {
	if m.Regex != "" {
		if _, err := regexp.Compile(m.Regex); err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
	}
	switch m.Type {
	case "", "string", "integer", "number", "bool", "array", "object", "null":
	default:
		return fmt.Errorf("unknown type %q", m.Type)
	}
	if m.Each != nil {
		return m.Each.Validate()
	}
	return nil
}
