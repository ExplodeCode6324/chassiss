package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/contracts"
	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

const (
	SubmissionEvidenceSchema = "chassiss.submission-evidence/v1"
	AttemptSchema            = "chassiss.attempt/v1"
	CheckContextSchema       = "chassiss.check-execution-context/v1"
	ReviewContextSchema      = "chassiss.review-context/v1"
	ReviewReportSchema       = "chassiss.review-report/v1"
	ClosureReportSchema      = "chassiss.taskbook-closure-report/v1"
	DriftSchema              = "chassiss.drift-classification/v1"
)

type CheckExecutionContext struct {
	ArchitectureBlob string  `json:"architecture_blob"`
	CheckID          string  `json:"check_id"`
	CheckSpecDigest  string  `json:"check_spec_digest"`
	Head             string  `json:"head"`
	Phase            string  `json:"phase"`
	Schema           string  `json:"schema"`
	Task             *string `json:"task"`
	TaskbookBlob     string  `json:"taskbook_blob"`
	Tree             string  `json:"tree"`
}

type CheckResult struct {
	CheckID       string `json:"check_id"`
	ContextDigest string `json:"context_digest"`
	ExitCode      *int64 `json:"exit_code"`
	Status        string `json:"status"`
}

type CheckBinding struct {
	ArchitectureBlob string
	Head             string
	Phase            string
	Task             *string
	TaskbookBlob     string
	Tree             string
}

func BuildCheckContext(spec contracts.CheckSpec, binding CheckBinding) (CheckExecutionContext, error) {
	specDigest, err := protocol.ObjectDigest("check-spec", spec)
	if err != nil {
		return CheckExecutionContext{}, err
	}
	context := CheckExecutionContext{
		ArchitectureBlob: binding.ArchitectureBlob,
		CheckID:          spec.ID, CheckSpecDigest: specDigest, Head: binding.Head,
		Phase: binding.Phase, Schema: CheckContextSchema, Task: binding.Task,
		TaskbookBlob: binding.TaskbookBlob, Tree: binding.Tree,
	}
	if err := context.Validate(); err != nil {
		return CheckExecutionContext{}, err
	}
	return context, nil
}

func (context CheckExecutionContext) Validate() error {
	if context.Schema != CheckContextSchema {
		return fmt.Errorf("Check Context schema must be %s", CheckContextSchema)
	}
	switch context.Phase {
	case "submission", "review", "integration":
		if context.Task == nil || *context.Task == "" {
			return fmt.Errorf("%s Check Context requires a Task", context.Phase)
		}
	case "workflow-closure":
		if context.Task != nil {
			return fmt.Errorf("workflow-closure Check Context task must be null")
		}
	default:
		return fmt.Errorf("unknown Check Context phase %q", context.Phase)
	}
	for _, digest := range []string{context.CheckSpecDigest} {
		if err := protocol.ValidateDigest(digest); err != nil {
			return err
		}
	}
	for name, oid := range map[string]string{
		"architecture_blob": context.ArchitectureBlob,
		"head":              context.Head, "taskbook_blob": context.TaskbookBlob, "tree": context.Tree,
	} {
		if err := validateAnyOID(oid); err != nil {
			return fmt.Errorf("Check Context %s: %w", name, err)
		}
	}
	return nil
}

func ValidateCheckResults(specs []contracts.CheckSpec, results []CheckResult, binding CheckBinding, requirePass bool) error {
	if len(specs) != len(results) {
		return fmt.Errorf("expected %d Check Results, got %d", len(specs), len(results))
	}
	for index, spec := range specs {
		result := results[index]
		if result.CheckID != spec.ID {
			return fmt.Errorf("Check Result %d must bind %s", index, spec.ID)
		}
		context, err := BuildCheckContext(spec, binding)
		if err != nil {
			return err
		}
		digest, err := protocol.ObjectDigest("check-execution-context", context)
		if err != nil {
			return err
		}
		if result.ContextDigest != digest {
			return fmt.Errorf("Check Result %s Context digest mismatch", spec.ID)
		}
		switch result.Status {
		case "pass":
			if result.ExitCode == nil || *result.ExitCode != 0 {
				return fmt.Errorf("pass Check Result requires exit_code=0")
			}
		case "fail":
			if result.ExitCode == nil || *result.ExitCode == 0 ||
				*result.ExitCode < -2147483648 || *result.ExitCode > 2147483647 {
				return fmt.Errorf("fail Check Result requires a non-zero signed 32-bit exit code")
			}
		case "error":
			if result.ExitCode != nil {
				return fmt.Errorf("error Check Result requires exit_code=null")
			}
		default:
			return fmt.Errorf("unknown Check Result status %q", result.Status)
		}
		if requirePass && result.Status != "pass" {
			return fmt.Errorf("Check %s did not pass", spec.ID)
		}
	}
	return nil
}

type SubmissionEvidence struct {
	ArchitectureBlob   string        `json:"architecture_blob"`
	Base               string        `json:"base"`
	ChangedPathsCount  int64         `json:"changed_paths_count"`
	ChangedPathsDigest string        `json:"changed_paths_digest"`
	CheckResults       []CheckResult `json:"check_results"`
	Head               string        `json:"head"`
	Schema             string        `json:"schema"`
	Submitter          Submitter     `json:"submitter"`
	Task               string        `json:"task"`
	TaskbookBlob       string        `json:"taskbook_blob"`
	Tree               string        `json:"tree"`
}

type Submitter struct {
	Actor          string `json:"actor"`
	KeyFingerprint string `json:"key_fingerprint"`
}

func (evidence SubmissionEvidence) Validate(specs []contracts.CheckSpec, changedPaths []string, objectFormat string) error {
	if evidence.Schema != SubmissionEvidenceSchema {
		return fmt.Errorf("Submission Evidence schema must be %s", SubmissionEvidenceSchema)
	}
	if evidence.ChangedPathsCount != int64(len(changedPaths)) {
		return fmt.Errorf("Submission changed path count mismatch")
	}
	digest, err := protocol.ObjectDigest("changed-paths", changedPaths)
	if err != nil {
		return err
	}
	if evidence.ChangedPathsDigest != digest {
		return fmt.Errorf("Submission changed path digest mismatch")
	}
	if err := protocol.ValidateActor(evidence.Submitter.Actor); err != nil {
		return err
	}
	if !strings.HasPrefix(evidence.Submitter.KeyFingerprint, "SHA256:") {
		return fmt.Errorf("Submission key fingerprint must use OpenSSH SHA256 form")
	}
	for name, oid := range map[string]string{
		"architecture_blob": evidence.ArchitectureBlob, "base": evidence.Base,
		"head": evidence.Head, "taskbook_blob": evidence.TaskbookBlob, "tree": evidence.Tree,
	} {
		if err := protocol.ValidateOID(oid, objectFormat); err != nil {
			return fmt.Errorf("Submission Evidence %s: %w", name, err)
		}
	}
	task := evidence.Task
	return ValidateCheckResults(specs, evidence.CheckResults, CheckBinding{
		ArchitectureBlob: evidence.ArchitectureBlob, Head: evidence.Head,
		Phase: "submission", Task: &task, TaskbookBlob: evidence.TaskbookBlob,
		Tree: evidence.Tree,
	}, true)
}

type ReviewContext struct {
	ArtifactTree     string        `json:"artifact_tree"`
	ArchitectureBlob string        `json:"architecture_blob"`
	AttemptHead      string        `json:"attempt_head"`
	CandidateTree    string        `json:"candidate_tree"`
	CheckResults     []CheckResult `json:"check_results"`
	RequiresClosure  []string      `json:"requires_closure"`
	ReviewMain       string        `json:"review_main"`
	Schema           string        `json:"schema"`
	Task             string        `json:"task"`
	TaskBase         string        `json:"task_base"`
	TaskbookBlob     string        `json:"taskbook_blob"`
}

func (context ReviewContext) Validate(specs []contracts.CheckSpec, objectFormat string, requirePass bool) error {
	if context.Schema != ReviewContextSchema {
		return fmt.Errorf("Review Context schema must be %s", ReviewContextSchema)
	}
	if !sort.StringsAreSorted(context.RequiresClosure) || duplicate(context.RequiresClosure) {
		return fmt.Errorf("Review Context requires_closure must be sorted and unique")
	}
	for name, oid := range map[string]string{
		"artifact_tree": context.ArtifactTree, "architecture_blob": context.ArchitectureBlob,
		"attempt_head": context.AttemptHead, "candidate_tree": context.CandidateTree,
		"review_main": context.ReviewMain, "task_base": context.TaskBase,
		"taskbook_blob": context.TaskbookBlob,
	} {
		if err := protocol.ValidateOID(oid, objectFormat); err != nil {
			return fmt.Errorf("Review Context %s: %w", name, err)
		}
	}
	task := context.Task
	return ValidateCheckResults(specs, context.CheckResults, CheckBinding{
		ArchitectureBlob: context.ArchitectureBlob, Head: context.ReviewMain,
		Phase: "review", Task: &task, TaskbookBlob: context.TaskbookBlob,
		Tree: context.CandidateTree,
	}, requirePass)
}

type ReviewReport struct {
	Findings                   []Finding           `json:"findings"`
	Results                    ReviewResults       `json:"results"`
	ReviewerAttentionResponses []AttentionResponse `json:"reviewer_attention_responses"`
	Schema                     string              `json:"schema"`
	Summary                    string              `json:"summary"`
	Verdict                    string              `json:"verdict"`
}

type ReviewResults struct {
	Architecture string `json:"architecture"`
	Contract     string `json:"contract"`
	Integration  string `json:"integration"`
	Requirements string `json:"requirements"`
}

type Finding struct {
	Category  string   `json:"category"`
	Paths     []string `json:"paths"`
	Resources []string `json:"resources"`
	Severity  string   `json:"severity"`
	Summary   string   `json:"summary"`
}

type AttentionResponse struct {
	Attention string `json:"attention"`
	Response  string `json:"response"`
}

func NewReviewReportTemplate(attention []string) ReviewReport {
	responses := make([]AttentionResponse, len(attention))
	for index, value := range attention {
		responses[index] = AttentionResponse{Attention: value, Response: ""}
	}
	return ReviewReport{
		Findings: []Finding{}, Results: ReviewResults{},
		ReviewerAttentionResponses: responses, Schema: ReviewReportSchema,
		Summary: "", Verdict: "",
	}
}

func (report ReviewReport) Validate(attention []string, resources map[string]contracts.Resource) error {
	if report.Schema != ReviewReportSchema ||
		(report.Verdict != "approve" && report.Verdict != "request_changes") ||
		strings.TrimSpace(report.Summary) == "" {
		return fmt.Errorf("Review Report schema, verdict, or summary is invalid")
	}
	canonical, err := protocol.CanonicalJSON(report)
	if err != nil {
		return err
	}
	if len(canonical) > 65536 {
		return fmt.Errorf("Review Report exceeds 65,536 canonical bytes")
	}
	if len(report.ReviewerAttentionResponses) != len(attention) {
		return fmt.Errorf("Review Report does not cover every reviewer_attention entry")
	}
	for index, expected := range attention {
		response := report.ReviewerAttentionResponses[index]
		if response.Attention != expected || strings.TrimSpace(response.Response) == "" {
			return fmt.Errorf("reviewer_attention response %d is missing or out of order", index)
		}
	}
	blocking := false
	for index, finding := range report.Findings {
		if finding.Severity != "blocking" && finding.Severity != "advisory" {
			return fmt.Errorf("Finding %d has invalid severity", index)
		}
		switch finding.Category {
		case "requirements", "contract", "architecture", "integration", "check":
		default:
			return fmt.Errorf("Finding %d has invalid category", index)
		}
		if strings.TrimSpace(finding.Summary) == "" ||
			!sort.StringsAreSorted(finding.Paths) || duplicate(finding.Paths) ||
			!sort.StringsAreSorted(finding.Resources) || duplicate(finding.Resources) {
			return fmt.Errorf("Finding %d has invalid summary or non-canonical sets", index)
		}
		for _, path := range finding.Paths {
			if err := contracts.ValidateRepoPath(path); err != nil {
				return fmt.Errorf("Finding %d path: %w", index, err)
			}
		}
		for _, resource := range finding.Resources {
			if _, exists := resources[resource]; !exists {
				return fmt.Errorf("Finding %d references missing Resource %s", index, resource)
			}
		}
		blocking = blocking || finding.Severity == "blocking"
	}
	if report.Verdict == "approve" {
		if report.Results.Requirements != "pass" || report.Results.Contract != "pass" ||
			report.Results.Architecture != "conformant" || report.Results.Integration != "pass" ||
			blocking {
			return fmt.Errorf("approve Report requires all results to pass and no blocking Finding")
		}
	} else {
		if report.Results.Requirements != "pass" && report.Results.Requirements != "fail" {
			return fmt.Errorf("invalid requirements result")
		}
		if report.Results.Contract != "pass" && report.Results.Contract != "fail" {
			return fmt.Errorf("invalid contract result")
		}
		if report.Results.Architecture != "conformant" && report.Results.Architecture != "nonconformant" {
			return fmt.Errorf("invalid architecture result")
		}
		if report.Results.Integration != "pass" && report.Results.Integration != "fail" {
			return fmt.Errorf("invalid integration result")
		}
	}
	return nil
}

type ClosureReport struct {
	CompletionCriteriaResponses []CriterionResponse `json:"completion_criteria_responses"`
	Findings                    []ClosureFinding    `json:"findings"`
	Schema                      string              `json:"schema"`
	Summary                     string              `json:"summary"`
	TaskResponses               []TaskResponse      `json:"task_responses"`
	Taskbook                    string              `json:"taskbook"`
}

type CriterionResponse struct {
	Criterion string `json:"criterion"`
	Response  string `json:"response"`
	Status    string `json:"status"`
}

type ClosureFinding struct {
	Paths     []string `json:"paths"`
	Resources []string `json:"resources"`
	Severity  string   `json:"severity"`
	Summary   string   `json:"summary"`
}

type TaskResponse struct {
	Phase    string `json:"phase"`
	Response string `json:"response"`
	Task     string `json:"task"`
}

func NewClosureReportTemplate(taskbook *contracts.Taskbook, phases map[string]string) ClosureReport {
	taskIDs := make([]string, 0, len(phases))
	for id := range phases {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	taskResponses := make([]TaskResponse, len(taskIDs))
	for index, id := range taskIDs {
		taskResponses[index] = TaskResponse{Task: id, Phase: phases[id], Response: ""}
	}
	criteria := make([]CriterionResponse, len(taskbook.Workflow.CompletionCriteria))
	for index, criterion := range taskbook.Workflow.CompletionCriteria {
		criteria[index] = CriterionResponse{
			Criterion: criterion, Response: "", Status: "",
		}
	}
	return ClosureReport{
		CompletionCriteriaResponses: criteria, Findings: []ClosureFinding{},
		Schema: ClosureReportSchema, Summary: "", TaskResponses: taskResponses,
		Taskbook: taskbook.ID,
	}
}

type DriftClassification struct {
	AffectedResources  []string      `json:"affected_resources"`
	ChangedPathsDigest string        `json:"changed_paths_digest"`
	Classification     string        `json:"classification"`
	CommitRangeDigest  string        `json:"commit_range_digest"`
	IntegrationParent  string        `json:"integration_parent"`
	Reasons            []DriftReason `json:"reasons"`
	ReviewMain         string        `json:"review_main"`
	Schema             string        `json:"schema"`
	Task               string        `json:"task"`
}

type DriftReason struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (classification DriftClassification) Validate() error {
	if classification.Schema != DriftSchema ||
		(classification.Classification != "unrelated" && classification.Classification != "relevant") {
		return fmt.Errorf("Drift Classification header is invalid")
	}
	if !sort.StringsAreSorted(classification.AffectedResources) ||
		duplicate(classification.AffectedResources) {
		return fmt.Errorf("affected_resources must be sorted and unique")
	}
	for index, reason := range classification.Reasons {
		switch reason.Kind {
		case "path", "resource", "contract", "check", "candidate-conflict", "unknown":
		default:
			return fmt.Errorf("Drift reason %d has invalid kind", index)
		}
		if strings.TrimSpace(reason.Value) == "" {
			return fmt.Errorf("Drift reason %d has an empty value", index)
		}
	}
	for _, digest := range []string{classification.ChangedPathsDigest, classification.CommitRangeDigest} {
		if err := protocol.ValidateDigest(digest); err != nil {
			return err
		}
	}
	return nil
}

func (report ClosureReport) Validate(taskbook *contracts.Taskbook, phases map[string]string, resources map[string]contracts.Resource) error {
	if report.Schema != ClosureReportSchema || report.Taskbook != taskbook.ID ||
		strings.TrimSpace(report.Summary) == "" {
		return fmt.Errorf("Closure Report header is invalid")
	}
	canonical, err := protocol.CanonicalJSON(report)
	if err != nil {
		return err
	}
	if len(canonical) > 65536 {
		return fmt.Errorf("Closure Report exceeds 65,536 canonical bytes")
	}
	taskIDs := make([]string, 0, len(phases))
	for id := range phases {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	if len(report.TaskResponses) != len(taskIDs) {
		return fmt.Errorf("Closure Report must cover every Task")
	}
	for index, id := range taskIDs {
		response := report.TaskResponses[index]
		if response.Task != id || response.Phase != phases[id] ||
			strings.TrimSpace(response.Response) == "" {
			return fmt.Errorf("Closure Task response %d is missing, out of order, or stale", index)
		}
		switch response.Phase {
		case "closed", "cancelled", "superseded":
		default:
			return fmt.Errorf("Closure Report contains a non-terminal phase")
		}
	}
	if len(report.CompletionCriteriaResponses) != len(taskbook.Workflow.CompletionCriteria) {
		return fmt.Errorf("Closure Report must cover every completion criterion")
	}
	for index, criterion := range taskbook.Workflow.CompletionCriteria {
		response := report.CompletionCriteriaResponses[index]
		if response.Criterion != criterion || response.Status != "pass" ||
			strings.TrimSpace(response.Response) == "" {
			return fmt.Errorf("completion criterion %d is not accepted in exact order", index)
		}
	}
	for index, finding := range report.Findings {
		if finding.Severity != "advisory" {
			return fmt.Errorf("Closure Finding %d must be advisory to archive", index)
		}
		if strings.TrimSpace(finding.Summary) == "" ||
			!sort.StringsAreSorted(finding.Paths) || duplicate(finding.Paths) ||
			!sort.StringsAreSorted(finding.Resources) || duplicate(finding.Resources) {
			return fmt.Errorf("Closure Finding %d is invalid", index)
		}
		for _, resource := range finding.Resources {
			if _, exists := resources[resource]; !exists {
				return fmt.Errorf("Closure Finding %d references missing Resource %s", index, resource)
			}
		}
		for _, path := range finding.Paths {
			if err := contracts.ValidateRepoPath(path); err != nil {
				return fmt.Errorf("Closure Finding %d path: %w", index, err)
			}
		}
	}
	return nil
}

func DecodeClosed(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func duplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}

func validateAnyOID(value string) error {
	if len(value) == 40 {
		return protocol.ValidateOID(value, "sha1")
	}
	if len(value) == 64 {
		return protocol.ValidateOID(value, "sha256")
	}
	return fmt.Errorf("Git OID must have 40 or 64 lowercase hexadecimal characters")
}
