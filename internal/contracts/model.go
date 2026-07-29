package contracts

type Architecture struct {
	Schema       string              `json:"schema"`
	ID           string              `json:"id"`
	Overview     string              `json:"overview"`
	Principles   []string            `json:"principles"`
	Modules      map[string]Resource `json:"modules"`
	APIs         map[string]Resource `json:"apis"`
	Schemas      map[string]Resource `json:"schemas"`
	Dependencies map[string]Resource `json:"dependencies"`
	Configs      map[string]Resource `json:"configs"`
	Extensions   map[string]any      `json:"extensions,omitempty"`
}

type Resource struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Owner       string   `json:"owner,omitempty"`
	Paths       []string `json:"paths"`
	Requires    []string `json:"requires"`
}

type Taskbook struct {
	Schema       string                 `json:"schema"`
	ID           string                 `json:"id"`
	Workflow     Workflow               `json:"workflow"`
	Requirements map[string]Requirement `json:"requirements"`
	Constraints  map[string]Constraint  `json:"constraints"`
	Tasks        map[string]Task        `json:"tasks"`
	Extensions   map[string]any         `json:"extensions,omitempty"`
}

type Workflow struct {
	Title              string      `json:"title"`
	Outcome            string      `json:"outcome"`
	CompletionCriteria []string    `json:"completion_criteria"`
	Checks             []CheckSpec `json:"checks"`
}

type Requirement struct {
	Title      string   `json:"title"`
	Statement  string   `json:"statement"`
	Acceptance []string `json:"acceptance"`
}

type Constraint struct {
	Title string `json:"title"`
	Rule  string `json:"rule"`
}

type Task struct {
	Title             string        `json:"title"`
	Goal              string        `json:"goal"`
	Requirements      []string      `json:"requirements"`
	Constraints       []string      `json:"constraints"`
	Deliverables      []string      `json:"deliverables"`
	Modules           []string      `json:"modules"`
	DependsOn         []string      `json:"depends_on"`
	Writes            []string      `json:"writes"`
	Affects           []string      `json:"affects"`
	Checks            []CheckSpec   `json:"checks"`
	ChangeLimits      *ChangeLimits `json:"change_limits,omitempty"`
	OutOfScope        []string      `json:"out_of_scope"`
	StopConditions    []string      `json:"stop_conditions"`
	ReviewerAttention []string      `json:"reviewer_attention"`
	Supersedes        []string      `json:"supersedes"`
}

type CheckSpec struct {
	ID             string   `json:"id"`
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd"`
	TimeoutSeconds int64    `json:"timeout_seconds"`
}

type ChangeLimits struct {
	MaxChangedPaths int64 `json:"max_changed_paths"`
}
