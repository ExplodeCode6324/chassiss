package app

import (
	"encoding/json"
	"time"
)

const (
	APIVersion        = "chassiss.dev/v1"
	ConfigVersion     = 2
	StateVersion      = 2
	TrustVersion      = 1
	CredentialVersion = 1
	EventVersion      = 2
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
	ID        string        `yaml:"id" json:"id"`
	Actor     string        `yaml:"actor" json:"actor"`
	Role      string        `yaml:"role" json:"role"`
	Actions   []string      `yaml:"actions" json:"actions"`
	PublicKey string        `yaml:"public_key" json:"public_key"`
	IssuedAt  time.Time     `yaml:"issued_at" json:"issued_at"`
	NotBefore *time.Time    `yaml:"not_before,omitempty" json:"not_before,omitempty"`
	ExpiresAt *time.Time    `yaml:"expires_at,omitempty" json:"expires_at,omitempty"`
	Resources ResourceScope `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type ResourceScope struct {
	Projects          []string `yaml:"projects,omitempty" json:"projects,omitempty"`
	Missions          []string `yaml:"missions,omitempty" json:"missions,omitempty"`
	Tasks             []string `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	Submissions       []string `yaml:"submissions,omitempty" json:"submissions,omitempty"`
	SubmissionDigests []string `yaml:"submission_digests,omitempty" json:"submission_digests,omitempty"`
	Heads             []string `yaml:"heads,omitempty" json:"heads,omitempty"`
	Baselines         []string `yaml:"baselines,omitempty" json:"baselines,omitempty"`
}

type Revocation struct {
	CredentialID string    `yaml:"credential_id" json:"credential_id"`
	RevokedAt    time.Time `yaml:"revoked_at" json:"revoked_at"`
	Reason       string    `yaml:"reason" json:"reason"`
}

type Trust struct {
	Version       int          `yaml:"version" json:"version"`
	Revision      int64        `yaml:"revision" json:"revision"`
	ProjectID     string       `yaml:"project_id" json:"project_id"`
	RootPublicKey string       `yaml:"root_public_key" json:"root_public_key"`
	Grants        []Grant      `yaml:"grants" json:"grants"`
	Revocations   []Revocation `yaml:"revocations" json:"revocations"`
	UpdatedAt     time.Time    `yaml:"updated_at" json:"updated_at"`
	Signature     string       `yaml:"signature" json:"signature"`
}

type Credential struct {
	Kind            string        `yaml:"kind" json:"kind"`
	Version         int           `yaml:"version" json:"version"`
	ID              string        `yaml:"id" json:"id"`
	ProjectID       string        `yaml:"project_id" json:"project_id"`
	RootFingerprint string        `yaml:"root_fingerprint" json:"root_fingerprint"`
	Actor           string        `yaml:"actor" json:"actor"`
	Role            string        `yaml:"role" json:"role"`
	Actions         []string      `yaml:"actions" json:"actions"`
	PrivateKey      string        `yaml:"private_key" json:"private_key"`
	IssuedAt        time.Time     `yaml:"issued_at" json:"issued_at"`
	NotBefore       *time.Time    `yaml:"not_before,omitempty" json:"not_before,omitempty"`
	ExpiresAt       *time.Time    `yaml:"expires_at,omitempty" json:"expires_at,omitempty"`
	Resources       ResourceScope `yaml:"resources,omitempty" json:"resources,omitempty"`
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
	ID             string            `yaml:"id" json:"id"`
	Argv           []string          `yaml:"argv" json:"argv"`
	Cwd            string            `yaml:"cwd" json:"cwd"`
	Env            map[string]string `yaml:"env" json:"env"`
	TimeoutSeconds int               `yaml:"timeout_seconds" json:"timeout_seconds"`
	Shell          bool              `yaml:"shell,omitempty" json:"shell,omitempty"`
}

type CheckResult struct {
	ID             string    `yaml:"id" json:"id"`
	SpecDigest     string    `yaml:"spec_digest" json:"spec_digest"`
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
	OwnerGrantID   string                 `yaml:"owner_grant_id,omitempty" json:"owner_grant_id,omitempty"`
	Branch         string                 `yaml:"branch,omitempty" json:"branch,omitempty"`
	Baseline       string                 `yaml:"baseline,omitempty" json:"baseline,omitempty"`
	WorktreePath   string                 `yaml:"worktree_path,omitempty" json:"worktree_path,omitempty"`
	WorktreeID     string                 `yaml:"worktree_id,omitempty" json:"worktree_id,omitempty"`
	WorktreeDigest string                 `yaml:"worktree_digest,omitempty" json:"worktree_digest,omitempty"`
	DependsOn      []string               `yaml:"depends_on" json:"depends_on"`
	AllowedPaths   []string               `yaml:"allowed_paths" json:"allowed_paths"`
	Checks         []CheckSpec            `yaml:"checks" json:"checks"`
	CheckResults   map[string]CheckResult `yaml:"check_results" json:"check_results"`
	Checkpoint     string                 `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty"`
	BlockReason    string                 `yaml:"block_reason,omitempty" json:"block_reason,omitempty"`
	PreviousStatus string                 `yaml:"previous_status,omitempty" json:"previous_status,omitempty"`
	SubmissionID   string                 `yaml:"submission_id,omitempty" json:"submission_id,omitempty"`
	ReplacementID  string                 `yaml:"replacement_id,omitempty" json:"replacement_id,omitempty"`
	SupersedesID   string                 `yaml:"supersedes_id,omitempty" json:"supersedes_id,omitempty"`
	ClosureReason  string                 `yaml:"closure_reason,omitempty" json:"closure_reason,omitempty"`
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
	ID             string                 `yaml:"id" json:"id"`
	SubmissionID   string                 `yaml:"submission_id" json:"submission_id"`
	SubmissionHead string                 `yaml:"submission_head" json:"submission_head"`
	PreviousHead   string                 `yaml:"previous_head" json:"previous_head"`
	IntegratedHead string                 `yaml:"integrated_head" json:"integrated_head"`
	IntegratedTree string                 `yaml:"integrated_tree" json:"integrated_tree"`
	Checks         map[string]CheckResult `yaml:"checks" json:"checks"`
	IntegratedBy   string                 `yaml:"integrated_by" json:"integrated_by"`
	CreatedAt      time.Time              `yaml:"created_at" json:"created_at"`
}

type Publication struct {
	ID                 string    `yaml:"id" json:"id"`
	Target             string    `yaml:"target" json:"target"`
	Remote             string    `yaml:"remote" json:"remote"`
	RemoteURLDigest    string    `yaml:"remote_url_digest" json:"remote_url_digest"`
	Branch             string    `yaml:"branch" json:"branch"`
	PreviousRemoteHead string    `yaml:"previous_remote_head,omitempty" json:"previous_remote_head,omitempty"`
	Head               string    `yaml:"head" json:"head"`
	PublishedBy        string    `yaml:"published_by" json:"published_by"`
	CreatedAt          time.Time `yaml:"created_at" json:"created_at"`
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
	Publications  map[string]Publication   `yaml:"publications" json:"publications"`
	UpdatedAt     time.Time                `yaml:"updated_at" json:"updated_at"`
	UpdatedBy     string                   `yaml:"updated_by" json:"updated_by"`
}

type Event struct {
	Version        int             `json:"version"`
	ProjectID      string          `json:"project_id"`
	Sequence       int64           `json:"sequence"`
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Actor          string          `json:"actor"`
	Role           string          `json:"role"`
	CredentialID   string          `json:"credential_id"`
	Resource       string          `json:"resource,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	PreviousDigest string          `json:"previous_digest,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Digest         string          `json:"digest"`
	Signature      string          `json:"signature"`
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
	Resources  ResourceScope
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
