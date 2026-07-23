package app

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.md
var templateFS embed.FS

var idPatterns = map[string]*regexp.Regexp{
	"mission": regexp.MustCompile(`^M[0-9]{3}$`),
	"task":    regexp.MustCompile(`^M[0-9]{3}-T[0-9]{3}$`),
}

func templateKinds() []string {
	return []string{"requirements", "architecture", "mission", "task"}
}

func renderTemplate(root, kind, id string) ([]byte, string, error) {
	if !containsString(templateKinds(), kind) {
		return nil, "", &CLIError{Code: "CHS-TEMPLATE-KIND", Message: "unknown template kind: " + kind, ExitCode: 20}
	}
	if (kind == "mission" || kind == "task") && !idPatterns[kind].MatchString(id) {
		return nil, "", &CLIError{Code: "CHS-TEMPLATE-ID", Message: "invalid " + kind + " ID", ExitCode: 20}
	}
	data, err := templateFS.ReadFile("templates/" + kind + ".md")
	if err != nil {
		return nil, "", err
	}
	text := string(data)
	path := ""
	switch kind {
	case "requirements":
		path = "docs/requirements.md"
	case "architecture":
		path = "docs/architecture.md"
		if root != "" {
			_, _, state, err := loadProject(root)
			if err == nil {
				if artifact, ok := state.Artifacts["requirements"]; ok && artifact.Status == "accepted" {
					text = strings.ReplaceAll(text, "REPLACE_REQUIREMENTS_DIGEST", artifact.Digest)
				}
			}
		}
	case "mission":
		path = "docs/missions/" + id + ".md"
		text = strings.ReplaceAll(text, "M000", id)
		text = replaceAcceptedDesignDigests(root, text)
	case "task":
		path = "docs/tasks/" + id + ".md"
		missionID := strings.Split(id, "-")[0]
		text = strings.ReplaceAll(text, "M000-T000", id)
		text = strings.ReplaceAll(text, "M000", missionID)
		text = replaceAcceptedDesignDigests(root, text)
		budget := newProjectDefaultTaskBudget
		if root != "" {
			if config, _, _, err := loadProject(root); err == nil {
				budget = config.DefaultTaskBudget
			}
		}
		text = strings.ReplaceAll(text, "REPLACE_MAX_CHANGED_FILES", strconv.Itoa(budget.MaxChangedFiles))
		text = strings.ReplaceAll(text, "REPLACE_MAX_DIFF_LINES", strconv.Itoa(budget.MaxDiffLines))
		text = strings.ReplaceAll(text, "REPLACE_MAX_COMMITS", strconv.Itoa(budget.MaxCommits))
	}
	return []byte(text), path, nil
}

func replaceAcceptedDesignDigests(root, text string) string {
	if root == "" {
		return text
	}
	_, _, state, err := loadProject(root)
	if err != nil {
		return text
	}
	if artifact, ok := state.Artifacts["requirements"]; ok && artifact.Status == "accepted" {
		text = strings.ReplaceAll(text, "REPLACE_REQUIREMENTS_DIGEST", artifact.Digest)
	}
	if artifact, ok := state.Artifacts["architecture"]; ok && artifact.Status == "accepted" {
		text = strings.ReplaceAll(text, "REPLACE_ARCHITECTURE_DIGEST", artifact.Digest)
	}
	return text
}

func parseArtifact(root, path string) (*ArtifactDocument, error) {
	absolute, err := pathWithin(root, path)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return nil, &CLIError{Code: "CHS-ARTIFACT-FRONTMATTER", Message: "artifact must start with YAML front matter", ExitCode: 10}
	}
	rest := text[4:]
	separator := strings.Index(rest, "\n---\n")
	if separator < 0 {
		return nil, &CLIError{Code: "CHS-ARTIFACT-FRONTMATTER", Message: "artifact front matter is not closed", ExitCode: 10}
	}
	metadataText := rest[:separator]
	body := rest[separator+5:]
	var metadata ArtifactMetadata
	decoder := yaml.NewDecoder(strings.NewReader(metadataText))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, &CLIError{Code: "CHS-ARTIFACT-FRONTMATTER", Message: "invalid artifact metadata: " + err.Error(), ExitCode: 10}
	}
	relative, _ := filepath.Rel(root, absolute)
	document := &ArtifactDocument{Metadata: metadata, Body: body, Raw: raw, Path: filepath.ToSlash(relative), Digest: digestBytes(raw)}
	if err := validateArtifactDocument(document); err != nil {
		return nil, err
	}
	return document, nil
}

func validateArtifactDocument(document *ArtifactDocument) error {
	metadata := document.Metadata
	if !containsString(templateKinds(), metadata.Kind) {
		return &CLIError{Code: "CHS-ARTIFACT-KIND", Message: "unsupported artifact kind: " + metadata.Kind, ExitCode: 10}
	}
	expectedPath := ""
	requiredHeadings := []string{}
	switch metadata.Kind {
	case "requirements":
		if metadata.ID != "requirements" {
			return &CLIError{Code: "CHS-ARTIFACT-ID", Message: "requirements ID must be requirements", ExitCode: 10}
		}
		expectedPath = "docs/requirements.md"
		requiredHeadings = []string{"# Requirements", "## Problem", "## Required Behavior", "## Success Criteria", "## Scope", "## Constraints", "## Decisions Required from Master"}
	case "architecture":
		if metadata.ID != "architecture" || metadata.RequirementsDigest == "" {
			return &CLIError{Code: "CHS-ARTIFACT-ID", Message: "architecture requires ID architecture and requirements_digest", ExitCode: 10}
		}
		expectedPath = "docs/architecture.md"
		requiredHeadings = []string{"# Architecture", "## System Context", "## Components and Boundaries", "## Interfaces", "## Data and State", "## Security", "## Validation Strategy", "## Parallelization Boundaries", "## Decisions Required from Master"}
	case "mission":
		if !idPatterns["mission"].MatchString(metadata.ID) || len(metadata.TaskIDs) == 0 || metadata.RequirementsDigest == "" || metadata.ArchitectureDigest == "" {
			return &CLIError{Code: "CHS-ARTIFACT-ID", Message: "mission requires a valid ID, design digests, and at least one task_id", ExitCode: 10}
		}
		expectedPath = "docs/missions/" + metadata.ID + ".md"
		requiredHeadings = []string{"# Mission " + metadata.ID, "## Outcome", "## Requirements Covered", "## Acceptance Criteria", "## Constraints and Risks", "## Completion Evidence"}
	case "task":
		if !idPatterns["task"].MatchString(metadata.ID) || metadata.MissionID != strings.Split(metadata.ID, "-")[0] || len(metadata.AllowedPaths) == 0 || len(metadata.AcceptanceChecks) == 0 || metadata.RequirementsDigest == "" || metadata.ArchitectureDigest == "" {
			return &CLIError{Code: "CHS-ARTIFACT-ID", Message: "task requires valid IDs, design digests, allowed_paths, and acceptance_checks", ExitCode: 10}
		}
		expectedPath = "docs/tasks/" + metadata.ID + ".md"
		requiredHeadings = []string{"# Task " + metadata.ID, "## Objective", "## Inputs and Assumptions", "## Forbidden and Out of Scope", "## Deliverables", "## Stop Conditions", "## Reviewer Attention"}
		if metadata.Budget != nil {
			if err := validateTaskBudgetDefinition(*metadata.Budget); err != nil {
				return err
			}
		}
	}
	if document.Path != expectedPath {
		return &CLIError{Code: "CHS-ARTIFACT-PATH", Message: fmt.Sprintf("%s artifact must be stored at %s", metadata.Kind, expectedPath), ExitCode: 10}
	}
	for _, heading := range requiredHeadings {
		if !hasHeading(document.Body, heading) {
			return &CLIError{Code: "CHS-ARTIFACT-SECTION", Message: "missing required heading: " + heading, ExitCode: 10}
		}
	}
	if strings.Contains(document.Body, "REPLACE_") || strings.Contains(document.Body, "<replace") {
		return &CLIError{Code: "CHS-ARTIFACT-PLACEHOLDER", Message: "artifact still contains template placeholders", ExitCode: 10}
	}
	seen := map[string]struct{}{}
	for _, check := range metadata.AcceptanceChecks {
		if err := validateCheckSpec(check); err != nil {
			return err
		}
		if err := validateIndependentVerification(metadata.AllowedPaths, check); err != nil {
			return err
		}
		if _, ok := seen[check.ID]; ok {
			return &CLIError{Code: "CHS-ARTIFACT-CHECK", Message: "duplicate acceptance check: " + check.ID, ExitCode: 10}
		}
		seen[check.ID] = struct{}{}
	}
	return nil
}

func validateIndependentVerification(allowedPaths []string, check CheckSpec) error {
	if len(check.VerificationPaths) == 0 {
		return &CLIError{Code: "CHS-ARTIFACT-VERIFICATION", Message: "each acceptance check requires independently owned verification_paths", ExitCode: 10}
	}
	seen := map[string]bool{}
	for _, pattern := range check.VerificationPaths {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(pattern)))
		if pattern == "" || filepath.IsAbs(pattern) || strings.ContainsRune(pattern, '\x00') || clean == "." || clean != filepath.ToSlash(pattern) || strings.HasPrefix(clean, "../") || seen[clean] {
			return &CLIError{Code: "CHS-ARTIFACT-VERIFICATION", Message: "verification_paths must be unique project-relative path patterns", ExitCode: 10}
		}
		seen[clean] = true
	}
	if pathsOverlap(allowedPaths, check.VerificationPaths) {
		return &CLIError{Code: "CHS-ARTIFACT-VERIFICATION", Message: "verification_paths must not overlap Developer allowed_paths", ExitCode: 10}
	}
	return nil
}

func hasHeading(body, heading string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == heading || strings.HasPrefix(strings.TrimSpace(line), heading+":") {
			return true
		}
	}
	return false
}

func validateArtifactAgainstState(document *ArtifactDocument, state State) error {
	metadata := document.Metadata
	requirements := state.Artifacts["requirements"]
	architecture := state.Artifacts["architecture"]
	switch metadata.Kind {
	case "architecture":
		if requirements.Status != "accepted" || metadata.RequirementsDigest != requirements.Digest {
			return &CLIError{Code: "CHS-ARTIFACT-BASELINE", Message: "architecture must reference the accepted requirements digest", ExitCode: 10}
		}
	case "mission", "task":
		if requirements.Status != "accepted" || architecture.Status != "accepted" || metadata.RequirementsDigest != requirements.Digest || metadata.ArchitectureDigest != architecture.Digest {
			return &CLIError{Code: "CHS-ARTIFACT-BASELINE", Message: metadata.Kind + " must reference accepted requirements and architecture digests", ExitCode: 10}
		}
	}
	if current, ok := state.Artifacts[metadata.ID]; ok && current.Status == "accepted" && current.Digest != document.Digest {
		return &CLIError{Code: "CHS-ARTIFACT-FROZEN", Message: "accepted artifact is frozen; create a new artifact ID or design-change flow", ExitCode: 10}
	}
	return nil
}

func submitArtifact(root, path string, principal Principal, expected int64) (State, State, ArtifactState, error) {
	document, err := parseArtifact(root, path)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	if err := validateArtifactAgainstState(document, state); err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	submissionID, err := newID("ART")
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	artifact := ArtifactState{
		ID: document.Metadata.ID, Kind: document.Metadata.Kind, Path: document.Path, Digest: document.Digest,
		Status: "submitted", SubmissionID: submissionID, SubmittedBy: principal.Actor, UpdatedAt: timeNow(),
	}
	previous, next, _, err := updateState(root, principal, "artifact.submitted", artifact.ID, expected, func(next *State) error {
		next.Artifacts[artifact.ID] = artifact
		switch artifact.Kind {
		case "mission":
			next.Missions[artifact.ID] = MissionState{ID: artifact.ID, ArtifactID: artifact.ID, Status: "planned", TaskIDs: append([]string{}, document.Metadata.TaskIDs...), UpdatedAt: timeNow()}
		case "task":
			budget := config.DefaultTaskBudget
			if document.Metadata.Budget != nil {
				budget = *document.Metadata.Budget
			}
			next.Tasks[artifact.ID] = TaskState{
				ID: artifact.ID, MissionID: document.Metadata.MissionID, ArtifactID: artifact.ID, Status: "planned",
				DependsOn: append([]string{}, document.Metadata.DependsOn...), AllowedPaths: append([]string{}, document.Metadata.AllowedPaths...),
				Budget: budget, Checks: append([]CheckSpec{}, document.Metadata.AcceptanceChecks...), CheckResults: map[string]CheckResult{}, UpdatedAt: timeNow(),
			}
		}
		return nil
	})
	return previous, next, artifact, err
}

func acceptArtifact(root, submissionID string, principal Principal, expected int64) (State, State, ArtifactState, error) {
	config, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	var artifact ArtifactState
	found := false
	for _, candidate := range state.Artifacts {
		if candidate.SubmissionID == submissionID {
			artifact, found = candidate, true
			break
		}
	}
	if !found || artifact.Status != "submitted" {
		return State{}, State{}, ArtifactState{}, &CLIError{Code: "CHS-ARTIFACT-NOT-PENDING", Message: "artifact submission is not pending", ExitCode: 10}
	}
	intent := struct {
		SubmissionID string `json:"submission_id"`
		ArtifactID   string `json:"artifact_id"`
		Path         string `json:"path"`
		Digest       string `json:"digest"`
	}{SubmissionID: submissionID, ArtifactID: artifact.ID, Path: artifact.Path, Digest: artifact.Digest}
	previous, next, _, err := executeGitOperation(root, "artifact.accept", "artifact.accepted", artifact.ID, principal, expected, intent, func(current State) (preparedOperation, error) {
		currentArtifact, found := artifactBySubmission(current, submissionID)
		if !found || currentArtifact.ID != artifact.ID || currentArtifact.Status != "submitted" {
			return preparedOperation{}, &CLIError{Code: "CHS-ARTIFACT-NOT-PENDING", Message: "artifact submission is not pending", ExitCode: 10}
		}
		document, err := parseArtifact(root, currentArtifact.Path)
		if err != nil {
			return preparedOperation{}, err
		}
		if document.Digest != currentArtifact.Digest {
			return preparedOperation{}, &CLIError{Code: "CHS-ARTIFACT-CHANGED", Message: "artifact content changed after submission", ExitCode: 10}
		}
		branch, err := currentBranch(root)
		if err != nil {
			return preparedOperation{}, err
		}
		if branch != config.DefaultBranch {
			return preparedOperation{}, &CLIError{Code: "CHS-ARTIFACT-BRANCH", Message: "artifacts must be accepted on the default branch", ExitCode: 10}
		}
		before, commit, err := gitPrepareCommit(root, "Accept "+currentArtifact.Kind+" "+currentArtifact.ID, currentArtifact.Path)
		if err != nil {
			return preparedOperation{}, err
		}
		if before != current.Baseline {
			return preparedOperation{}, &CLIError{Code: "CHS-ARTIFACT-BASELINE-MOVED", Message: "default branch no longer matches the state baseline", ExitCode: 12, Retryable: true}
		}
		tree, err := git(root, "rev-parse", commit+"^{tree}")
		if err != nil {
			return preparedOperation{}, err
		}
		return preparedOperation{
			Payload:  artifactAcceptedPayload{ArtifactID: currentArtifact.ID, AcceptedCommit: commit},
			GitAfter: GitOperationState{Branch: branch, Head: commit, IndexTree: tree},
			ApplyGit: func() error { return applyPreparedCommit(root, branch, before, commit) },
		}, nil
	})
	if err == nil {
		artifact = next.Artifacts[artifact.ID]
	}
	return previous, next, artifact, err
}

func rejectArtifact(root, submissionID, reason string, principal Principal, expected int64) (State, State, ArtifactState, error) {
	if strings.TrimSpace(reason) == "" {
		return State{}, State{}, ArtifactState{}, &CLIError{Code: "CHS-ARTIFACT-REASON", Message: "artifact rejection requires a reason", ExitCode: 20}
	}
	_, _, state, err := loadProject(root)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	expected, err = effectiveExpected(state, expected)
	if err != nil {
		return State{}, State{}, ArtifactState{}, err
	}
	artifact, found := artifactBySubmission(state, submissionID)
	if !found || artifact.Status != "submitted" {
		return State{}, State{}, ArtifactState{}, &CLIError{Code: "CHS-ARTIFACT-NOT-PENDING", Message: "artifact submission is not pending", ExitCode: 10}
	}
	artifact.Status = "rejected"
	artifact.RejectedBy = principal.Actor
	artifact.RejectionReason = reason
	artifact.UpdatedAt = timeNow()
	previous, next, _, err := updateState(root, principal, "artifact.rejected", artifact.ID, expected, func(next *State) error {
		next.Artifacts[artifact.ID] = artifact
		return nil
	})
	return previous, next, artifact, err
}

func timeNow() time.Time { return time.Now().UTC() }

func taskGraphIssues(mission MissionState, tasks map[string]TaskState) error {
	missionSet := stringSet(mission.TaskIDs)
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return &CLIError{Code: "CHS-TASK-CYCLE", Message: "task dependency cycle includes " + id, ExitCode: 10}
		}
		if visited[id] {
			return nil
		}
		task, ok := tasks[id]
		if !ok {
			return &CLIError{Code: "CHS-TASK-MISSING", Message: "mission references missing task " + id, ExitCode: 10}
		}
		visiting[id] = true
		for _, dependency := range task.DependsOn {
			if _, ok := missionSet[dependency]; !ok {
				return &CLIError{Code: "CHS-TASK-DEPENDENCY", Message: fmt.Sprintf("task %s depends on task outside mission: %s", id, dependency), ExitCode: 10}
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, id := range mission.TaskIDs {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func matchAllowed(pattern, path string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		if pattern[index] == '*' {
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				expression.WriteString(".*")
				index++
			} else {
				expression.WriteString("[^/]*")
			}
		} else {
			expression.WriteString(regexp.QuoteMeta(string(pattern[index])))
		}
	}
	expression.WriteString("$")
	matched, _ := regexp.MatchString(expression.String(), path)
	return matched
}

func allowedFile(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if matchAllowed(pattern, path) {
			return true
		}
	}
	return false
}

func sortedArtifactIDs(artifacts map[string]ArtifactState) []string {
	ids := make([]string, 0, len(artifacts))
	for id := range artifacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
