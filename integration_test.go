// Copyright 2026 Kevin McDonald
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package protoscope_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf16"
)

func TestBOMHandling(t *testing.T) {
	// Build the protoscope binary
	tmpDir := t.TempDir()
	protoscopeBin := filepath.Join(tmpDir, "protoscope")
	if runtime.GOOS == "windows" {
		protoscopeBin += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", protoscopeBin, "./cmd/protoscope")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build protoscope: %v", err)
	}

	tests := []struct {
		name     string
		encoding string // "utf8", "utf8bom", "utf16le", "utf16be"
		input    string
	}{
		{"UTF8", "utf8", `1: {"John Doe"}`},
		{"UTF8BOM", "utf8bom", `1: {"John Doe"}`},
		{"UTF16LE", "utf16le", `1: {"John Doe"}`},
		{"UTF16BE", "utf16be", `1: {"John Doe"}`},
	}

	expectedOutput := []byte{0x0a, 0x08, 0x4a, 0x6f, 0x68, 0x6e, 0x20, 0x44, 0x6f, 0x65}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var inputBytes []byte
			switch tc.encoding {
			case "utf8":
				inputBytes = []byte(tc.input)
			case "utf8bom":
				inputBytes = append([]byte{0xef, 0xbb, 0xbf}, []byte(tc.input)...)
			case "utf16le":
				inputBytes = append([]byte{0xff, 0xfe}, encodeUTF16(tc.input, binary.LittleEndian)...)
			case "utf16be":
				inputBytes = append([]byte{0xfe, 0xff}, encodeUTF16(tc.input, binary.BigEndian)...)
			}

			inputPath := filepath.Join(tmpDir, tc.name+".txt")
			if err := os.WriteFile(inputPath, inputBytes, 0644); err != nil {
				t.Fatalf("failed to write input file: %v", err)
			}

			// Run protoscope -s <inputPath>
			cmd := exec.Command(protoscopeBin, "-s", inputPath)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				t.Fatalf("protoscope failed: %v\nstderr: %s", err, stderr.String())
			}

			if !bytes.Equal(stdout.Bytes(), expectedOutput) {
				t.Errorf("expected %x, got %x", expectedOutput, stdout.Bytes())
			}
		})
	}
}

func encodeUTF16(s string, order binary.ByteOrder) []byte {
	u16 := utf16.Encode([]rune(s))
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		order.PutUint16(b[i*2:], v)
	}
	return b
}
