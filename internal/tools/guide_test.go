package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// S12: the usage guide is the product's documentation for an LLM, and an LLM
// cannot tell a stale guide from a correct one. This test is the drift guard
// between internal/tools/llm_guide.md and the tool registry. It checks, exactly:
//
//  1. every registered tool appears in the guide as a `backticked` name;
//  2. the guide's "Tool reference" table lists exactly the registered tools — a
//     tool the registry does not expose is a fictional tool, and one missing
//     from the table is undocumented;
//  3. every parameter the guide documents for a tool exists in that tool's input
//     schema (no fictional parameter);
//  4. every parameter a tool's schema declares is documented in the guide (no
//     silently added or dropped parameter);
//  5. every error code the guide's table documents is a code this package or
//     internal/types really emits, and the guide documents the whole set.
//
// The guide is parsed, not compared to a golden copy: a golden file would have to
// be rewritten for every honest documentation improvement, which is how a guard
// stops being read.

// toolReferenceHeading is the section holding the machine-checkable table.
const toolReferenceHeading = "## Tool reference"

// errorTableHeading is the section holding the code → action table.
const errorTableHeading = "## Errors and what to do"

// documentedCodes are the codes the query layer can return, as literals, because
// the guide documents strings and a caller implements against strings.
var documentedCodes = []string{
	"NO_STORE",
	"SPEC_NOT_FOUND",
	"SPEC_AMBIGUOUS",
	"DIFF_NEEDS_SPECS",
	"INVALID_QUERY",
	"STORE_ERROR",
}

// backticked matches one markdown code span's content.
var backticked = regexp.MustCompile("`([^`]+)`")

// sectionLines returns the lines of the markdown section that starts with
// heading, up to the next "## " heading.
func sectionLines(t *testing.T, guide, heading string) []string {
	t.Helper()
	lines := strings.Split(guide, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != heading {
			continue
		}
		var out []string
		for _, rest := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(rest), "## ") {
				break
			}
			out = append(out, rest)
		}
		return out
	}
	t.Fatalf("the guide has no %q section, so its tool and parameter reference cannot be checked", heading)
	return nil
}

// tableRows returns the body rows of the first markdown table in lines, each row
// as its trimmed cells. The header and the |---|---| separator are dropped.
func tableRows(t *testing.T, lines []string) [][]string {
	t.Helper()
	var rows [][]string
	seenTable := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		seenTable = true
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if isSeparator(cells) {
			continue
		}
		rows = append(rows, cells)
	}
	if !seenTable {
		t.Fatalf("the section holds no table, so nothing is machine-checkable")
	}
	if len(rows) < 2 {
		t.Fatalf("the table has %d rows: it needs a header and at least one entry", len(rows))
	}
	return rows[1:] // drop the header
}

// isSeparator reports whether every cell is a markdown separator (---).
func isSeparator(cells []string) bool {
	for _, cell := range cells {
		if strings.Trim(cell, ":- ") != "" {
			return false
		}
	}
	return true
}

// codeSpans returns the content of every markdown code span of a table cell.
func codeSpans(cell string) []string {
	var out []string
	for _, match := range backticked.FindAllStringSubmatch(cell, -1) {
		out = append(out, strings.TrimSpace(match[1]))
	}
	return out
}

// registeredTools maps every tool name the registry exposes to its declared
// parameter names.
func registeredTools(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, def := range (&Registry{}).definitions() {
		params := make([]string, 0, len(def.tool.InputSchema.Properties))
		for name := range def.tool.InputSchema.Properties {
			params = append(params, name)
		}
		sort.Strings(params)
		out[def.tool.Name] = params
	}
	if len(out) == 0 {
		t.Fatal("the registry exposes no tools")
	}
	return out
}

// documentedTools reads the guide's tool reference table into
// tool name → documented parameters.
func documentedTools(t *testing.T, guide string) map[string][]string {
	t.Helper()
	rows := tableRows(t, sectionLines(t, guide, toolReferenceHeading))
	out := map[string][]string{}
	for _, row := range rows {
		if len(row) < 3 {
			t.Fatalf("tool reference row %v has %d cells, want 3 (tool, parameters, purpose)", row, len(row))
		}
		names := codeSpans(row[0])
		if len(names) != 1 {
			t.Fatalf("tool reference row %v must name exactly one tool in backticks, found %v", row, names)
		}
		name := names[0]
		if _, dup := out[name]; dup {
			t.Fatalf("the tool reference documents %q twice", name)
		}
		params := codeSpans(row[1])
		if row[1] != "—" && len(params) == 0 {
			t.Fatalf("tool reference row for %q lists %q as parameters and no code spans", name, row[1])
		}
		out[name] = params
	}
	return out
}

// TestGuideMatchesToolInventory is the S12 drift guard: it fails when the guide
// and the registry disagree about which tools exist or which parameters they take.
func TestGuideMatchesToolInventory(t *testing.T) {
	registered := registeredTools(t)
	documented := documentedTools(t, llmGuide)

	// 1. Every registered tool is mentioned by name somewhere in the guide.
	for name := range registered {
		if !strings.Contains(llmGuide, "`"+name+"`") {
			t.Errorf("the guide never mentions the registered tool %q", name)
		}
	}

	// 2. The reference table lists exactly the registered tools.
	for name := range registered {
		if _, ok := documented[name]; !ok {
			t.Errorf("the tool reference table does not document the registered tool %q", name)
		}
	}
	for name := range documented {
		if _, ok := registered[name]; !ok {
			t.Errorf("the tool reference table documents %q, which the registry does not expose", name)
		}
	}

	// 3 and 4. Parameters agree in both directions.
	for name, want := range registered {
		got, ok := documented[name]
		if !ok {
			continue
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("parameters of %q: the guide documents [%s], the schema declares [%s]",
				name, strings.Join(got, ", "), strings.Join(want, ", "))
		}
	}

	// 5. The error table documents exactly the codes the query layer emits.
	rows := tableRows(t, sectionLines(t, llmGuide, errorTableHeading))
	documentedSet := map[string]bool{}
	for _, row := range rows {
		if len(row) < 3 {
			t.Errorf("error row %v has %d cells, want 3 (code, meaning, action)", row, len(row))
			continue
		}
		codes := codeSpans(row[0])
		if len(codes) != 1 {
			t.Errorf("error row %v must name exactly one code in backticks, found %v", row, codes)
			continue
		}
		documentedSet[codes[0]] = true
		if strings.TrimSpace(row[2]) == "" {
			t.Errorf("error code %q has no action", codes[0])
		}
	}
	for _, code := range documentedCodes {
		if !documentedSet[code] {
			t.Errorf("the error table does not document %q", code)
		}
		if !codeAppearsInSources(t, code) {
			t.Errorf("the error table documents %q, which no source emits", code)
		}
	}
	for code := range documentedSet {
		if !containsString(documentedCodes, code) {
			t.Errorf("the error table documents %q, which is not part of the documented code set", code)
		}
	}
}

// TestGuideTeachesTheWorkflow covers the S12 content requirements a table cannot:
// the first step, the addressing rules, the exploration path and the token rules.
func TestGuideTeachesTheWorkflow(t *testing.T) {
	for _, want := range []string{
		"swagger-mcp index", // the index command, the only step that needs a terminal
		"all_specs",         // the sweep escape hatch
		"spec_status",       // staleness
		"resolve_depth",     // the token dial of the detail tools
		"limit",             // paging instead of dumping
		"offset",
		"$ref(", // shared-schema notation
	} {
		if !strings.Contains(llmGuide, want) {
			t.Errorf("the guide does not mention %q", want)
		}
	}
}

// codeAppearsInSources reports whether the literal code is emitted by this
// package or by internal/types, which owns the request-level codes.
func codeAppearsInSources(t *testing.T, code string) bool {
	t.Helper()
	quoted := `"` + code + `"`
	for _, dir := range []string{".", filepath.Join("..", "types")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if strings.Contains(string(raw), quoted) {
				return true
			}
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
