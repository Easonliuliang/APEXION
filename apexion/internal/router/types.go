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

// CostClass describes relative execution and dependency cost.
type CostClass string

const (
	CostLow    CostClass = "low"
	CostMedium CostClass = "medium"
	CostHigh   CostClass = "high"
)

// RoutingStrategy selects which routing pipeline to execute.
type RoutingStrategy string

const (
	RoutingLegacy       RoutingStrategy = "legacy"
	RoutingHybrid       RoutingStrategy = "hybrid"
	RoutingCapabilityV2 RoutingStrategy = "capability_v2"
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
	// Strategy controls the routing algorithm.
	// Empty defaults to legacy behavior.
	Strategy RoutingStrategy
	// ShadowEval emits shadow planning details when strategy supports it.
	ShadowEval bool
	// ShadowSampleRate controls probabilistic sampling for shadow plans.
	// <= 0 disables shadow generation, >= 1 always generates.
	ShadowSampleRate float64
	// DeterministicFastpath enables deterministic pre-model tool execution hints.
	DeterministicFastpath bool
	// FastpathConfidence is the minimum confidence to emit a fastpath plan.
	FastpathConfidence float64
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
	Shadow   *ShadowPlan    `json:"shadow,omitempty"`
	FastPath *FastPathPlan  `json:"fastpath,omitempty"`
}

// ShadowPlan records a non-blocking shadow route for comparison.
type ShadowPlan struct {
	Strategy RoutingStrategy `json:"strategy"`
	Primary  []PlannedTool   `json:"primary"`
	Fallback []string        `json:"fallback,omitempty"`
	Filtered []FilteredTool  `json:"filtered,omitempty"`
}

// FastPathPlan describes a deterministic direct tool call suggestion.
type FastPathPlan struct {
	Tool       string  `json:"tool"`
	Task       string  `json:"task"`
	InputJSON  string  `json:"input_json"`
	Confidence float64 `json:"confidence"`
}

// ToolCapability is declarative metadata for capability-aware routing.
type ToolCapability struct {
	Name                string
	Domains             []Intent
	SemanticLevel       SemanticLevel
	Risk                RiskClass
	Cost                CostClass
	LatencyHintMs       int
	SupportsParallel    bool
	Requires            []string
	DeterministicFor    []string
	ProviderConstraints []string
	DegradePolicy       []string
}
