package router

// Intent is the inferred user goal category for tool routing.
type Intent string

const (
	IntentCodebase Intent = "codebase"
	IntentDebug    Intent = "debug"
	IntentResearch Intent = "research"
	IntentGit      Intent = "git"
	IntentVision   Intent = "vision"
	IntentSystem   Intent = "system"
)

// SemanticLevel describes how goal-oriented a tool is.
type SemanticLevel string

const (
	SemanticHigh      SemanticLevel = "high"
	SemanticMedium    SemanticLevel = "medium"
	SemanticPrimitive SemanticLevel = "primitive"
)

// RiskClass captures operational risk for a tool.
type RiskClass string

const (
	RiskRead    RiskClass = "read"
	RiskWrite   RiskClass = "write"
	RiskExecute RiskClass = "execute"
	RiskNetwork RiskClass = "network"
)

// ToolProfile is routing metadata for a tool.
type ToolProfile struct {
	Domain        Intent
	SemanticLevel SemanticLevel
	Risk          RiskClass
}

// CandidateTool is a tool candidate available for a given turn.
type CandidateTool struct {
	Name        string
	Description string
	ReadOnly    bool
}

// PlanInput is the routing context for a turn.
type PlanInput struct {
	UserText            string
	HasImage            bool
	ModelImageSupported bool
	Provider            string
	Model               string
	Tools               []CandidateTool
}

// PlanOptions controls route planning behavior.
type PlanOptions struct {
	// MaxCandidates limits primary tools exposed to model.
	// 0 means no cap.
	MaxCandidates int
}

// PlannedTool is a ranked tool recommendation.
type PlannedTool struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// FilteredTool is a tool removed by hard-gating rules.
type FilteredTool struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RoutePlan is the routing result for a turn.
type RoutePlan struct {
	Intent   Intent         `json:"intent"`
	Primary  []PlannedTool  `json:"primary"`
	Fallback []string       `json:"fallback,omitempty"`
	Filtered []FilteredTool `json:"filtered,omitempty"`
}
