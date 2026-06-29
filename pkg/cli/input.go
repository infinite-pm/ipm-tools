package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// InputType represents the detected input format.
type InputType int

const (
	InputTypeUnknown InputType = iota
	InputTypeIPMT
	InputTypeIPMGraph
	InputTypeLayoutGraph
	// InputTypeDebugJSON is a recorded run (.debug.json) — a dev-tool
	// input handled by LoadReport, not DetectInput (the shipping tools
	// never see it).
	InputTypeDebugJSON
)

// DetectedInput contains the parsed input and its detected type.
type DetectedInput struct {
	Type        InputType
	IPMGraph    *model.IpmGraph
	LayoutGraph *layout.Graph
}

// DetectInput attempts to detect and parse the input data.
// Returns the parsed data and its detected type.
// Tries detection in this order:
// 1. File extension (.ipmt, .layout.json, .json)
// 2. Content structure (layout.Graph has Meta.Bounds, model.IpmGraph has Nodes array)
// 3. IPMT text parsing as fallback
func DetectInput(data []byte, filename string) (*DetectedInput, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	// Try extension-based detection first
	ext := strings.ToLower(filepath.Ext(filename))
	basename := strings.ToLower(filepath.Base(filename))

	// .ipmt files - parse as IPMT text
	if ext == ".ipmt" {
		doc, err := parser.Parse(data, parser.Options{})
		if err != nil {
			return nil, fmt.Errorf("parse IPMT: %w", err)
		}
		return &DetectedInput{
			Type:     InputTypeIPMT,
			IPMGraph: doc,
		}, nil
	}

	// .layout.json files - parse as layout.Graph
	if strings.HasSuffix(basename, ".layout.json") {
		var graph layout.Graph
		if err := json.Unmarshal(data, &graph); err != nil {
			return nil, fmt.Errorf("parse layout JSON: %w", err)
		}
		if graph.Meta.Bounds.Width <= 0 {
			return nil, fmt.Errorf("invalid layout graph: missing bounds")
		}
		return &DetectedInput{
			Type:        InputTypeLayoutGraph,
			LayoutGraph: &graph,
		}, nil
	}

	// .json files - try content-based detection
	if ext == ".json" {
		// Try layout.Graph first (more specific structure)
		var graph layout.Graph
		if err := json.Unmarshal(data, &graph); err == nil && graph.Meta.Bounds.Width > 0 {
			return &DetectedInput{
				Type:        InputTypeLayoutGraph,
				LayoutGraph: &graph,
			}, nil
		}

		// Try model.IpmGraph
		var doc model.IpmGraph
		if err := json.Unmarshal(data, &doc); err == nil && len(doc.Nodes) > 0 {
			return &DetectedInput{
				Type:     InputTypeIPMGraph,
				IPMGraph: &doc,
			}, nil
		}

		return nil, fmt.Errorf("unrecognized JSON structure (expected layout.Graph or model.IpmGraph)")
	}

	// No extension or stdin - try content-based detection
	// First try JSON parsing
	var graph layout.Graph
	if err := json.Unmarshal(data, &graph); err == nil && graph.Meta.Bounds.Width > 0 {
		return &DetectedInput{
			Type:        InputTypeLayoutGraph,
			LayoutGraph: &graph,
		}, nil
	}

	var doc model.IpmGraph
	if err := json.Unmarshal(data, &doc); err == nil && len(doc.Nodes) > 0 {
		return &DetectedInput{
			Type:     InputTypeIPMGraph,
			IPMGraph: &doc,
		}, nil
	}

	// Fallback: try parsing as IPMT text
	doc2, err := parser.Parse(data, parser.Options{})
	if err == nil {
		return &DetectedInput{
			Type:     InputTypeIPMT,
			IPMGraph: doc2,
		}, nil
	}

	return nil, fmt.Errorf("unable to detect input format (tried layout JSON, IPM JSON, and IPMT text)")
}
