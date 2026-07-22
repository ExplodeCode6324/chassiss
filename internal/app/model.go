package app

import "time"

const (
	APIVersion        = "chassiss.dev/v1"
	StateVersion      = 1
	CredentialVersion = 1
	EventVersion      = 1
)

type Config struct {
	Version         int       `yaml:"version" json:"version"`
	ProjectID       string    `yaml:"project_id" json:"project_id"`
	Mode            string    `yaml:"mode" json:"mode"`
	DefaultBranch   string    `yaml:"default_branch" json:"default_branch"`
	ContentBackend  string    `yaml:"content_backend" json:"content_backend"`
	WIPLimit        int       `yaml:"wip_limit" json:"wip_limit"`
	RootFingerprint string    `yaml:"root_fingerprint" json:"root_fingerprint"`
	CreatedAt       time.Time `yaml:"created_at" json:"created_at"`
}

type RootKey struct {
	Kind       string    `yaml:"kind" json:"kind"`
	Version    int       `yaml:"version" json:"version"`
	ID         string    `yaml:"id" json:"id"`
	PublicKey  string    `yaml:"public_key" json:"public_key"`
	PrivateKey string    `yaml:"private_key" json:"private_key"`
	CreatedAt  time.Time `yaml:"created_at" json:"created_at"`
}

type Grant struct {
	ID        string    `yaml:"id" json:"id"`
	Actor     string    `yaml:"actor" json:"actor"`
	Role      string    `yaml:"role" json:"role"`
	Actions   []string  `yaml:"actions" json:"actions"`
	PublicKey string    `yaml:"public_key" json:"public_key"`
	IssuedAt  time.Time `yaml:"issued_at" json:"issued_at"`
}

type Revocation struct {
	CredentialID string    `yaml:"credential_id" json:"credential_id"`
	RevokedAt    time.Time `yaml:"revoked_at" json:"revoked_at"`
	Reason       string    `yaml:"reason" json:"reason"`
}

type Trust struct {
	Version       int          `yaml:"version" json:"version"`
	ProjectID     string       `yaml:"project_id" json:"project_id"`
	RootPublicKey string       `yaml:"root_public_key" json:"root_public_key"`
	Grants        []Grant      `yaml:"grants" json:"grants"`
	Revocations   []Revocation `yaml:"revocations" json:"revocations"`
	UpdatedAt     time.Time    `yaml:"updated_at" json:"updated_at"`
	Signature     string       `yaml:"signature" json:"signature"`
}

type Credential struct {
	Kind            string    `yaml:"kind" json:"kind"`
	Version         int       `yaml:"version" json:"version"`
	ID              string    `yaml:"id" json:"id"`
	ProjectID       string    `yaml:"project_id" json:"project_id"`
	RootFingerprint string    `yaml:"root_fingerprint" json:"root_fingerprint"`
	Actor           string    `yaml:"actor" json:"actor"`
	Role            string    `yaml:"role" json:"role"`
	Actions         []string  `yaml:"actions" json:"actions"`
	PrivateKey      string    `yaml:"private_key" json:"private_key"`
	IssuedAt        time.Time `yaml:"issued_at" json:"issued_at"`
}

type ArtifactState struct {
	ID              string    `yaml:"id" json:"id"`
	Kind            string    `yaml:"kind" json:"kind"`
	Path            string    `yaml:"path" json:"path"`
	Digest          string    `yaml:"digest" json:"digest"`
	Status          string    `yaml:"status" json:"status"`
	SubmissionID    string    `yaml:"submission_id" json:"submission_id"`
	SubmittedBy     string    `yaml:"submitted_by" json:"submitted_by"`
	AcceptedBy      string    `yaml:"accepted_by,omitempty" json:"accepted_by,omitempty"`
	AcceptedCommit  string    `yaml:"accepted_commit,omitempty" json:"accepted_commit,omitempty"`
	RejectedBy      string    `yaml:"rejected_by,omitempty" json:"rejected_by,omitempty"`
	RejectionReason string    `yaml:"rejection_reason,omitempty" json:"rejection_reason,omitempty"`
	UpdatedAt       time.Time `yaml:"updated_at" json:"updated_at"`
}

type CheckSpec struct {
	ID      string `yaml:"id" json:"id"`
	Command string `yaml:"command" json:"command"`
}

type CheckResult struct {
	ID             string    `yaml:"id" json:"id"`
	Command        string    `yaml:"command" json:"command"`
	ExitCode       int       `yaml:"exit_code" json:"exit_code"`
	Passed         bool      `yaml:"passed" json:"passed"`
	Output         string    `yaml:"output" json:"output"`
	SnapshotDigest string    `yaml:"snapshot_digest,omitempty" json:"snapshot_digest,omitempty"`
	CheckedAt      time.Time `yaml:"checked_at" json:"checked_at"`
}

type MissionState struct {
	ID                 string    `yaml:"id" json:"id"`
	ArtifactID         string    `yaml:"artifact_id" json:"artifact_id"`
	Status             string    `yaml:"status" json:"status"`
	TaskIDs            []string  `yaml:"task_ids" json:"task_ids"`
	AcceptanceEvidence string    `yaml:"acceptance_evidence,omitempty" json:"acceptance_evidence,omitempty"`
	BlockReason        string    `yaml:"block_reason,omitempty" json:"block_reason,omitempty"`
	PreviousStatus     string    `yaml:"previous_status,omitempty" json:"previous_status,omitempty"`
	UpdatedAt          time.Time `yaml:"updated_at" json:"updated_at"`
}

type TaskState struct {
	ID             string                 `yaml:"id" json:"id"`
	MissionID      string                 `yaml:"mission_id" json:"mission_id"`
	ArtifactID     string                 `yaml:"artifact_id" json:"artifact_id"`
	Status         string                 `yaml:"status" json:"status"`
	Owner          string                 `yaml:"owner,omitempty" json:"owner,omitempty"`
	Branch         string                 `yaml:"branch,omitempty" json:"branch,omitempty"`
	Baseline       string                 `yaml:"baseline,omitempty" json:"baseline,omitempty"`
	DependsOn      []string               `yaml:"depends_on" json:"depends_on"`
	AllowedPaths   []string               `yaml:"allowed_paths" json:"allowed_paths"`
	Checks         []CheckSpec            `yaml:"checks" json:"checks"`
	CheckResults   map[string]CheckResult `yaml:"check_results" json:"check_results"`
	Checkpoint     string                 `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty"`
	BlockReason    string                 `yaml:"block_reason,omitempty" json:"block_reason,omitempty"`
	PreviousStatus string                 `yaml:"previous_status,omitempty" json:"previous_status,omitempty"`
	SubmissionID   string                 `yaml:"submission_id,omitempty" json:"submission_id,omitempty"`
	UpdatedAt      time.Time              `yaml:"updated_at" json:"updated_at"`
}

type Submission struct {
	ID            string                 `yaml:"id" json:"id"`
	TaskID        string                 `yaml:"task_id" json:"task_id"`
	Actor         string                 `yaml:"actor" json:"actor"`
	BaseCommit    string                 `yaml:"base_commit" json:"base_commit"`
	HeadCommit    string                 `yaml:"head_commit" json:"head_commit"`
	ChangedFiles  []string               `yaml:"changed_files" json:"changed_files"`
	Checks        map[string]CheckResult `yaml:"checks" json:"checks"`
	Handoff       string                 `yaml:"handoff" json:"handoff"`
	Digest        string                 `yaml:"digest" json:"digest"`
	Status        string                 `yaml:"status" json:"status"`
	ReviewID      string                 `yaml:"review_id,omitempty" json:"review_id,omitempty"`
	IntegrationID string                 `yaml:"integration_id,omitempty" json:"integration_id,omitempty"`
	CreatedAt     time.Time              `yaml:"created_at" json:"created_at"`
}

type Review struct {
	ID               string    `yaml:"id" json:"id"`
	SubmissionID     string    `yaml:"submission_id" json:"submission_id"`
	SubmissionDigest string    `yaml:"submission_digest" json:"submission_digest"`
	Reviewer         string    `yaml:"reviewer" json:"reviewer"`
	Verdict          string    `yaml:"verdict" json:"verdict"`
	Report           string    `yaml:"report" json:"report"`
	CreatedAt        time.Time `yaml:"created_at" json:"created_at"`
}

type Integration struct {
	ID             string    `yaml:"id" json:"id"`
	SubmissionID   string    `yaml:"submission_id" json:"submission_id"`
	PreviousHead   string    `yaml:"previous_head" json:"previous_head"`
	IntegratedHead string    `yaml:"integrated_head" json:"integrated_head"`
	IntegratedBy   string    `yaml:"integrated_by" json:"integrated_by"`
	CreatedAt      time.Time `yaml:"created_at" json:"created_at"`
}

type State struct {
	Version       int                      `yaml:"version" json:"version"`
	ProjectID     string                   `yaml:"project_id" json:"project_id"`
	Revision      int64                    `yaml:"revision" json:"revision"`
	Phase         string                   `yaml:"phase" json:"phase"`
	Baseline      string                   `yaml:"baseline,omitempty" json:"baseline,omitempty"`
	ActiveMission string                   `yaml:"active_mission,omitempty" json:"active_mission,omitempty"`
	Artifacts     map[string]ArtifactState `yaml:"artifacts" json:"artifacts"`
	Missions      map[string]MissionState  `yaml:"missions" json:"missions"`
	Tasks         map[string]TaskState     `yaml:"tasks" json:"tasks"`
	Submissions   map[string]Submission    `yaml:"submissions" json:"submissions"`
	Reviews       map[string]Review        `yaml:"reviews" json:"reviews"`
	Integrations  map[string]Integration   `yaml:"integrations" json:"integrations"`
	UpdatedAt     time.Time                `yaml:"updated_at" json:"updated_at"`
	UpdatedBy     string                   `yaml:"updated_by" json:"updated_by"`
}

type Event struct {
	Version        int       `json:"version"`
	ProjectID      string    `json:"project_id"`
	Sequence       int64     `json:"sequence"`
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Actor          string    `json:"actor"`
	Role           string    `json:"role"`
	CredentialID   string    `json:"credential_id"`
	Resource       string    `json:"resource,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	State          State     `json:"state"`
	Digest         string    `json:"digest"`
	Signature      string    `json:"signature"`
}

type ArtifactMetadata struct {
	Kind               string      `yaml:"kind"`
	ID                 string      `yaml:"id"`
	RequirementsDigest string      `yaml:"requirements_digest,omitempty"`
	ArchitectureDigest string      `yaml:"architecture_digest,omitempty"`
	MissionID          string      `yaml:"mission_id,omitempty"`
	TaskIDs            []string    `yaml:"task_ids,omitempty"`
	DependsOn          []string    `yaml:"depends_on,omitempty"`
	AllowedPaths       []string    `yaml:"allowed_paths,omitempty"`
	AcceptanceChecks   []CheckSpec `yaml:"acceptance_checks,omitempty"`
}

type ArtifactDocument struct {
	Metadata ArtifactMetadata
	Body     string
	Raw      []byte
	Path     string
	Digest   string
}

type Principal struct {
	ID         string
	Actor      string
	Role       string
	Actions    map[string]struct{}
	PrivateKey []byte
	PublicKey  []byte
}

type Response struct {
	APIVersion     string         `json:"api_version"`
	OK             bool           `json:"ok"`
	Command        string         `json:"command"`
	ProjectID      string         `json:"project_id,omitempty"`
	RevisionBefore int64          `json:"revision_before,omitempty"`
	RevisionAfter  int64          `json:"revision_after,omitempty"`
	Result         any            `json:"result,omitempty"`
	Next           []string       `json:"allowed_next_actions,omitempty"`
	Error          *ResponseError `json:"error,omitempty"`
}

type ResponseError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Retryable   bool     `json:"retryable"`
	Remediation []string `json:"remediation,omitempty"`
}

type CLIError struct {
	Code      string
	Message   string
	ExitCode  int
	Retryable bool
	Remedy    []string
}

func (e *CLIError) Error() string { return e.Message }
