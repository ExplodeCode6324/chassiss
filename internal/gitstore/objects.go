package gitstore

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

type Commit struct {
	OID       string
	Tree      string
	Parents   []string
	Signature string
	Message   string
}

type Entry struct {
	Mode string
	OID  string
}

type TreeMap map[string]Entry

func (runner Runner) ObjectFormat(ctx context.Context) (string, error) {
	result, err := runner.Run(ctx, "rev-parse", "--show-object-format")
	if err != nil {
		// Git versions before --show-object-format only support SHA-1.
		return "sha1", nil
	}
	format := strings.TrimSpace(string(result.Stdout))
	if format != "sha1" && format != "sha256" {
		return "", fmt.Errorf("unsupported Git object format %q", format)
	}
	return format, nil
}

func (runner Runner) Resolve(ctx context.Context, revision string) (string, error) {
	result, err := runner.Run(ctx, "rev-parse", "--verify", revision+"^{object}")
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(result.Stdout))
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return "", err
	}
	if err := protocol.ValidateOID(oid, format); err != nil {
		return "", err
	}
	return oid, nil
}

func (runner Runner) ReadCommit(ctx context.Context, oid string) (Commit, error) {
	result, err := runner.Run(ctx, "cat-file", "commit", oid)
	if err != nil {
		return Commit{}, err
	}
	raw := result.Stdout
	separator := bytes.Index(raw, []byte("\n\n"))
	if separator < 0 {
		return Commit{}, fmt.Errorf("commit object has no message separator")
	}
	headers := strings.Split(string(raw[:separator]), "\n")
	commit := Commit{OID: oid, Parents: make([]string, 0)}
	var signature strings.Builder
	for index := 0; index < len(headers); index++ {
		line := headers[index]
		switch {
		case strings.HasPrefix(line, "tree "):
			if commit.Tree != "" {
				return Commit{}, fmt.Errorf("commit has duplicate tree header")
			}
			commit.Tree = strings.TrimPrefix(line, "tree ")
		case strings.HasPrefix(line, "parent "):
			commit.Parents = append(commit.Parents, strings.TrimPrefix(line, "parent "))
		case strings.HasPrefix(line, "gpgsig "):
			signature.WriteString(strings.TrimPrefix(line, "gpgsig "))
			for index+1 < len(headers) && strings.HasPrefix(headers[index+1], " ") {
				index++
				signature.WriteByte('\n')
				signature.WriteString(strings.TrimPrefix(headers[index], " "))
			}
		}
	}
	if commit.Tree == "" {
		return Commit{}, fmt.Errorf("commit has no tree")
	}
	commit.Signature = signature.String()
	commit.Message = string(raw[separator+2:])
	return commit, nil
}

func (runner Runner) ReadTree(ctx context.Context, treeish string) (TreeMap, error) {
	result, err := runner.Run(ctx, "ls-tree", "-rz", "--full-tree", "-r", treeish)
	if err != nil {
		return nil, err
	}
	mapping := make(TreeMap)
	for _, record := range bytes.Split(result.Stdout, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("malformed ls-tree record")
		}
		metadata := strings.Split(string(record[:tab]), " ")
		if len(metadata) != 3 {
			return nil, fmt.Errorf("malformed ls-tree metadata")
		}
		path := string(record[tab+1:])
		if _, exists := mapping[path]; exists {
			return nil, fmt.Errorf("duplicate tree path %q", path)
		}
		mapping[path] = Entry{Mode: metadata[0], OID: metadata[2]}
	}
	return mapping, nil
}

func ChangedPaths(left, right TreeMap) []string {
	union := make(map[string]struct{}, len(left)+len(right))
	for path := range left {
		union[path] = struct{}{}
	}
	for path := range right {
		union[path] = struct{}{}
	}
	result := make([]string, 0)
	for path := range union {
		if left[path] != right[path] {
			result = append(result, path)
		}
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare([]byte(result[i]), []byte(result[j])) < 0 })
	return result
}

func Overlay(base, work, main TreeMap) (TreeMap, []string, error) {
	changed := ChangedPaths(base, work)
	result := cloneTree(main)
	for _, path := range changed {
		if entry, exists := work[path]; exists {
			result[path] = entry
		} else {
			delete(result, path)
		}
	}
	if err := validateFlatTree(result); err != nil {
		return nil, nil, err
	}
	collateral := ChangedPaths(main, result)
	changedSet := make(map[string]struct{}, len(changed))
	for _, path := range changed {
		changedSet[path] = struct{}{}
	}
	for _, path := range collateral {
		if _, expected := changedSet[path]; !expected {
			return nil, nil, fmt.Errorf("candidate overlay changed collateral path %s", path)
		}
	}
	return result, changed, nil
}

func validateFlatTree(mapping TreeMap) error {
	paths := make([]string, 0, len(mapping))
	for path, entry := range mapping {
		if path == "" || strings.HasSuffix(path, "/") {
			return fmt.Errorf("invalid flat tree path %q", path)
		}
		if !validMode(entry.Mode) {
			return fmt.Errorf("unsupported Git tree mode %q at %s", entry.Mode, path)
		}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return bytes.Compare([]byte(paths[i]), []byte(paths[j])) < 0 })
	for index := 0; index+1 < len(paths); index++ {
		if strings.HasPrefix(paths[index+1], paths[index]+"/") {
			return fmt.Errorf("tree prefix collision between %s and %s", paths[index], paths[index+1])
		}
	}
	return nil
}

func validMode(mode string) bool {
	switch mode {
	case "100644", "100755", "120000", "160000":
		return true
	default:
		return false
	}
}

func cloneTree(value TreeMap) TreeMap {
	result := make(TreeMap, len(value))
	for path, entry := range value {
		result[path] = entry
	}
	return result
}

func (runner Runner) HashBlob(ctx context.Context, data []byte) (string, error) {
	result, err := runner.RunWithInput(ctx, data, nil, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (runner Runner) ReadBlob(ctx context.Context, oid string) ([]byte, error) {
	result, err := runner.Run(ctx, "cat-file", "blob", oid)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func HashObject(objectType string, data []byte, objectFormat string) (string, error) {
	header := []byte(objectType + " " + strconv.Itoa(len(data)) + "\x00")
	input := append(header, data...)
	switch objectFormat {
	case "sha1":
		sum := sha1.Sum(input)
		return hex.EncodeToString(sum[:]), nil
	case "sha256":
		sum := sha256.Sum256(input)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
}

// TreeOID computes the exact Git tree OID without modifying the repository.
func TreeOID(mapping TreeMap, objectFormat string) (string, error) {
	if err := validateFlatTree(mapping); err != nil {
		return "", err
	}
	root := &treeNode{files: map[string]Entry{}, directories: map[string]*treeNode{}}
	for path, entry := range mapping {
		segments := strings.Split(path, "/")
		current := root
		for _, segment := range segments[:len(segments)-1] {
			child := current.directories[segment]
			if child == nil {
				child = &treeNode{files: map[string]Entry{}, directories: map[string]*treeNode{}}
				current.directories[segment] = child
			}
			current = child
		}
		current.files[segments[len(segments)-1]] = entry
	}
	return hashTreeNode(root, objectFormat)
}

type treeNode struct {
	files       map[string]Entry
	directories map[string]*treeNode
}

type encodedTreeEntry struct {
	name     string
	sortName string
	mode     string
	oid      string
}

func hashTreeNode(node *treeNode, objectFormat string) (string, error) {
	entries := make([]encodedTreeEntry, 0, len(node.files)+len(node.directories))
	for name, entry := range node.files {
		entries = append(entries, encodedTreeEntry{
			name: name, sortName: name, mode: entry.Mode, oid: entry.OID,
		})
	}
	for name, child := range node.directories {
		oid, err := hashTreeNode(child, objectFormat)
		if err != nil {
			return "", err
		}
		entries = append(entries, encodedTreeEntry{
			name: name, sortName: name + "/", mode: "40000", oid: oid,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare([]byte(entries[i].sortName), []byte(entries[j].sortName)) < 0
	})
	var body bytes.Buffer
	for _, entry := range entries {
		rawOID, err := hex.DecodeString(entry.oid)
		if err != nil {
			return "", err
		}
		body.WriteString(entry.mode)
		body.WriteByte(' ')
		body.WriteString(entry.name)
		body.WriteByte(0)
		body.Write(rawOID)
	}
	return HashObject("tree", body.Bytes(), objectFormat)
}

func (runner Runner) WriteTree(ctx context.Context, mapping TreeMap) (string, error) {
	if err := validateFlatTree(mapping); err != nil {
		return "", err
	}
	gitDir, err := repositoryGitDir(ctx, runner)
	if err != nil {
		return "", err
	}
	index, err := newTemporaryIndex(gitDir)
	if err != nil {
		return "", err
	}
	defer index.Close()
	extra := map[string]string{"GIT_INDEX_FILE": index.Path}
	paths := make([]string, 0, len(mapping))
	for path := range mapping {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return bytes.Compare([]byte(paths[i]), []byte(paths[j])) < 0 })
	for _, path := range paths {
		entry := mapping[path]
		cacheInfo := entry.Mode + "," + entry.OID + "," + path
		if _, err := runner.RunWithInput(ctx, nil, extra, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
			return "", err
		}
	}
	result, err := runner.RunWithInput(ctx, nil, extra, "write-tree")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(string(result.Stdout))
	format, err := runner.ObjectFormat(ctx)
	if err != nil {
		return "", err
	}
	if err := protocol.ValidateOID(tree, format); err != nil {
		return "", err
	}
	return tree, nil
}

type temporaryIndex struct {
	Path string
}

func newTemporaryIndex(gitDir string) (*temporaryIndex, error) {
	file, err := createOwnerOnlyTemp(gitDir, "chassiss-index-")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return nil, err
	}
	// update-index requires a non-existent path rather than an empty file.
	if err := removeFile(path); err != nil {
		return nil, err
	}
	return &temporaryIndex{Path: path}, nil
}

func (index *temporaryIndex) Close() error { return removeFile(index.Path) }

func ParseMode(value string) (int, error) {
	number, err := strconv.ParseInt(value, 8, 32)
	return int(number), err
}
