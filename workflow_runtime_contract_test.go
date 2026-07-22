package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type parsedWorkflowAction struct {
	name    string
	sha     string
	version string
}

func TestWorkflowRuntimeSyntaxIsUnambiguous(t *testing.T) {
	err := filepath.WalkDir(filepath.Join(".github", "workflows"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if _, _, parseErr := parseWorkflowRuntime(string(content)); parseErr != nil {
			t.Errorf("%s: %v", filepath.Base(path), parseErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowRuntimeParserHandlesValidYAMLForms(t *testing.T) {
	for _, workflow := range []string{
		"jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - \"uses\": actions/checkout@v0.0.0-peer-mutant\n",
		"jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - uses : actions/checkout@v0.0.0-peer-mutant\n",
		"jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps: [{uses: actions/checkout@v0.0.0-peer-mutant}]\n",
	} {
		if _, _, err := parseWorkflowRuntime(workflow); err == nil {
			t.Fatal("parseWorkflowRuntime accepted an action without a commit SHA")
		}
	}

	otherSHA := "0000000000000000000000000000000000000000"
	escapedAction := "jobs:\n    test:\n        runs-on: ubuntu-24.04\n        steps:\n            - uses: \"github/codeql-action/\\u0069nit@" + otherSHA + "\"\n"
	actions, runners, err := parseWorkflowRuntime(escapedAction)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].name != "github/codeql-action/init" || actions[0].sha != otherSHA {
		t.Fatalf("parseWorkflowRuntime actions = %+v, want decoded CodeQL init", actions)
	}
	if len(runners) != 1 || runners[0] != "ubuntu-24.04" {
		t.Fatalf("parseWorkflowRuntime runners = %v, want ubuntu-24.04", runners)
	}

	quotedRunner := "jobs:\n    test:\n        \"runs-on\": macos-15\n"
	_, runners, err = parseWorkflowRuntime(quotedRunner)
	if err != nil || len(runners) != 1 || runners[0] != "macos-15" {
		t.Fatalf("parseWorkflowRuntime runners = %v, error = %v", runners, err)
	}

	blockScalar := "jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: |\n          uses: actions/checkout@v0.0.0-peer-mutant\n"
	actions, _, err = parseWorkflowRuntime(blockScalar)
	if err != nil || len(actions) != 0 {
		t.Fatalf("parseWorkflowRuntime actions = %+v, error = %v", actions, err)
	}
}

func TestWorkflowRuntimeParserRejectsAmbiguousYAML(t *testing.T) {
	sha := "3d3c42e5aac5ba805825da76410c181273ba90b1"
	for _, workflow := range []string{
		"jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - &checkout\n        uses: actions/checkout@" + sha + "\n      - *checkout\n",
		"jobs:\n  first:\n    runs-on: ubuntu-24.04\njobs:\n  second:\n    runs-on: macos-15\n",
		"jobs:\n  test:\n    runs-on: ubuntu-24.04\n    steps:\n      - uses: actions/checkout@" + sha + "\n        uses: actions/checkout@0000000000000000000000000000000000000000\n",
		"jobs:\n  first:\n    runs-on: ubuntu-24.04\n---\njobs:\n  second:\n    runs-on: macos-15\n",
	} {
		if _, _, err := parseWorkflowRuntime(workflow); err == nil {
			t.Fatal("parseWorkflowRuntime accepted ambiguous YAML")
		}
	}
}

func TestDecodeStrictJSONRejectsAmbiguousData(t *testing.T) {
	for _, data := range []string{
		`{"name":"one","unknown":"value"}`,
		`{"name":"one","name":"two"}`,
		`{"name":"one"} {"name":"two"}`,
	} {
		var value struct {
			Name string `json:"name"`
		}
		if err := decodeStrictJSON(strings.NewReader(data), &value); err == nil {
			t.Fatal("decodeStrictJSON accepted ambiguous data")
		}
	}
}

func TestWorkflowRuntimeParserRejectsAnchorOnMappingKey(t *testing.T) {
	workflow := `jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - &action_key uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
`
	if _, _, err := parseWorkflowRuntime(workflow); err == nil {
		t.Fatal("parseWorkflowRuntime accepted an anchor on a mapping key")
	}
}

func TestWorkflowRuntimeParserDoesNotOmitDirectMatrixRunnerAxis(t *testing.T) {
	workflow := `jobs:
  test:
    runs-on: ${{ matrix.runner }}
    strategy:
      matrix:
        runner: [ubuntu-24.04, self-hosted]
        include:
          - runner: ubuntu-24.04
`
	_, runners, err := parseWorkflowRuntime(workflow)
	if err != nil {
		return
	}
	if slices.Contains(runners, "self-hosted") {
		return
	}
	t.Fatalf("parseWorkflowRuntime silently omitted the direct matrix runner axis: %v", runners)
}

func TestDecodeStrictJSONRejectsCaseFoldedDuplicateField(t *testing.T) {
	var value struct {
		Name string `json:"name"`
	}
	if err := decodeStrictJSON(strings.NewReader(`{"name":"one","Name":"two"}`), &value); err == nil {
		t.Fatalf("decodeStrictJSON accepted two keys for one Go field and selected %q", value.Name)
	}
}

type interfacePayload struct {
	Name string `json:"name"`
}

func TestDecodeStrictJSONRejectsCaseFoldedFieldBehindTypedInterface(t *testing.T) {
	value := struct {
		Payload any `json:"payload"`
	}{Payload: &interfacePayload{}}
	if err := decodeStrictJSON(strings.NewReader(`{"payload":{"Name":"two"}}`), &value); err == nil {
		t.Fatalf("decodeStrictJSON accepted case-folded nested field and selected %q", value.Payload.(*interfacePayload).Name)
	}
}

func TestWorkflowActionsIgnoreUsesInputKeys(t *testing.T) {
	workflow := `jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
        with:
          uses: ordinary-input-value
`
	actions, _, err := parseWorkflowRuntime(workflow)
	if err != nil {
		t.Fatalf("parseWorkflowRuntime rejected a valid action input named uses: %v", err)
	}
	if len(actions) != 1 || actions[0].name != "actions/checkout" {
		t.Fatalf("actions = %+v, want only the executable checkout step", actions)
	}
}

func TestWorkflowActionsIgnoreNonExecutableUsesMappings(t *testing.T) {
	workflow := `env:
  uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - run: echo test
`
	actions, _, err := parseWorkflowRuntime(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %+v, want no executable action references", actions)
	}
}

func decodeStrictJSON(reader io.Reader, output any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 {
		return fmt.Errorf("JSON has no document root")
	}
	if err := validateUnambiguousYAML(document.Content[0]); err != nil {
		return err
	}
	outputType := reflect.TypeOf(output)
	if outputType == nil || outputType.Kind() != reflect.Pointer {
		return fmt.Errorf("JSON output must be a pointer")
	}
	if err := validateExactJSONFields(document.Content[0], outputType.Elem(), reflect.ValueOf(output).Elem()); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON has more than one value")
		}
		return err
	}
	return nil
}

func parseWorkflowRuntime(content string) ([]parsedWorkflowAction, []string, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, err
	}
	if len(document.Content) != 1 {
		return nil, nil, fmt.Errorf("workflow has no document root")
	}
	if err := validateUnambiguousYAML(document.Content[0]); err != nil {
		return nil, nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("workflow has more than one YAML document")
		}
		return nil, nil, err
	}
	actions, err := workflowActions(document.Content[0])
	if err != nil {
		return nil, nil, err
	}
	runners, err := workflowRunnerValues(document.Content[0])
	return actions, runners, err
}

func validateUnambiguousYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("YAML anchors and aliases are not supported")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if err := validateUnambiguousYAML(key); err != nil {
				return err
			}
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("line %d has a non-string mapping key", key.Line)
			}
			if seen[key.Value] {
				return fmt.Errorf("line %d repeats mapping key %q", key.Line, key.Value)
			}
			seen[key.Value] = true
			if err := validateUnambiguousYAML(value); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := validateUnambiguousYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func validateExactJSONFields(node *yaml.Node, valueType reflect.Type, value reflect.Value) error {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
		if value.IsValid() && value.Kind() == reflect.Pointer && !value.IsNil() {
			value = value.Elem()
		} else {
			value = reflect.Value{}
		}
	}
	if valueType.Kind() == reflect.Interface {
		if !value.IsValid() || value.Kind() != reflect.Interface || value.IsNil() {
			return nil
		}
		value = value.Elem()
		return validateExactJSONFields(node, value.Type(), value)
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		type fieldInfo struct {
			index int
			type_ reflect.Type
		}
		fields := make(map[string]fieldInfo)
		for index := 0; index < valueType.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = fieldInfo{index: index, type_: field.Type}
		}
		for index := 0; index < len(node.Content); index += 2 {
			key, child := node.Content[index], node.Content[index+1]
			field, ok := fields[key.Value]
			if !ok {
				return fmt.Errorf("JSON field %q does not use an exact declared name", key.Value)
			}
			fieldValue := reflect.Value{}
			if value.IsValid() && value.Kind() == reflect.Struct {
				fieldValue = value.Field(field.index)
			}
			if err := validateExactJSONFields(child, field.type_, fieldValue); err != nil {
				return fmt.Errorf("JSON field %q: %w", key.Value, err)
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for index := 0; index < len(node.Content); index += 2 {
			childValue := reflect.Value{}
			if value.IsValid() && value.Kind() == reflect.Map && valueType.Key().Kind() == reflect.String {
				mapKey := reflect.New(valueType.Key()).Elem()
				mapKey.SetString(node.Content[index].Value)
				childValue = value.MapIndex(mapKey)
			}
			if err := validateExactJSONFields(node.Content[index+1], valueType.Elem(), childValue); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		for index, child := range node.Content {
			childValue := reflect.Value{}
			if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) && index < value.Len() {
				childValue = value.Index(index)
			}
			if err := validateExactJSONFields(child, valueType.Elem(), childValue); err != nil {
				return err
			}
		}
	}
	return nil
}

func workflowActions(node *yaml.Node) ([]parsedWorkflowAction, error) {
	var actions []parsedWorkflowAction
	appendAction := func(value *yaml.Node) error {
		if value.Kind != yaml.ScalarNode {
			return fmt.Errorf("line %d has a non-scalar action reference", value.Line)
		}
		if strings.HasPrefix(value.Value, "./") {
			return nil
		}
		separator := strings.LastIndexByte(value.Value, '@')
		if separator < 1 {
			return fmt.Errorf("line %d has an action without a commit ref", value.Line)
		}
		name, sha := value.Value[:separator], value.Value[separator+1:]
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) {
			return fmt.Errorf("line %d uses %s with non-SHA ref %s", value.Line, name, sha)
		}
		version := strings.TrimSpace(strings.TrimPrefix(value.LineComment, "#"))
		actions = append(actions, parsedWorkflowAction{name: name, sha: sha, version: version})
		return nil
	}
	jobs, ok := workflowMapValue(node, "jobs")
	if !ok {
		return actions, nil
	}
	if jobs.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow jobs is not a mapping")
	}
	for index := 1; index < len(jobs.Content); index += 2 {
		job := jobs.Content[index]
		if job.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d has a non-mapping job", job.Line)
		}
		if uses, ok := workflowMapValue(job, "uses"); ok {
			if err := appendAction(uses); err != nil {
				return nil, err
			}
		}
		steps, ok := workflowMapValue(job, "steps")
		if !ok {
			continue
		}
		if steps.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("line %d has non-sequence steps", steps.Line)
		}
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("line %d has a non-mapping step", step.Line)
			}
			if uses, ok := workflowMapValue(step, "uses"); ok {
				if err := appendAction(uses); err != nil {
					return nil, err
				}
			}
		}
	}
	return actions, nil
}

func workflowRunnerValues(root *yaml.Node) ([]string, error) {
	jobs, ok := workflowMapValue(root, "jobs")
	if !ok || jobs.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow has no jobs mapping")
	}
	var runners []string
	for index := 0; index < len(jobs.Content); index += 2 {
		name, job := jobs.Content[index].Value, jobs.Content[index+1]
		if job.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("job %s is not a mapping", name)
		}
		runsOn, ok := workflowMapValue(job, "runs-on")
		if !ok {
			continue
		}
		if runsOn.Kind != yaml.ScalarNode || runsOn.Value == "" {
			return nil, fmt.Errorf("job %s has an invalid runs-on value", name)
		}
		if !strings.Contains(runsOn.Value, "${{") {
			runners = append(runners, runsOn.Value)
			continue
		}
		if runsOn.Value != "${{ matrix.runner }}" {
			return nil, fmt.Errorf("job %s has unsupported runs-on expression %q", name, runsOn.Value)
		}
		matrixRunners, err := workflowMatrixRunners(job)
		if err != nil {
			return nil, fmt.Errorf("job %s: %w", name, err)
		}
		runners = append(runners, matrixRunners...)
	}
	return runners, nil
}

func workflowMatrixRunners(job *yaml.Node) ([]string, error) {
	strategy, ok := workflowMapValue(job, "strategy")
	if !ok || strategy.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("matrix.runner has no strategy mapping")
	}
	matrix, ok := workflowMapValue(strategy, "matrix")
	if !ok || matrix.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("matrix.runner has no matrix mapping")
	}
	var runners []string
	seen := make(map[string]bool)
	appendRunner := func(value *yaml.Node, source string) error {
		if value.Kind != yaml.ScalarNode || value.Value == "" || strings.Contains(value.Value, "${{") {
			return fmt.Errorf("%s has an invalid runner", source)
		}
		if !seen[value.Value] {
			seen[value.Value] = true
			runners = append(runners, value.Value)
		}
		return nil
	}
	if axis, ok := workflowMapValue(matrix, "runner"); ok {
		if axis.Kind != yaml.SequenceNode || len(axis.Content) == 0 {
			return nil, fmt.Errorf("matrix.runner has no fixed values")
		}
		for _, value := range axis.Content {
			if err := appendRunner(value, "matrix.runner"); err != nil {
				return nil, err
			}
		}
	}
	if include, ok := workflowMapValue(matrix, "include"); ok {
		if include.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("matrix.include is not a sequence")
		}
		for _, entry := range include.Content {
			value, ok := workflowMapValue(entry, "runner")
			if !ok {
				return nil, fmt.Errorf("matrix.include entry has no runner")
			}
			if err := appendRunner(value, "matrix.include"); err != nil {
				return nil, err
			}
		}
	}
	if len(runners) == 0 {
		return nil, fmt.Errorf("matrix.runner has no fixed values")
	}
	return runners, nil
}

func workflowMapValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	if mapping.Kind != yaml.MappingNode {
		return nil, false
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1], true
		}
	}
	return nil, false
}
