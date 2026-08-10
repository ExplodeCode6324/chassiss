package state

type State struct {
	Audit     *AuditIndex          `json:"audit,omitempty"`
	Authority Authority            `json:"authority"`
	Project   Project              `json:"project"`
	Protocol  string               `json:"protocol"`
	Schema    string               `json:"schema"`
	Tasks     map[string]TaskState `json:"tasks"`
}

type AuditIndex struct {
	Failures []AttemptFailureIndex `json:"failures,omitempty"`
	Reviews  []ReviewIndex         `json:"reviews,omitempty"`
}

type ReviewIndex struct {
	AttemptDigest  string `json:"attempt_digest"`
	ContextDigest  string `json:"context_digest"`
	GrantID        string `json:"grant_id"`
	KeyFingerprint string `json:"key_fingerprint"`
	KeyID          string `json:"key_id"`
	OperationID    string `json:"operation_id"`
	ReportDigest   string `json:"report_digest"`
	Reviewer       string `json:"reviewer"`
	Task           string `json:"task"`
	Taskbook       string `json:"taskbook"`
	Verdict        string `json:"verdict"`
}

type AttemptFailureIndex struct {
	Actor              string `json:"actor"`
	AgentGrantID       string `json:"agent_grant_id"`
	AgentKeyID         string `json:"agent_key_id"`
	ChangedPathsDigest string `json:"changed_paths_digest"`
	Code               string `json:"code"`
	OperationID        string `json:"operation_id"`
	Phase              string `json:"phase"`
	Summary            string `json:"summary"`
	Task               string `json:"task"`
	Taskbook           string `json:"taskbook"`
	WorkHead           string `json:"work_head"`
	WorkTree           string `json:"work_tree"`
}

type Project struct {
	Architecture *BlobRef     `json:"architecture"`
	ID           string       `json:"id"`
	Source       *SourceRef   `json:"source,omitempty"`
	Taskbook     *TaskbookRef `json:"taskbook"`
}

type BlobRef struct {
	BlobOID string `json:"blob_oid"`
	Path    string `json:"path"`
}

type SourceRef struct {
	Commit       string `json:"commit"`
	HistoryBlob  string `json:"history_blob"`
	HistoryPath  string `json:"history_path"`
	ObjectFormat string `json:"object_format"`
	Tree         string `json:"tree"`
}

type TaskbookRef struct {
	BlobOID string `json:"blob_oid"`
	ID      string `json:"id"`
	Path    string `json:"path"`
}

type Authority struct {
	Grants map[string]Grant `json:"grants"`
	Root   Root             `json:"root"`
}

type Root struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type Grant struct {
	Actor        string   `json:"actor"`
	Capabilities []string `json:"capabilities"`
	KeyID        string   `json:"key_id"`
	Limits       Limits   `json:"limits"`
	PublicKey    string   `json:"public_key"`
	Scope        Scope    `json:"scope"`
}

type Limits struct {
	MaxActiveTasks  *int64 `json:"max_active_tasks,omitempty"`
	MaxChangedPaths *int64 `json:"max_changed_paths,omitempty"`
	Mode            string `json:"mode"`
}

type Scope struct {
	Resources []string `json:"resources"`
	Tasks     []string `json:"tasks"`
}

type TaskState struct {
	Actor    string    `json:"actor,omitempty"`
	Attempt  *Attempt  `json:"attempt,omitempty"`
	Base     string    `json:"base,omitempty"`
	Blocked  *bool     `json:"blocked,omitempty"`
	Contract *Contract `json:"contract,omitempty"`
	Phase    string    `json:"phase"`
	Review   *Review   `json:"review,omitempty"`
}

type Contract struct {
	ArchitectureBlob string `json:"architecture_blob"`
	TaskbookBlob     string `json:"taskbook_blob"`
}

type Attempt struct {
	EvidenceDigest          string `json:"evidence_digest"`
	Head                    string `json:"head"`
	SubmitterKeyFingerprint string `json:"submitter_key_fingerprint"`
}

type Review struct {
	CandidateTree  string `json:"candidate_tree"`
	ContextDigest  string `json:"context_digest"`
	KeyFingerprint string `json:"key_fingerprint"`
	KeyID          string `json:"key_id"`
	ReportDigest   string `json:"report_digest"`
	ReviewMain     string `json:"review_main"`
	Reviewer       string `json:"reviewer"`
}
