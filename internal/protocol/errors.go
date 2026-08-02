package protocol

import "fmt"

// Category is a stable CLI error category from the chassiss.cli/v1 contract.
type Category string

const (
	CategoryUsage         Category = "usage"
	CategoryLocal         Category = "local"
	CategoryTrust         Category = "trust"
	CategoryProtocol      Category = "protocol"
	CategoryAuthorization Category = "authorization"
	CategoryValidation    Category = "validation"
	CategoryConflict      Category = "conflict"
	CategoryCheck         Category = "check"
	CategoryReview        Category = "review"
	CategoryNetwork       Category = "network"
	CategoryUnsupported   Category = "unsupported"
)

// Error is the internal representation of a stable public protocol error.
type Error struct {
	Code        string
	Message     string
	Category    Category
	Retryable   bool
	CurrentHead string
	OperationID string
	Details     map[string]any
	Remediation []Remediation
	Cause       error
}

// Remediation is deliberately argv-based. It is never a shell string.
type Remediation struct {
	Argv        []string `json:"argv"`
	Description string   `json:"description"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

// ExitCode maps the stable public category to the CLI process contract.
func (e *Error) ExitCode() int {
	switch e.Category {
	case CategoryUsage:
		return 2
	case CategoryLocal:
		return 3
	case CategoryTrust, CategoryProtocol:
		return 4
	case CategoryAuthorization:
		return 5
	case CategoryValidation, CategoryConflict:
		return 6
	case CategoryCheck, CategoryReview:
		return 7
	case CategoryNetwork:
		return 8
	default:
		return 9
	}
}

func NewError(code string, category Category, message string) *Error {
	return &Error{
		Code:     code,
		Category: category,
		Message:  message,
		Details:  map[string]any{},
	}
}

func WrapError(code string, category Category, message string, cause error) *Error {
	err := NewError(code, category, message)
	err.Cause = cause
	return err
}

const (
	ErrUsageInvalid             = "CHS_USAGE_INVALID"
	ErrProjectNotFound          = "CHS_PROJECT_NOT_FOUND"
	ErrProjectAlreadyRegistered = "CHS_PROJECT_ALREADY_REGISTERED"
	ErrWorktreeNotFound         = "CHS_WORKTREE_NOT_FOUND"
	ErrPathNotFound             = "CHS_PATH_NOT_FOUND"
	ErrWorktreeDirty            = "CHS_WORKTREE_DIRTY"
	ErrLocalStateCorrupt        = "CHS_LOCAL_STATE_CORRUPT"
	ErrPendingUnresolved        = "CHS_PENDING_OPERATION_UNRESOLVED"
	ErrSecretKeyNotFound        = "CHS_SECRET_KEY_NOT_FOUND"
	ErrIdentityAmbiguous        = "CHS_IDENTITY_AMBIGUOUS"
	ErrCacheBusy                = "CHS_CACHE_BUSY"

	ErrProtocolUnsupported     = "CHS_PROTOCOL_UNSUPPORTED"
	ErrSchemaInvalid           = "CHS_SCHEMA_INVALID"
	ErrUnknownCoreField        = "CHS_UNKNOWN_CORE_FIELD"
	ErrGenesisInvalid          = "CHS_GENESIS_INVALID"
	ErrProjectIDMismatch       = "CHS_PROJECT_ID_MISMATCH"
	ErrRootFingerprintMismatch = "CHS_ROOT_FINGERPRINT_MISMATCH"
	ErrMainlineNonLinear       = "CHS_MAINLINE_NON_LINEAR"
	ErrMainlineRollback        = "CHS_MAINLINE_ROLLBACK"
	ErrCommitSignatureInvalid  = "CHS_COMMIT_SIGNATURE_INVALID"
	ErrTrailerInvalid          = "CHS_TRAILER_INVALID"
	ErrOperationInvalid        = "CHS_OPERATION_INVALID"
	ErrOperationDigestMismatch = "CHS_OPERATION_DIGEST_MISMATCH"
	ErrEvidenceInvalid         = "CHS_EVIDENCE_INVALID"
	ErrEvidenceDigestMismatch  = "CHS_EVIDENCE_DIGEST_MISMATCH"
	ErrStateDigestMismatch     = "CHS_STATE_DIGEST_MISMATCH"
	ErrReducerMismatch         = "CHS_REDUCER_MISMATCH"
	ErrOperationIDCollision    = "CHS_OPERATION_ID_COLLISION"

	ErrGrantNotFound       = "CHS_GRANT_NOT_FOUND"
	ErrGrantRevoked        = "CHS_GRANT_REVOKED"
	ErrKeyMismatch         = "CHS_KEY_MISMATCH"
	ErrCapabilityDenied    = "CHS_CAPABILITY_DENIED"
	ErrTaskScopeDenied     = "CHS_TASK_SCOPE_DENIED"
	ErrResourceScopeDenied = "CHS_RESOURCE_SCOPE_DENIED"
	ErrLimitExceeded       = "CHS_LIMIT_EXCEEDED"

	ErrTaskbookStale              = "CHS_TASKBOOK_STALE"
	ErrArchitectureStale          = "CHS_ARCHITECTURE_STALE"
	ErrTaskbookInvalid            = "CHS_TASKBOOK_INVALID"
	ErrArchitectureInvalid        = "CHS_ARCHITECTURE_INVALID"
	ErrArchitectureNotEstablished = "CHS_ARCHITECTURE_NOT_ESTABLISHED"
	ErrTaskbookNotActive          = "CHS_TASKBOOK_NOT_ACTIVE"
	ErrTaskbookAlreadyActive      = "CHS_TASKBOOK_ALREADY_ACTIVE"
	ErrTaskbookNotComplete        = "CHS_TASKBOOK_NOT_COMPLETE"
	// ErrTaskbookNotQuiescent refuses a governance mutation that cannot preserve
	// an in-flight Task's frozen contract.
	ErrTaskbookNotQuiescent  = "CHS_TASKBOOK_NOT_QUIESCENT"
	ErrTaskbookClosureStale  = "CHS_TASKBOOK_CLOSURE_STALE"
	ErrReferenceNotFound     = "CHS_REFERENCE_NOT_FOUND"
	ErrGraphCycle            = "CHS_GRAPH_CYCLE"
	ErrPathScopeInvalid      = "CHS_PATH_SCOPE_INVALID"
	ErrPathEncodingInvalid   = "CHS_PATH_ENCODING_INVALID"
	ErrScopeViolation        = "CHS_SCOPE_VIOLATION"
	ErrProtectedPathChanged  = "CHS_PROTECTED_PATH_CHANGED"
	ErrDependencyUnsatisfied = "CHS_DEPENDENCY_UNSATISFIED"
	ErrTaskConflict          = "CHS_TASK_CONFLICT"
	ErrTaskPhaseInvalid      = "CHS_TASK_PHASE_INVALID"
	ErrTaskBlocked           = "CHS_TASK_BLOCKED"
	ErrTaskActorMismatch     = "CHS_TASK_ACTOR_MISMATCH"
	ErrReleaseHasChanges     = "CHS_RELEASE_HAS_CHANGES"

	ErrCheckFailed             = "CHS_CHECK_FAILED"
	ErrCheckError              = "CHS_CHECK_ERROR"
	ErrAttemptStale            = "CHS_ATTEMPT_STALE"
	ErrAttemptUnreachable      = "CHS_ATTEMPT_UNREACHABLE"
	ErrReviewReportInvalid     = "CHS_REVIEW_REPORT_INVALID"
	ErrReviewContextStale      = "CHS_REVIEW_CONTEXT_STALE"
	ErrReviewRequired          = "CHS_REVIEW_REQUIRED"
	ErrDriftRelevant           = "CHS_DRIFT_RELEVANT"
	ErrCandidateConflict       = "CHS_CANDIDATE_CONFLICT"
	ErrIntegrationTreeMismatch = "CHS_INTEGRATION_TREE_MISMATCH"

	ErrCASRetryExhausted       = "CHS_CAS_RETRY_EXHAUSTED"
	ErrRemoteUnreachable       = "CHS_REMOTE_UNREACHABLE"
	ErrPushResultUnknown       = "CHS_PUSH_RESULT_UNKNOWN"
	ErrRemoteIdentityMismatch  = "CHS_REMOTE_IDENTITY_MISMATCH"
	ErrRemoteAtomicUnsupported = "CHS_REMOTE_ATOMIC_UNSUPPORTED"
	ErrArchiveRefInvalid       = "CHS_ARCHIVE_REF_INVALID"
	ErrProposalStale           = "CHS_PROPOSAL_STALE"
	ErrDirectGitStateDetected  = "CHS_DIRECT_GIT_STATE_DETECTED"
	ErrOwnerWorkflowActive     = "CHS_OWNER_WORKFLOW_ACTIVE"
	ErrFeatureNotInV1          = "CHS_FEATURE_NOT_IN_V1"
)
