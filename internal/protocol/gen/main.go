// Command gen generates internal/protocol/registry_generated.go directly
// from spec/command_matrix.csv and spec/pid_matrix.csv. Those CSVs are the
// literal, single source of truth for the PID and command registries — there
// is no separate hand-maintained table to drift out of sync, and any
// unrecognized value in the CSVs fails generation loudly instead of being
// silently accepted.
package main

import (
	"encoding/csv"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type commandRow struct {
	id                  string
	safetyClass         string
	confidence          string
	experimentalDefault bool
	reportID            byte
	request             []byte
	expectedResponse    string
	appliesTo           []uint16
	operationGroup      string
}

type pidRow struct {
	name           string
	pid            uint16
	supportLevel   string
	protocolFamily string
	supportTier    string
}

var validSafetyClasses = map[string]bool{
	"SafeRead": true, "SafeWrite": true, "UnsafeBoot": true, "UnsafeFirmware": true,
}
var validConfidence = map[string]bool{"confirmed": true, "inferred": true}
var validProtocolFamily = map[string]bool{
	"Standard64": true, "JpHandshake": true, "DInput": true, "DS4Boot": true, "Unknown": true,
}
var validSupportLevel = map[string]bool{"full": true, "detect-only": true}
var validSupportTier = map[string]bool{"full": true, "candidate-readonly": true, "detect-only": true}

func main() {
	specDir := flag.String("spec-dir", "../../spec", "directory containing command_matrix.csv and pid_matrix.csv")
	out := flag.String("out", "registry_generated.go", "output Go file path")
	flag.Parse()

	commands, commandOrder, err := readCommandMatrix(filepath.Join(*specDir, "command_matrix.csv"))
	if err != nil {
		fatalf("reading command_matrix.csv: %v", err)
	}
	pids, err := readPidMatrix(filepath.Join(*specDir, "pid_matrix.csv"))
	if err != nil {
		fatalf("reading pid_matrix.csv: %v", err)
	}

	src, err := renderGo(commands, commandOrder, pids)
	if err != nil {
		fatalf("rendering: %v", err)
	}
	formatted, err := format.Source(src)
	if err != nil {
		fatalf("gofmt generated source: %v\n--- source ---\n%s", err, src)
	}
	if err := os.WriteFile(*out, formatted, 0o644); err != nil {
		fatalf("writing %s: %v", *out, err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+format+"\n", a...)
	os.Exit(1)
}

func readCommandMatrix(path string) ([]commandRow, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	col := columnIndex(header)

	var rows []commandRow
	var order []string
	seenIDs := map[string]bool{}
	for {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, err
		}
		id := rec[col["command_id"]]
		if !seenIDs[id] {
			seenIDs[id] = true
			order = append(order, id)
		}

		safetyClass := rec[col["safety_class"]]
		if !validSafetyClasses[safetyClass] {
			return nil, nil, fmt.Errorf("row %s: unrecognized safety_class %q", id, safetyClass)
		}
		confidence := strings.ToLower(rec[col["confidence"]])
		if !validConfidence[confidence] {
			return nil, nil, fmt.Errorf("row %s: unrecognized confidence %q", id, confidence)
		}
		experimental, err := strconv.ParseBool(rec[col["experimental_default"]])
		if err != nil {
			return nil, nil, fmt.Errorf("row %s: bad experimental_default: %v", id, err)
		}
		reportIDU64, err := strconv.ParseUint(strings.TrimPrefix(rec[col["report_id"]], "0x"), 16, 8)
		if err != nil {
			return nil, nil, fmt.Errorf("row %s: bad report_id: %v", id, err)
		}
		request, err := hex.DecodeString(rec[col["request_hex"]])
		if err != nil {
			return nil, nil, fmt.Errorf("row %s: bad request_hex: %v", id, err)
		}
		declaredLen, err := strconv.Atoi(rec[col["request_len"]])
		if err != nil {
			return nil, nil, fmt.Errorf("row %s: bad request_len: %v", id, err)
		}
		if declaredLen != len(request) {
			return nil, nil, fmt.Errorf("row %s: request_len=%d but request_hex decodes to %d bytes", id, declaredLen, len(request))
		}

		var appliesTo []uint16
		rawApplies := rec[col["applies_to"]]
		if rawApplies != "*" && rawApplies != "" {
			for _, part := range strings.Split(rawApplies, ";") {
				pid, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(part), "0x"), 16, 16)
				if err != nil {
					return nil, nil, fmt.Errorf("row %s: bad applies_to entry %q: %v", id, part, err)
				}
				appliesTo = append(appliesTo, uint16(pid))
			}
		}

		rows = append(rows, commandRow{
			id:                  id,
			safetyClass:         safetyClass,
			confidence:          confidence,
			experimentalDefault: experimental,
			reportID:            byte(reportIDU64),
			request:             request,
			expectedResponse:    rec[col["expected_response"]],
			appliesTo:           appliesTo,
			operationGroup:      rec[col["operation_group"]],
		})
	}
	return rows, order, nil
}

func readPidMatrix(path string) ([]pidRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	col := columnIndex(header)

	var rows []pidRow
	seenPIDs := map[uint16]bool{}
	for {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		name := rec[col["pid_name"]]
		pid, err := strconv.ParseUint(rec[col["pid_decimal"]], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("row %s: bad pid_decimal: %v", name, err)
		}
		if seenPIDs[uint16(pid)] {
			return nil, fmt.Errorf("duplicate pid in pid_matrix.csv: %#04x (%s)", pid, name)
		}
		seenPIDs[uint16(pid)] = true

		supportLevel := rec[col["support_level"]]
		if !validSupportLevel[supportLevel] {
			return nil, fmt.Errorf("row %s: unrecognized support_level %q", name, supportLevel)
		}
		family := rec[col["protocol_family"]]
		if !validProtocolFamily[family] {
			return nil, fmt.Errorf("row %s: unrecognized protocol_family %q", name, family)
		}
		tier := rec[col["support_tier"]]
		if !validSupportTier[tier] {
			return nil, fmt.Errorf("row %s: unrecognized support_tier %q", name, tier)
		}

		rows = append(rows, pidRow{
			name:           name,
			pid:            uint16(pid),
			supportLevel:   supportLevel,
			protocolFamily: family,
			supportTier:    tier,
		})
	}
	return rows, nil
}

func columnIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, name := range header {
		idx[name] = i
	}
	return idx
}

func renderGo(commands []commandRow, commandOrder []string, pids []pidRow) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by internal/protocol/gen from spec/command_matrix.csv and\n")
	b.WriteString("// spec/pid_matrix.csv. DO NOT EDIT — edit the spec CSVs and re-run\n")
	b.WriteString("// `go generate ./...` instead.\n\n")
	b.WriteString("package protocol\n\n")

	b.WriteString("// CommandID constants, one per unique command_id in spec/command_matrix.csv,\n")
	b.WriteString("// in first-appearance order.\nconst (\n")
	for _, id := range commandOrder {
		b.WriteString(fmt.Sprintf("\tCommand%s CommandID = %q\n", id, id))
	}
	b.WriteString(")\n\n")

	b.WriteString("// PIDRegistry is generated from spec/pid_matrix.csv.\n")
	b.WriteString("var PIDRegistry = []PidRow{\n")
	for _, p := range pids {
		b.WriteString(fmt.Sprintf(
			"\t{Name: %q, Pid: %#04x, SupportLevel: %q, SupportTier: %q, ProtocolFamily: %q},\n",
			p.name, p.pid, p.supportLevel, p.supportTier, p.protocolFamily,
		))
	}
	b.WriteString("}\n\n")

	b.WriteString("// CommandRegistry is generated from spec/command_matrix.csv.\n")
	b.WriteString("var CommandRegistry = []CommandRow{\n")
	for _, c := range commands {
		appliesTo := "nil"
		if len(c.appliesTo) > 0 {
			parts := make([]string, len(c.appliesTo))
			for i, p := range c.appliesTo {
				parts[i] = fmt.Sprintf("%#04x", p)
			}
			appliesTo = fmt.Sprintf("[]uint16{%s}", strings.Join(parts, ", "))
		}
		b.WriteString(fmt.Sprintf(
			"\t{ID: Command%s, SafetyClass: %q, Confidence: %q, ExperimentalDefault: %v, ReportID: %#02x, Request: %s, ExpectedResponse: %q, AppliesTo: %s, OperationGroup: %q},\n",
			c.id, c.safetyClass, c.confidence, c.experimentalDefault, c.reportID, byteSliceLiteral(c.request), c.expectedResponse, appliesTo, c.operationGroup,
		))
	}
	b.WriteString("}\n")

	return []byte(b.String()), nil
}

func byteSliceLiteral(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("%#02x", b)
	}
	return fmt.Sprintf("[]byte{%s}", strings.Join(parts, ", "))
}
