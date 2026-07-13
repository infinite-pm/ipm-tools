package testconfig

import (
	"encoding/json"
	"os"
)

// Params contains parameters for the processing method
type Params struct {
	ParserName      string                 `json:"parser_name,omitempty"`
	ParserParams    map[string]interface{} `json:"parser_params,omitempty"`
	TransformName   string                 `json:"transform_name,omitempty"`   // Alias for ParserName
	TransformParams map[string]interface{} `json:"transform_params,omitempty"` // Alias for ParserParams
}

// GetName returns the parser/transform name (supports both field names)
func (p *Params) GetName() string {
	if p.ParserName != "" {
		return p.ParserName
	}
	return p.TransformName
}

// GetParams returns the parser/transform params (supports both field names)
func (p *Params) GetParams() map[string]interface{} {
	if p.ParserParams != nil {
		return p.ParserParams
	}
	return p.TransformParams
}

// SourceConfig represents a test source configuration entry from sources.json
type SourceConfig struct {
	Name        string   `json:"name"`
	Destination string   `json:"destination"`
	SourceFiles []string `json:"source_files"`
	SourcesGlob []string `json:"sources_glob"`
	Method      string   `json:"method"`
	Params      *Params  `json:"params,omitempty"`
}

// Load reads and parses a sources.json file
func Load(path string) ([]SourceConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg []SourceConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// FilterByParserName returns configs matching the specified parser name
func FilterByParserName(configs []SourceConfig, parserName string) []SourceConfig {
	var filtered []SourceConfig
	for _, cfg := range configs {
		if cfg.Params != nil && cfg.Params.GetName() == parserName {
			filtered = append(filtered, cfg)
		}
	}
	return filtered
}

// FilterByItemType returns configs whose parser produces items of the specified type
// For example, itemType "ipmt" matches parsers "ipmt-md" and "ipmt-file"
func FilterByItemType(configs []SourceConfig, itemType string) []SourceConfig {
	var filtered []SourceConfig
	for _, cfg := range configs {
		if cfg.Params != nil && cfg.Params.GetName() != "" {
			// Determine item type from parser name
			var cfgItemType string
			switch cfg.Params.GetName() {
			case "ipmt-md", "ipmt-file":
				cfgItemType = "ipmt"
			case "ipmt-invalid-md", "ipmt-invalid-file":
				cfgItemType = "ipmt-invalid"
			case "ipmdev-layout-rule-md":
				cfgItemType = "ipmdev-layout-rule"
			}
			if cfgItemType == itemType {
				filtered = append(filtered, cfg)
			}
		}
	}
	return filtered
}

// GetUniqueDestinations returns unique destination paths from configs
func GetUniqueDestinations(configs []SourceConfig) []string {
	seen := make(map[string]bool)
	var dests []string
	for _, cfg := range configs {
		if !seen[cfg.Destination] {
			seen[cfg.Destination] = true
			dests = append(dests, cfg.Destination)
		}
	}
	return dests
}
