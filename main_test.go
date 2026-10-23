package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDoor32SocketIsReadForEditorStartup(t *testing.T) {
	path := t.TempDir() + `/door32.sys`
	if err := os.WriteFile(path, []byte("2\r\n9876\r\n9600\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := readDoor32Socket(path)
	if err != nil || handle != 9876 {
		t.Fatalf("readDoor32Socket() = %d, %v; want 9876", handle, err)
	}
}

func TestHeadersPreserveAllFields(t *testing.T) {
	input := "Morgan User\r\n\r\nTesting subject\r\n123\r\nGeneral\r\nYES\r\n"
	header, err := parseHeaders(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := headers{From: "Morgan User", Subject: "Testing subject", MessageNumber: "123", Area: "General", Private: "YES"}
	if header != want {
		t.Fatalf("parsed header = %#v, want %#v", header, want)
	}
}

func TestRenderDisplaysHeaderAndVisibleCursor(t *testing.T) {
	var output bytes.Buffer
	e := &editor{
		conn:   &output,
		header: headers{From: "Morgan", To: "Sysop", Subject: "Test", MessageNumber: "42", Area: "General", Private: "NO"},
		lines:  []string{"abc"},
	}
	if err := e.render(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"From:      Morgan", "To:        Sysop", "Subject:   Test", "Message #: 42", "Area:      General", "Private:   NO", "\x1b[7ma\x1b[0m", "\x1b[6;1H\x1b[?25h"} {
		if !strings.Contains(text, want) {
			t.Errorf("render output missing %q", want)
		}
	}
	if !strings.Contains(text, "\x1b[2;1H") || !strings.Contains(text, "\x1b[3;1H") || !strings.Contains(text, "\x1b[4;1H") {
		t.Fatalf("expected paired headers to occupy adjacent rows; render output was %q", text)
	}
	for index, line := range e.displayHeaderLines() {
		if len(line) != screenCols {
			t.Errorf("header row %d is %d columns, want %d", index+1, len(line), screenCols)
		}
	}
}

func TestHeaderPairTruncatesLongValues(t *testing.T) {
	line := headerPair("From:", strings.Repeat("A", 40), "To:", "B")
	if len(line) != screenCols {
		t.Fatalf("headerPair width = %d, want %d", len(line), screenCols)
	}
	if strings.Contains(line, strings.Repeat("A", 30)) {
		t.Fatal("long header value was not truncated")
	}
}

func TestHeaderPairShowsBlankPositionalField(t *testing.T) {
	line := headerPair("From:", "Alice", "To:", "")
	if !strings.Contains(line, "From:      Alice") || !strings.Contains(line, "To:        (blank)") {
		t.Fatalf("blank header field is not visible: %q", line)
	}
	if len(line) != screenCols {
		t.Fatalf("headerPair width = %d, want %d", len(line), screenCols)
	}
}

func TestDecodeSequence(t *testing.T) {
	tests := []struct {
		sequence []byte
		want     key
	}{
		{[]byte{'[', 'A'}, keyUp},
		{[]byte{'[', '1', '~'}, keyHome},
		{[]byte{'[', '3', '~'}, keyDelete},
		{[]byte{'[', '1', '2', '~'}, keyF2},
		{[]byte{'O', 'R'}, keyF3},
		{[]byte{'O', 'A'}, keyUp},
		{[]byte{'O', 'B'}, keyDown},
		{[]byte{'O', 'C'}, keyRight},
		{[]byte{'O', 'D'}, keyLeft},
		{[]byte{'[', '1', ';', '5', 'A'}, keyUp},
	}
	for _, test := range tests {
		if got := decodeSequence(test.sequence); got != test.want {
			t.Errorf("decodeSequence(%q) = %v, want %v", test.sequence, got, test.want)
		}
	}
}

func TestEditorSplitAndJoinLines(t *testing.T) {
	e := &editor{lines: []string{"hello world"}, x: 5}
	if _, err := e.handle(keyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.lines, []string{"hello", " world"}) {
		t.Fatalf("after split: %#v", e.lines)
	}
	if _, err := e.handle(keyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.lines, []string{"hello world"}) || e.x != 5 {
		t.Fatalf("after join: lines=%#v x=%d", e.lines, e.x)
	}
}

func TestEditorInsertAndOvertype(t *testing.T) {
	e := &editor{lines: []string{"cat"}, x: 1}
	e.handle(keyOther, 'a')
	if e.lines[0] != "caat" {
		t.Fatalf("insert mode produced %q", e.lines[0])
	}
	e.overtype = true
	e.handle(keyOther, 'r')
	if e.lines[0] != "cart" {
		t.Fatalf("overtype mode produced %q", e.lines[0])
	}
}

func TestWriteBodyRoundTrip(t *testing.T) {
	path := t.TempDir() + `/msgtmp`
	want := []string{"first", "second"}
	if err := writeBody(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := readBody(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip got %#v, want %#v", got, want)
	}
}

func TestKeyReaderArrowSequences(t *testing.T) {
	reader := &keyReader{input: make(chan inputEvent, 6)}
	for _, value := range []byte{0x1b, '[', 'A', 0x1b, 'O', 'D'} {
		reader.input <- inputEvent{b: value}
	}
	for _, want := range []key{keyUp, keyLeft} {
		got, _, err := reader.readKey()
		if err != nil || got != want {
			t.Fatalf("readKey() = %v, %v; want %v", got, err, want)
		}
	}
}

func TestKeyReaderSkipsTelnetNegotiation(t *testing.T) {
	reader := &keyReader{input: make(chan inputEvent, 7)}
	for _, value := range []byte{0xff, 0xfd, 0x18, 0xff, 0xfb, 0x01, 'A'} {
		reader.input <- inputEvent{b: value}
	}
	got, value, err := reader.readKey()
	if err != nil || got != keyOther || value != 'A' {
		t.Fatalf("readKey() = %v, %q, %v; want literal A after Telnet negotiation", got, value, err)
	}
}

func TestTelnetNegotiationReplies(t *testing.T) {
	for _, test := range []struct {
		command byte
		want    []byte
	}{
		{command: 251, want: []byte{0xff, 254, 0x18}},
		{command: 253, want: []byte{0xff, 252, 0x18}},
		{command: 252},
		{command: 254},
	} {
		var output bytes.Buffer
		reader := &keyReader{input: make(chan inputEvent, 4), writer: &output}
		for _, value := range []byte{0xff, test.command, 0x18, 'Z'} {
			reader.input <- inputEvent{b: value}
		}
		got, value, err := reader.readKey()
		if err != nil || got != keyOther || value != 'Z' {
			t.Fatalf("command %d readKey() = %v, %q, %v", test.command, got, value, err)
		}
		if !bytes.Equal(output.Bytes(), test.want) {
			t.Errorf("command %d response = %v, want %v", test.command, output.Bytes(), test.want)
		}
	}
}

func TestEditorDeleteBackspaceAndDeleteLine(t *testing.T) {
	e := &editor{lines: []string{"abcd"}, x: 1}
	e.handle(keyDelete, 0)
	if e.lines[0] != "acd" {
		t.Fatalf("Delete produced %q", e.lines[0])
	}
	e.handle(keyBackspace, 0)
	if e.lines[0] != "cd" || e.x != 0 {
		t.Fatalf("Backspace produced %q at column %d", e.lines[0], e.x)
	}
	e.lines = []string{"first", "second"}
	e.y, e.x = 0, 2
	e.handle(keyDeleteLine, 0)
	if !reflect.DeepEqual(e.lines, []string{"second"}) || e.y != 0 {
		t.Fatalf("Ctrl-Y produced %#v at line %d", e.lines, e.y)
	}
}

func TestSlashCommands(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := writeBody(quotePath, []string{"> old message"}); err != nil {
		t.Fatal(err)
	}
	if err := writeBody(bodyFile, []string{"unchanged"}); err != nil {
		t.Fatal(err)
	}

	quote := &editor{lines: []string{"/q"}, cfg: defaultConfig()}
	if quit, err := quote.handle(keyEnter, 0); err != nil || quit {
		t.Fatalf("/q result = quit %v, err %v", quit, err)
	}
	if !reflect.DeepEqual(quote.lines, []string{" old message"}) {
		t.Fatalf("/q produced %#v", quote.lines)
	}

	save := &editor{lines: []string{"saved body"}}
	if quit, err := save.handle(keyEnter, 0); err != nil || quit {
		t.Fatalf("ordinary Enter result = quit %v, err %v", quit, err)
	}
	save.lines = []string{"/s", "saved body"}
	save.y = 0
	if quit, err := save.handle(keyEnter, 0); err != nil || !quit {
		t.Fatalf("/s result = quit %v, err %v", quit, err)
	}
	got, err := readBody(bodyFile)
	if err != nil || !reflect.DeepEqual(got, []string{"saved body"}) {
		t.Fatalf("/s saved %#v, err %v", got, err)
	}

	abort := &editor{lines: []string{"/a"}}
	if quit, err := abort.handle(keyEnter, 0); err != nil || !quit {
		t.Fatalf("/a result = quit %v, err %v", quit, err)
	}
}
