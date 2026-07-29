package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type artifact struct {
	GOARCH string `json:"goarch"`
	GOOS   string `json:"goos"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Artifacts       []artifact `json:"artifacts"`
	Protocol        string     `json:"protocol"`
	ReleaseIdentity string     `json:"release_identity"`
	Schema          string     `json:"schema"`
	SourceCommit    string     `json:"source_commit"`
	Version         string     `json:"version"`
}

func main() {
	skill := flag.String("skill", "", "absolute Skill directory")
	source := flag.String("source", "", "source Git commit")
	version := flag.String("version", "", "bundle version")
	flag.Parse()
	if !filepath.IsAbs(*skill) || *source == "" || *version == "" {
		fail("--skill must be absolute and --source/--version must be non-empty")
	}

	targets := [][2]string{
		{"darwin", "arm64"},
		{"darwin", "amd64"},
		{"linux", "arm64"},
		{"linux", "amd64"},
	}
	artifacts := make([]artifact, 0, len(targets))
	checksums := make([]string, 0, len(targets))
	for _, target := range targets {
		relative := filepath.ToSlash(filepath.Join("bin", target[0]+"-"+target[1], "chassiss"))
		data, err := os.ReadFile(filepath.Join(*skill, filepath.FromSlash(relative)))
		if err != nil {
			fail(err.Error())
		}
		digest := sha256.Sum256(data)
		value := hex.EncodeToString(digest[:])
		artifacts = append(artifacts, artifact{
			GOARCH: target[1], GOOS: target[0], Path: relative, SHA256: value,
		})
		checksums = append(checksums, value+"  "+relative)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	sort.Strings(checksums)

	document, err := json.MarshalIndent(manifest{
		Artifacts: artifacts, Protocol: "chassiss/v1",
		ReleaseIdentity: "skill-bundled", Schema: "chassiss.skill-bundle/v1",
		SourceCommit: *source, Version: *version,
	}, "", "  ")
	if err != nil {
		fail(err.Error())
	}
	document = append(document, '\n')
	if err := os.WriteFile(filepath.Join(*skill, "manifest.json"), document, 0o644); err != nil {
		fail(err.Error())
	}
	checksumDocument := []byte(strings.Join(checksums, "\n") + "\n")
	if err := os.WriteFile(filepath.Join(*skill, "manifest.sha256"), checksumDocument, 0o644); err != nil {
		fail(err.Error())
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
