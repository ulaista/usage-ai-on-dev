package domain

import "time"

type Intent struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Goal         string    `json:"goal"`
	Status       string    `json:"status"`
	Constraints  []string  `json:"constraints,omitempty"`
	Affected     []string  `json:"affected,omitempty"`
	Verification []string  `json:"verification,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Batch struct {
	ID        string    `json:"id"`
	IntentID  string    `json:"intent_id"`
	Title     string    `json:"title"`
	Scope     []string  `json:"scope,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Execution struct {
	ID            string    `json:"id"`
	Task          string    `json:"task"`
	TaskType      string    `json:"task_type"`
	Model         string    `json:"model"`
	Route         string    `json:"route"`
	LatencyMillis int64     `json:"latency_ms"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	Accepted      *bool     `json:"accepted,omitempty"`
	Fallback      bool      `json:"fallback"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type TelemetrySummary struct {
	Samples          int     `json:"samples"`
	Reviewed         int     `json:"reviewed"`
	AcceptanceRate   float64 `json:"acceptance_rate"`
	AverageLatencyMS float64 `json:"avg_latency_ms"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	Fallbacks        int     `json:"fallbacks"`
	Errors           int     `json:"errors"`
}

type WorkloadStats struct {
	TaskType          string  `json:"task_type"`
	Model             string  `json:"model,omitempty"`
	Samples           int     `json:"samples"`
	Reviewed          int     `json:"reviewed"`
	Accepted          int     `json:"accepted"`
	AcceptanceRate    float64 `json:"acceptance_rate"`
	Fallbacks         int     `json:"fallbacks"`
	FallbackRate      float64 `json:"fallback_rate"`
	Errors            int     `json:"errors"`
	ErrorRate         float64 `json:"error_rate"`
	AverageLatencyMS  float64 `json:"avg_latency_ms"`
	AverageInputTokens float64 `json:"avg_input_tokens"`
	AverageOutputTokens float64 `json:"avg_output_tokens"`
}

type CacheSaving struct {
	Kind              string    `json:"kind"`
	Key               string    `json:"key"`
	SavedInputTokens  int       `json:"saved_input_tokens"`
	SavedOutputTokens int       `json:"saved_output_tokens"`
	CreatedAt         time.Time `json:"created_at"`
}

type EconomySummary struct {
	Hits              int   `json:"hits"`
	ContextHits       int   `json:"context_hits"`
	ExecutionHits     int   `json:"execution_hits"`
	AutotuneHits      int   `json:"autotune_hits"`
	SingleflightJoins int   `json:"singleflight_joins"`
	DeltaHits         int   `json:"delta_hits"`
	VerificationHits  int   `json:"verification_capsules"`
	SemanticHits      int   `json:"semantic_hits"`
	SavedInputTokens  int64 `json:"saved_input_tokens"`
	SavedOutputTokens int64 `json:"saved_output_tokens"`
	SavedTotalTokens  int64 `json:"saved_total_tokens"`
}

type SymbolQuery struct {
	Pattern      string `json:"pattern"`
	RelativePath string `json:"relative_path,omitempty"`
	IncludeBody  bool   `json:"include_body,omitempty"`
	Depth        int    `json:"depth,omitempty"`
}

type SemanticResult struct {
	Provider    string `json:"provider"`
	Raw         string `json:"raw"`
	CacheHit    bool   `json:"cache_hit,omitempty"`
	CacheKey    string `json:"cache_key,omitempty"`
	RepoStateID string `json:"repo_state_id,omitempty"`
}
