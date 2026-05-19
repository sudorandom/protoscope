// Copyright 2022 Google LLC
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

package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"

	_ "embed"

	descpb "google.golang.org/protobuf/types/descriptorpb"

	"github.com/protocolbuffers/protoscope"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	outPath  = flag.String("o", "", "output file to use (defaults to stdout)")
	assemble = flag.Bool("s", false, "whether to treat the input as a Protoscope source file")
	spec     = flag.Bool("spec", false, "opens the Protoscope spec in $PAGER")

	noQuotedStrings        = flag.Bool("no-quoted-strings", false, "assume no fields in the input proto are strings")
	allFieldsAreMessages   = flag.Bool("all-fields-are-messages", false, "try really hard to disassemble all fields as messages")
	explicitWireTypes      = flag.Bool("explicit-wire-types", false, "include an explicit wire type for every field")
	noGroups               = flag.Bool("no-groups", false, "do not try to disassemble groups")
	explicitLengthPrefixes = flag.Bool("explicit-length-prefixes", false, "emit literal length prefixes instead of braces")
	varintDelimited        = flag.Bool("varint-delimited", false, "whether to treat the input as a varint-delimited stream of messages")

	descriptorSet = flag.String("descriptor-set", "", "path to a file containing an encoded FileDescriptorSet, for aiding disassembly")
	messageType   = flag.String("message-type", "", "full name of a type in the FileDescriptorSet given by -descriptor-set;\n"+
		"the decoder will assume that the input file is an encoded binary proto\n"+
		"of this type for the purposes of providing better output")
	printFieldNames = flag.Bool("print-field-names", false, "prints out field names, if using -message-type")
	printEnumNames  = flag.Bool("print-enum-names", false, "prints out enum value names, if using -message-type")
)

func main() {
	if err := Main(); err != nil {
		fmt.Fprintln(os.Stderr, "protoscope:", err)
		os.Exit(1)
	}
}

func Main() error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [-s] [OPTION...] [INPUT]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Assemble a Protoscope file to binary, or inspect binary data as Protoscope text.\n")
		fmt.Fprintf(os.Stderr, "Run with -spec to learn more about the Protoscope language.\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *spec {
		pager := os.Getenv("PAGER")
		if pager == "" {
			return fmt.Errorf("%s", protoscope.LanguageTxt)
		}

		cmd := exec.Command(pager)
		cmd.Stdout = os.Stdout
		cmd.Stdin = strings.NewReader(protoscope.LanguageTxt)
		if err := cmd.Run(); err != nil {
			return err
		}
		return nil
	}

	var schema protoreflect.MessageDescriptor
	if *descriptorSet != "" || *messageType != "" {
		if *assemble {
			return errors.New("-message-type and -descriptor-set cannot be mixed with -s")
		}
		if *descriptorSet == "" {
			return errors.New("-message-type without -descriptor-set")
		}
		if *messageType == "" {
			return errors.New("-descriptor-set without -message-type")
		}

		descBytes, err := os.ReadFile(*descriptorSet)
		if err != nil {
			return err
		}

		var fds descpb.FileDescriptorSet
		if err := proto.Unmarshal(descBytes, &fds); err != nil {
			return err
		}

		files, err := protodesc.NewFiles(&fds)
		if err != nil {
			return err
		}

		desc, err := files.FindDescriptorByName(protoreflect.FullName(*messageType))
		if err != nil {
			return err
		}

		if msgDesc, ok := desc.(protoreflect.MessageDescriptor); ok {
			schema = msgDesc
		} else {
			return fmt.Errorf("not a message type: %s", *messageType)
		}
	}

	inPath := ""
	inFile := os.Stdin
	if flag.NArg() == 1 {
		inPath = flag.Arg(0)
		var err error
		inFile, err = os.Open(inPath)
		if err != nil {
			return err
		}
		defer func() { _ = inFile.Close() }()
	}

	inBytes, err := io.ReadAll(inFile)
	if err != nil {
		return err
	}

	var outBytes []byte
	if *assemble {
		inputText, err := decodeInput(inBytes)
		if err != nil {
			return err
		}
		scanner := protoscope.NewScanner(inputText)
		scanner.SetFile(inPath)
		scanner.Delimited = *varintDelimited

		outBytes, err = scanner.Exec()
		if err != nil {
			return fmt.Errorf("syntax error: %s", err)
		}
	} else {

		outBytes = []byte(protoscope.Write(inBytes, protoscope.WriterOptions{
			NoQuotedStrings:        *noQuotedStrings,
			AllFieldsAreMessages:   *allFieldsAreMessages,
			ExplicitWireTypes:      *explicitWireTypes,
			NoGroups:               *noGroups,
			ExplicitLengthPrefixes: *explicitLengthPrefixes,
			Delimited:              *varintDelimited,

			Schema:          schema,
			PrintFieldNames: *printFieldNames,
			PrintEnumNames:  *printEnumNames,
		}))

	}

	outFile := os.Stdout
	if *outPath != "" {
		var err error
		outFile, err = os.Create(*outPath)
		if err != nil {
			return err
		}
		defer func() { _ = outFile.Close() }()
	}

	_, err = outFile.Write(outBytes)
	return err
}

// decodeInput detects and handles Byte Order Marks (BOM) for UTF-8 and UTF-16.
// This is particularly important for Windows users, as PowerShell redirection
// (e.g., `protoscope person.bin > person.txt`) often produces UTF-16 LE files with a BOM.
func decodeInput(b []byte) (string, error) {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		// Strip UTF-8 BOM.
		return string(b[3:]), nil
	}
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		// Decode UTF-16 Little Endian (common in PowerShell).
		return decodeUTF16(b[2:], binary.LittleEndian)
	}
	if len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff {
		// Decode UTF-16 Big Endian.
		return decodeUTF16(b[2:], binary.BigEndian)
	}
	// Fallback to treating as raw UTF-8.
	return string(b), nil
}

// decodeUTF16 converts UTF-16 bytes to a UTF-8 string using the specified byte order.
func decodeUTF16(b []byte, order binary.ByteOrder) (string, error) {
	if len(b)%2 != 0 {
		return "", errors.New("invalid UTF-16: odd number of bytes")
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = order.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u16)), nil
}
