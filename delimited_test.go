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

package protoscope

import (
	"bytes"
	"testing"
)

func TestDelimited(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []byte
	}{
		{
			name: "single",
			text: "1: 1",
			want: []byte{0x02, 0x08, 0x01},
		},
		{
			name: "multiple",
			text: "1: 1\n---\n2: 2",
			want: []byte{0x02, 0x08, 0x01, 0x02, 0x10, 0x02},
		},
		{
			name: "empty_first",
			text: "---\n1: 1",
			want: []byte{0x00, 0x02, 0x08, 0x01},
		},
		{
			name: "empty_last",
			text: "1: 1\n---",
			want: []byte{0x02, 0x08, 0x01, 0x00},
		},
		{
			name: "multiple_separators",
			text: "1: 1\n---\n---\n2: 2",
			want: []byte{0x02, 0x08, 0x01, 0x00, 0x02, 0x10, 0x02},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.text)
			s.Delimited = true
			got, err := s.Exec()
			if err != nil {
				t.Fatalf("Exec() error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Exec() got %x, want %x", got, tt.want)
			}

			// Round trip
			gotText := Write(got, WriterOptions{Delimited: true})
			s2 := NewScanner(gotText)
			s2.Delimited = true
			got2, err := s2.Exec()
			if err != nil {
				t.Fatalf("Round trip Exec() error: %v", err)
			}
			if !bytes.Equal(got2, tt.want) {
				t.Errorf("Round trip Exec() got %x, want %x\nText:\n%s", got2, tt.want, gotText)
			}
		})
	}
}

func TestSeparatorError(t *testing.T) {
	tests := []struct {
		name string
		text string
		delimited bool
	}{
		{
			name: "separator_without_flag",
			text: "1: 1\n---\n2: 2",
			delimited: false,
		},
		{
			name: "separator_in_block",
			text: "1: { --- }",
			delimited: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.text)
			s.Delimited = tt.delimited
			_, err := s.Exec()
			if err == nil {
				t.Fatal("Exec() expected error, got nil")
			}
		})
	}
}
