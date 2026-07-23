package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var liveDocumentationPaths = []string{
	"README.md",
	"CHANGELOG.md",
	"CODE_OF_CONDUCT.md",
	"CONTRIBUTING.md",
	"GOVERNANCE.md",
	"SECURITY.md",
	"SUPPORT.md",
	"docs/FUNCTIONS.md",
	"docs/LIMITATIONS.md",
	"docs/RUNTIME.md",
	"docs/STYLE.md",
	"docs/architecture.md",
	"docs/cli-reference.md",
	"docs/development.md",
	"docs/release-verification.md",
	"docs/releasing.md",
	"man/minecraft-ping.1",
}

func TestLiveDocumentationDeclaresSimplifiedTechnicalEnglish(t *testing.T) {
	t.Parallel()

	const declaration = "This document uses ASD-STE100 Simplified Technical English."
	for _, path := range liveDocumentationPaths {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(data), declaration) {
			t.Errorf("%s does not declare the documentation language", path)
		}
	}
}

func TestLiveDocumentationUsesControlledStyle(t *testing.T) {
	t.Parallel()

	for _, path := range liveDocumentationPaths {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		for _, finding := range controlledStyleFindings(string(data)) {
			t.Errorf("%s: %s", path, finding)
		}
	}
}

func TestControlledStyleCheckerRejectsUnsupportedProse(t *testing.T) {
	t.Parallel()

	for name, prose := range map[string]string{
		"passive voice": "Reports will be handled by the maintainer.",
		"long sentence": "This sentence has more than twenty five separate words because it deliberately adds many unnecessary words that make the instruction difficult for a reader to understand quickly and correctly.",
		"semicolon":     "Run the test; then read the result.",
		"em dash":       "Use one option — do not use two options.",
	} {
		if findings := controlledStyleFindings(prose); len(findings) == 0 {
			t.Errorf("style checker accepted %s", name)
		}
	}
}

func TestStyleGuideDocumentsAutomatedBoundaries(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "STYLE.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)
	for _, required := range []string{
		"25 words",
		"passive-voice patterns",
		"semicolons",
		"en dashes",
		"em dashes",
		"fenced code blocks",
		"link targets",
		"inline code",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("docs/STYLE.md does not state automated boundary %q", required)
		}
	}
}

func TestFunctionCatalogCoversProductionFunctions(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "FUNCTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(data)
	functions, err := productionFunctions(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := compareFunctionCatalog(functions, catalog); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionCatalogComparisonRejectsDrift(t *testing.T) {
	t.Parallel()

	valid := "| Function | Purpose |\n| --- | --- |\n| `main.one` | It returns one. |\n| `main.two` | It returns two. |\n"
	if err := compareFunctionCatalog([]string{"main.one", "main.two"}, valid); err != nil {
		t.Fatal(err)
	}
	if err := compareFunctionCatalog([]string{"main.one"}, valid); err == nil {
		t.Fatal("compareFunctionCatalog accepted a stale function")
	}
	if err := compareFunctionCatalog([]string{"main.one", "main.two", "main.three"}, valid); err == nil {
		t.Fatal("compareFunctionCatalog accepted a missing function")
	}
	duplicate := valid + "| `main.one` | It returns one again. |\n"
	if err := compareFunctionCatalog([]string{"main.one", "main.two"}, duplicate); err == nil {
		t.Fatal("compareFunctionCatalog accepted a duplicate function")
	}
}

func TestProductionFunctionsIgnoreVendoredModules(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "scripts"),
		filepath.Join(root, "vendor", "example.com", "dependency"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, source := range map[string]string{
		filepath.Join(root, "owned.go"):                                             "package owned\n\nfunc Owned() {}\n",
		filepath.Join(root, "vendor", "example.com", "dependency", "dependency.go"): "package dependency\n\nfunc Vendored() {}\n",
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	functions, err := productionFunctions(root)
	if err != nil {
		t.Fatal(err)
	}
	foundOwned := false
	for _, function := range functions {
		if strings.HasSuffix(function, ".Vendored") {
			t.Fatalf("productionFunctions included vendored function %q", function)
		}
		if strings.HasSuffix(function, ".Owned") {
			foundOwned = true
		}
	}
	if !foundOwned {
		t.Fatal("productionFunctions did not include the repository-owned function")
	}
}

func TestDocumentedLimitsCoverEnforcedConstants(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "LIMITATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"253 bytes",
		"1 through 65,535",
		"30 seconds",
		"250 milliseconds",
		"2 MiB",
		"1 MiB",
		"255 bytes",
		"2,048 bytes",
		"64 MiB",
		"five bytes",
		"2 minutes",
		"2 seconds",
		"500 milliseconds",
		"5 seconds",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("docs/LIMITATIONS.md does not contain %q", required)
		}
	}
}

func TestDocumentedBehaviorCoversNonobviousBoundaries(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "LIMITATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	limits := string(data)
	for _, required := range []string{
		"The interval is the minimum time from one probe start to the next probe",
		"The probe timeout starts after a TCP or UDP connection succeeds",
		"The probe timeout does not limit DNS, SRV lookup, or TCP or UDP connection",
		"replies are fewer than the count",
		"Numeric mode can put the host name in the first banner",
		"resolves the destination once before a text session",
		"socket operation can continue until its socket deadline",
		"9,223,372,036.854775807",
		"0.0.0.0/8",
		"ff00::/8",
		"default probe timeout of 12 seconds",
	} {
		if !strings.Contains(limits, required) {
			t.Errorf("docs/LIMITATIONS.md does not state behavior boundary %q", required)
		}
	}
}

func TestCLIReferenceAndManPageCoverEveryOption(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile(filepath.Join("docs", "cli-reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	manPage, err := os.ReadFile(filepath.Join("man", "minecraft-ping.1"))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := acceptedCLIOptions(source)
	if err != nil {
		t.Fatal(err)
	}
	sets := map[string][]string{
		"accepted options": accepted,
		"help output":      optionTokens(usageText()),
		"CLI reference":    markdownOptionTokens(string(reference)),
		"man page":         manPageOptionTokens(string(manPage)),
	}
	for name, options := range sets {
		if err := compareExactSets(accepted, options); err != nil {
			t.Errorf("%s option drift: %v", name, err)
		}
	}
}

func TestCLIOptionComparisonRejectsAcceptedAndHelpDrift(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	mutant := strings.Replace(
		string(source),
		`case "--version":`,
		`case "--numeric":
		return raw, true
	case "--version":`,
		1,
	)
	mutant = strings.Replace(
		mutant,
		"  --allow-private       allow private, loopback, and local-only IP targets",
		"  --allow-private       allow private, loopback, and local-only IP targets\n  --numeric             use numeric output",
		1,
	)
	accepted, err := acceptedCLIOptions([]byte(mutant))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile(filepath.Join("docs", "cli-reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := compareExactSets(accepted, markdownOptionTokens(string(reference))); err == nil {
		t.Fatal("option comparison accepted an undocumented parser and help option")
	}
}

func TestFunctionCatalogDocumentsSemanticContracts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "FUNCTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	descriptions := functionDescriptions(string(data))

	if got := descriptions["main.parseEdition"]; !strings.Contains(got, "empty value or `java` as Java") || !strings.Contains(got, "rejects other values") {
		t.Errorf("main.parseEdition description does not state the empty-value and rejection contracts: %q", got)
	}
	if parsed, err := parseEdition(""); err != nil || parsed != editionJava {
		t.Fatalf("empty edition contract changed: edition=%v error=%v", parsed, err)
	}
	if _, err := parseEdition("unsupported"); err == nil {
		t.Fatal("unsupported edition contract changed")
	}

	if got := descriptions["main.targetSpec.validate"]; !strings.Contains(got, "checks the port only when the destination has an explicit port") {
		t.Errorf("main.targetSpec.validate description does not state the optional-port contract: %q", got)
	}
	if err := newTargetSpec("example.com", 0, false).validate(); err != nil {
		t.Fatalf("implicit-port target contract changed: %v", err)
	}
	if err := newTargetSpec("example.com", 0, true).validate(); err == nil {
		t.Fatal("invalid explicit-port target contract changed")
	}

	const runProbeDescription = "It runs one JSON probe and decodes the result."
	if got := descriptions["cmd/release-integration.runProbe"]; got != runProbeDescription {
		t.Errorf("cmd/release-integration.runProbe description = %q, want %q", got, runProbeDescription)
	}
}

func controlledStyleFindings(document string) []string {
	var findings []string
	passiveVoice := regexp.MustCompile(`(?i)\b(?:am|is|are|was|were|be|been|being|will be|must be|can be|may be|should be)\s+(?:(?:not|also|always|currently|only|explicitly|privately|already)\s+)*(?:[a-z]+ed|built|cut|done|found|given|kept|known|made|read|run|seen|sent|set|shown|taken|told|used|written)\b`)
	word := regexp.MustCompile(`[A-Za-z0-9]+(?:[-'][A-Za-z0-9]+)*`)
	inlineCode := regexp.MustCompile("`[^`]*`")
	linkTarget := regexp.MustCompile(`\]\([^)]*\)`)
	sentenceEnd := regexp.MustCompile(`[.!?]+(?:[[:space:]]+|$)`)

	checkUnit := func(line int, raw string) {
		prose := inlineCode.ReplaceAllString(raw, " technical-name ")
		prose = linkTarget.ReplaceAllString(prose, "]")
		prose = strings.TrimSpace(strings.TrimLeft(prose, "#>*-+0123456789. "))
		if prose == "" || regexp.MustCompile(`^[-:| ]+$`).MatchString(prose) {
			return
		}
		if strings.ContainsAny(prose, ";–—") {
			findings = append(findings, fmt.Sprintf("line %d uses disallowed punctuation", line))
		}
		for _, sentence := range sentenceEnd.Split(prose, -1) {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			if count := len(word.FindAllString(sentence, -1)); count > 25 {
				findings = append(findings, fmt.Sprintf("line %d has a %d-word sentence (maximum 25)", line, count))
			}
			if passiveVoice.MatchString(sentence) {
				findings = append(findings, fmt.Sprintf("line %d has a passive-voice pattern: %q", line, sentence))
			}
		}
	}

	lines := strings.Split(document, "\n")
	var paragraph []string
	paragraphLine := 1
	inFence := false
	flush := func() {
		if len(paragraph) != 0 {
			checkUnit(paragraphLine, strings.Join(paragraph, " "))
			paragraph = nil
		}
	}
	for index, line := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == ".EX" {
			flush()
			inFence = true
			continue
		}
		if trimmed == ".EE" {
			inFence = false
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flush()
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "#"):
			flush()
		case strings.HasPrefix(trimmed, "."):
			flush()
		case strings.HasPrefix(trimmed, "|"):
			flush()
			for cell := range strings.SplitSeq(strings.Trim(trimmed, "|"), "|") {
				checkUnit(lineNumber, cell)
			}
		case regexp.MustCompile(`^(?:[-+*]|[0-9]+\.)[[:space:]]+`).MatchString(trimmed):
			flush()
			checkUnit(lineNumber, trimmed)
		default:
			if len(paragraph) == 0 {
				paragraphLine = lineNumber
			}
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return findings
}

func acceptedCLIOptions(source []byte) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "cli.go", source, 0)
	if err != nil {
		return nil, err
	}
	options := make(map[string]bool)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || (function.Name.Name != "consumeLongFlag" && function.Name.Name != "consumeShortFlags") {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				switch literal.Kind {
				case token.STRING:
					if strings.HasPrefix(value, "--") {
						options[value] = true
					}
				case token.CHAR:
					if len([]rune(value)) == 1 {
						options["-"+value] = true
					}
				}
			}
			return true
		})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("cli.go does not declare accepted options")
	}
	return sortedSet(options), nil
}

func optionTokens(text string) []string {
	pattern := regexp.MustCompile("(?:^|[[:space:],\\\"`])(--?[A-Za-z0-9][A-Za-z0-9-]*)")
	options := make(map[string]bool)
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		options[match[1]] = true
	}
	return sortedSet(options)
}

func markdownOptionTokens(document string) []string {
	section := between(document, "## Options", "## Important Notes")
	return optionTokens(section)
}

func manPageOptionTokens(document string) []string {
	section := between(document, ".SH OPTIONS", ".SH DESTINATIONS")
	return optionTokens(section)
}

func between(text string, start string, end string) string {
	startIndex := strings.Index(text, start)
	if startIndex < 0 {
		return ""
	}
	text = text[startIndex+len(start):]
	before, _, ok := strings.Cut(text, end)
	if !ok {
		return text
	}
	return before
}

func compareExactSets(expected []string, actual []string) error {
	expectedSet := make(map[string]bool, len(expected))
	actualSet := make(map[string]bool, len(actual))
	for _, value := range expected {
		expectedSet[value] = true
	}
	for _, value := range actual {
		actualSet[value] = true
	}
	var missing []string
	var stale []string
	for value := range expectedSet {
		if !actualSet[value] {
			missing = append(missing, value)
		}
	}
	for value := range actualSet {
		if !expectedSet[value] {
			stale = append(stale, value)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) != 0 || len(stale) != 0 {
		return fmt.Errorf("missing=%v stale=%v", missing, stale)
	}
	return nil
}

func sortedSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func functionDescriptions(catalog string) map[string]string {
	descriptions := make(map[string]string)
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\| (.+) \\|$")
	for _, match := range rowPattern.FindAllStringSubmatch(catalog, -1) {
		descriptions[match[1]] = match[2]
	}
	return descriptions
}

func compareFunctionCatalog(expected []string, catalog string) error {
	documented := make(map[string]bool)
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")
	for _, match := range rowPattern.FindAllStringSubmatch(catalog, -1) {
		name := match[1]
		if documented[name] {
			return fmt.Errorf("docs/FUNCTIONS.md contains duplicate function %s", name)
		}
		documented[name] = true
	}

	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	var missing []string
	for name := range expectedSet {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	var stale []string
	for name := range documented {
		if !expectedSet[name] {
			stale = append(stale, name)
		}
	}
	if len(missing) == 0 && len(stale) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return fmt.Errorf("docs/FUNCTIONS.md function drift: missing=%v stale=%v", missing, stale)
}

func productionFunctions(root string) ([]string, error) {
	functionSet := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		if directory == "." {
			directory = "main"
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := directory + "." + function.Name.Name
			if function.Recv != nil && len(function.Recv.List) != 0 {
				name = directory + "." + receiverName(function.Recv.List[0].Type) + "." + function.Name.Name
			}
			functionSet[name] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	shellFunction := regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\(\)[[:space:]]*\{`)
	err = filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".sh" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, match := range shellFunction.FindAllSubmatch(data, -1) {
			functionSet[filepath.ToSlash(relative)+":"+string(match[1])] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	functions := make([]string, 0, len(functionSet))
	for name := range functionSet {
		functions = append(functions, name)
	}
	sort.Strings(functions)
	return functions, nil
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return "unknown"
	}
}
