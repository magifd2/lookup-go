package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testBinaryName is the name of the test binary that will be built.
const testBinaryName = "test_lookup-go"

// TestMain is a special function that Go's testing package runs before any tests.
// It's used here to build the actual 'lookup-go' binary once, so that all
// sub-tests can execute it as a black box.
func TestMain(m *testing.M) {
	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", testBinaryName, ".")
	if err := buildCmd.Run(); err != nil {
		fmt.Printf("Failed to build test binary: %v\n", err)
		os.Exit(1)
	}

	// Run the tests
	exitCode := m.Run()

	// Clean up the binary
	_ = os.Remove(testBinaryName)

	os.Exit(exitCode)
}

// blackBoxTestCase defines a single black-box test case.
	type blackBoxTestCase struct {
		name          string   // Name of the test case
		args          []string // Command-line arguments for the binary
		inputFile     string   // Path to the input file to be piped to stdin
		expectedFile  string   // Path to the file with the expected output
		isJsonL       bool     // Whether the output is expected to be JSON Lines
	}

// TestBlackBox runs all the black-box test cases.
func TestBlackBox(t *testing.T) {
	// Define all test cases
	testCases := []blackBoxTestCase{
		{
			name:         "Exact Match with Field Renaming",
			args:         []string{"-c", "testdata/lookup_config.json", "-m", "user as username OUTPUT department as dept, role"},
			inputFile:    "testdata/input.jsonl",
			expectedFile: "testdata/exact_match.expected.jsonl",
			isJsonL:      true,
		},
		{
			name:         "Wildcard Match with All Fields",
			args:         []string{"-c", "testdata/lookup_config.json", "-m", "hostname as username"},
			inputFile:    "testdata/input.jsonl",
			expectedFile: "testdata/wildcard_match.expected.jsonl",
			isJsonL:      true,
		},
		{
			name:         "Regex Match",
			args:         []string{"-c", "testdata/lookup_config.json", "-m", "process as username"},
			inputFile:    "testdata/input.jsonl",
			expectedFile: "testdata/regex_match.expected.jsonl",
			isJsonL:      true,
		},
		{
			name:         "CIDR Match with JSON Array Input",
			args:         []string{"-c", "testdata/lookup_config.json", "-m", "client_ip as ip_range"},
			inputFile:    "testdata/input_array.json",
			expectedFile: "testdata/cidr_match_array.expected.json",
			isJsonL:      false,
		},
	}

	// Run each test case as a sub-test
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Read the input data
			inputData, err := os.ReadFile(tc.inputFile)
			if err != nil {
				t.Fatalf("Failed to read input file %s: %v", tc.inputFile, err)
			}

			// Execute the command
			cmd := exec.Command("./"+testBinaryName, tc.args...)
			cmd.Stdin = bytes.NewReader(inputData)
			output, err := cmd.CombinedOutput() // CombinedOutput captures both stdout and stderr
			if err != nil {
				t.Fatalf("Command execution failed: %v\nOutput:\n%s", err, string(output))
			}

			// Read the expected output
			expectedOutput, err := os.ReadFile(tc.expectedFile)
			if err != nil {
				t.Fatalf("Failed to read expected output file %s: %v", tc.expectedFile, err)
			}

			// Compare the actual output with the expected output
			if err := compareJSON(output, expectedOutput, tc.isJsonL); err != nil {
				t.Errorf("Output does not match expected result: %v", err)
				t.Logf("EXPECTED:\n%s\n", string(expectedOutput))
				t.Logf("ACTUAL:\n%s\n", string(output))
			}
		})
	}
}

// compareJSON compares two JSON outputs for semantic equality.
// It handles both standard JSON arrays/objects and JSON Lines.
func compareJSON(actual, expected []byte, isJsonL bool) error {
	if isJsonL {
		return compareJSONLines(actual, expected)
	}
	return compareSingleJSON(actual, expected)
}

// compareSingleJSON compares two single JSON objects/arrays.
func compareSingleJSON(actual, expected []byte) error {
	var actualObj, expectedObj interface{}/

	if err := json.Unmarshal(bytes.TrimSpace(actual), &actualObj); err != nil {
		return fmt.Errorf("failed to unmarshal actual output: %w\nOutput: %s", err, string(actual))
	}
	if err := json.Unmarshal(bytes.TrimSpace(expected), &expectedObj); err != nil {
		return fmt.Errorf("failed to unmarshal expected output: %w", err)
	}

	if !reflect.DeepEqual(actualObj, expectedObj) {
		return fmt.Errorf("JSON objects are not equal")
	}
	return nil
}

// compareJSONLines compares two sets of JSON Lines.
func compareJSONLines(actual, expected []byte) error {
	actualLines := strings.Split(strings.TrimSpace(string(actual)), "\n")
	expectedLines := strings.Split(strings.TrimSpace(string(expected)), "\n")

	if len(actualLines) != len(expectedLines) {
		return fmt.Errorf("mismatched line count: got %d, want %d", len(actualLines), len(expectedLines))
	}

	for i := range actualLines {
		if err := compareSingleJSON([]byte(actualLines[i]), []byte(expectedLines[i])); err != nil {
			return fmt.Errorf("mismatch on line %d: %w", i+1, err)
		}
	}

	return nil
}
