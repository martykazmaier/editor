package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDoor32SocketIsReadForEditorStartup(t *testing.T) {
	path := t.TempDir() + `/door32.sys`
	contents := "2\r\n9876\r\n9600\r\nEleBBS\r\n12\r\nAda Lovelace\r\nAda\r\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	handle, userName, err := readDoor32(path)
	if err != nil || handle != 9876 || userName != "Ada Lovelace" {
		t.Fatalf("readDoor32() = %d, %q, %v; want 9876, Ada Lovelace", handle, userName, err)
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

func TestParseConfigQuotePreferencesAndSignature(t *testing.T) {
	cfg, err := parseConfig(strings.NewReader("quote_prefix=|\nquote_strip=no\nquote_all=yes\nsig_text=Regards,\\nMorgan\nsig_append=yes\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Quote.Prefix != "|" || cfg.Quote.Strip || !cfg.Quote.QuoteAll {
		t.Fatalf("quote config = %#v", cfg.Quote)
	}
	if cfg.Signature.Text != "Regards,\nMorgan" || !cfg.Signature.Append {
		t.Fatalf("signature config = %#v", cfg.Signature)
	}
	if got := cfg.appendSignature([]string{"message"}); !reflect.DeepEqual(got, []string{"message", "Regards,", "Morgan"}) {
		t.Fatalf("appendSignature() = %#v", got)
	}
}

func TestPreferencesFileSitsBesideExecutable(t *testing.T) {
	path, err := preferencesFile()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if filepath.Clean(path) != filepath.Join(filepath.Dir(exe), "eleedit.ini") {
		t.Fatalf("preferencesFile() = %q", path)
	}
}

func TestOnePreferencesFileKeepsEachUser(t *testing.T) {
	path := t.TempDir() + "/eleedit.ini"
	ada := config{
		Quote:     quoteConfig{Prefix: "|", Strip: false, QuoteAll: true},
		Signature: signatureConfig{Text: "Regards,\nAda\\n", Append: true},
	}
	grace := defaultConfig()
	grace.Quote.Prefix = ">"
	if err := saveUserConfig(path, "Ada Lovelace", ada); err != nil {
		t.Fatal(err)
	}
	if err := saveUserConfig(path, "Grace Hopper", grace); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUserConfig(path, " ada lovelace ")
	if err != nil || loaded != ada {
		t.Fatalf("Ada config = %#v, %v", loaded, err)
	}
	loaded, err = loadUserConfig(path, "Grace Hopper")
	if err != nil || loaded != grace {
		t.Fatalf("Grace config = %#v, %v", loaded, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
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
	for _, want := range []string{"From:      Morgan", "To:        Sysop", "Subject:   Test", "Message #: 42", "Area:      General", "Private:   NO", "\x1b[7ma\x1b[27m", "\x1b[6;1H\x1b[?25h", "/s Save  /q Quote  /p Preferences  /a Abort  Ctrl-Y Delete line"} {
		if !strings.Contains(text, want) {
			t.Errorf("render output missing %q", want)
		}
	}
	if strings.Contains(text, "F2 Save") || strings.Contains(text, "F3 Quote") {
		t.Error("render output advertises function keys")
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

func TestPreferencesCommandSavesPerUserConfig(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	var output bytes.Buffer
	input := []byte("!\rno\ryes\rRegards,\rAda\x13yes\r")
	reader := &keyReader{input: make(chan inputEvent, len(input)), writer: &output}
	for _, value := range input {
		reader.input <- inputEvent{b: value}
	}
	e := &editor{
		conn:       &output,
		reader:     reader,
		header:     headers{From: "Ada Lovelace"},
		userName:   "Ada Lovelace",
		lines:      []string{"/p", "message"},
		cfg:        defaultConfig(),
		configPath: "eleedit.ini",
	}
	if quit, err := e.handle(keyEnter, 0); err != nil || quit {
		t.Fatalf("/p result = quit %v, err %v", quit, err)
	}
	if e.cfg.Quote.Prefix != "!" || e.cfg.Quote.Strip || !e.cfg.Quote.QuoteAll {
		t.Fatalf("updated quote preferences = %#v", e.cfg.Quote)
	}
	if e.cfg.Signature.Text != "Regards,\nAda" || !e.cfg.Signature.Append {
		t.Fatalf("updated signature = %#v", e.cfg.Signature)
	}
	loaded, err := loadUserConfig(e.configPath, "Ada Lovelace")
	if err != nil || loaded != e.cfg {
		t.Fatalf("persisted config = %#v, %v; want %#v", loaded, err, e.cfg)
	}
	if _, err := os.Stat("eleedit-ada lovelace.ini"); !os.IsNotExist(err) {
		t.Fatalf("preferences were written to a per-user file; stat error = %v", err)
	}
	if !reflect.DeepEqual(e.lines, []string{"message"}) {
		t.Fatalf("preferences command left lines %#v", e.lines)
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
		{[]byte{'[', 'K'}, keyEnd},
		{[]byte{'[', '1', '~'}, keyHome},
		{[]byte{'[', '4', '~'}, keyEnd},
		{[]byte{'[', '8', '~'}, keyEnd},
		{[]byte{'[', '4', ';', '1', '~'}, keyEnd},
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

func TestEditorEndAndDownPastLastLine(t *testing.T) {
	e := &editor{lines: []string{"last line"}}
	e.handle(keyEnd, 0)
	if e.x != len(e.lines[0]) {
		t.Fatalf("End cursor column = %d, want %d", e.x, len(e.lines[0]))
	}
	e.handle(keyDown, 0)
	if !reflect.DeepEqual(e.lines, []string{"last line", ""}) || e.y != 1 || e.x != 0 {
		t.Fatalf("Down past last line: lines=%#v cursor=(%d, %d)", e.lines, e.y, e.x)
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

func TestEditorWrapsAtWordBoundary(t *testing.T) {
	first := strings.Repeat("a", screenCols-6) + " " + "word"
	e := &editor{lines: []string{first}, x: len(first)}
	e.handle(keyOther, 's')
	if !reflect.DeepEqual(e.lines, []string{strings.Repeat("a", screenCols-6), "words"}) {
		t.Fatalf("wrapped lines = %#v", e.lines)
	}
	if e.y != 1 || e.x != 5 {
		t.Fatalf("wrapped cursor = (%d, %d), want (1, 5)", e.y, e.x)
	}
}

func TestEditorHardWrapsLongWords(t *testing.T) {
	e := &editor{lines: []string{strings.Repeat("a", screenCols-1)}, x: screenCols - 1}
	e.handle(keyOther, 'b')
	if !reflect.DeepEqual(e.lines, []string{strings.Repeat("a", screenCols-1), "b"}) {
		t.Fatalf("hard-wrapped lines = %#v", e.lines)
	}
	if e.y != 1 || e.x != 1 {
		t.Fatalf("wrapped cursor = (%d, %d), want (1, 1)", e.y, e.x)
	}
}

func TestEditorDoesNotLoseTextWhenWrapLimitIsReached(t *testing.T) {
	lines := make([]string, maxLines)
	lines[maxLines-1] = strings.Repeat("a", screenCols-1)
	e := &editor{lines: lines, y: maxLines - 1, x: screenCols - 1}
	e.handle(keyOther, 'b')
	if e.lines[maxLines-1] != strings.Repeat("a", screenCols-1) {
		t.Fatalf("line at limit changed to %q", e.lines[maxLines-1])
	}
	if e.status != "Maximum line count reached" {
		t.Fatalf("status = %q, want maximum line count warning", e.status)
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

func TestKeyReaderBackspaceAndDelete(t *testing.T) {
	reader := &keyReader{input: make(chan inputEvent, 2)}
	reader.input <- inputEvent{b: 0x08}
	reader.input <- inputEvent{b: 0x7f}
	got, _, err := reader.readKey()
	if err != nil || got != keyBackspace {
		t.Fatalf("readKey(BS) = %v, %v; want Backspace", got, err)
	}
	got, _, err = reader.readKey()
	if err != nil || got != keyDelete {
		t.Fatalf("readKey(DEL) = %v, %v; want Delete", got, err)
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

func TestKeyReaderEndSequence(t *testing.T) {
	for _, sequence := range [][]byte{{0x1b, '[', '4', ';', '1', '~'}, {0x1b, '[', 'K'}} {
		reader := &keyReader{input: make(chan inputEvent, len(sequence))}
		for _, value := range sequence {
			reader.input <- inputEvent{b: value}
		}
		got, _, err := reader.readKey()
		if err != nil || got != keyEnd {
			t.Errorf("readKey(%q) = %v, %v; want End", sequence, got, err)
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

	e.lines = []string{"a", "b", "c", "d"}
	e.y, e.x = 1, 0
	e.handle(keyDeleteLine, 0)
	if !reflect.DeepEqual(e.lines, []string{"a", "c", "d"}) || e.y != 1 {
		t.Fatalf("Ctrl-Y in the middle produced %#v at line %d", e.lines, e.y)
	}

	e.lines = []string{"keep", "drop"}
	e.y, e.x = 1, 2
	e.handle(keyDeleteLine, 0)
	if !reflect.DeepEqual(e.lines, []string{"keep", ""}) || e.y != 1 || e.x != 0 {
		t.Fatalf("Ctrl-Y on the last line produced %#v at line %d col %d", e.lines, e.y, e.x)
	}
}

func TestSlashCommandsRunWhenTyped(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := writeBody(bodyFile, []string{"unchanged"}); err != nil {
		t.Fatal(err)
	}

	save := &editor{conn: &bytes.Buffer{}, reader: keyReaderWith('n'), lines: []string{"", "kept"}}
	if quit, err := save.handle(keyOther, '/'); err != nil || quit {
		t.Fatalf("typing / = quit %v, err %v", quit, err)
	}
	if quit, err := save.handle(keyOther, 's'); err != nil || quit {
		t.Fatalf("declined typed /s = quit %v, err %v", quit, err)
	}
	if save.lines[0] != "" || save.x != 0 {
		t.Fatalf("declined typed /s left %#v at column %d", save.lines, save.x)
	}
	if got, err := readBody(bodyFile); err != nil || !reflect.DeepEqual(got, []string{"unchanged"}) {
		t.Fatalf("declined typed /s saved %#v, err %v", got, err)
	}

	save.reader = keyReaderWith('Y')
	if quit, err := save.handle(keyOther, '/'); err != nil || quit {
		t.Fatalf("typing / again = quit %v, err %v", quit, err)
	}
	if quit, err := save.handle(keyOther, 's'); err != nil || !quit {
		t.Fatalf("typed /s = quit %v, err %v", quit, err)
	}
	if got, err := readBody(bodyFile); err != nil || !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("typed /s saved %#v, err %v", got, err)
	}

	abort := &editor{conn: &bytes.Buffer{}, reader: keyReaderWith('y'), lines: []string{"hello"}}
	if _, err := abort.handle(keyOther, '/'); err != nil {
		t.Fatal(err)
	}
	if quit, err := abort.handle(keyOther, 'a'); err != nil || !quit {
		t.Fatalf("typed /a = quit %v, err %v", quit, err)
	}
	if _, err := os.Stat(bodyFile); !os.IsNotExist(err) {
		t.Fatalf("typed /a left msgtmp present, stat err %v", err)
	}

	plain := &editor{lines: []string{"hello"}, x: len("hello")}
	if quit, err := plain.handle(keyOther, '/'); err != nil || quit {
		t.Fatalf("slash in the line = quit %v, err %v", quit, err)
	}
	if quit, err := plain.handle(keyOther, 's'); err != nil || quit || plain.lines[0] != "hello/s" {
		t.Fatalf("slash in the line produced quit %v %#v", quit, plain.lines)
	}
}

func keyReaderWith(value byte) *keyReader {
	reader := &keyReader{input: make(chan inputEvent, 1)}
	reader.input <- inputEvent{b: value}
	return reader
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
	save.conn = &bytes.Buffer{}
	save.reader = keyReaderWith('n')
	if quit, err := save.handle(keyEnter, 0); err != nil || quit {
		t.Fatalf("/s declined result = quit %v, err %v", quit, err)
	}
	if got, err := readBody(bodyFile); err != nil || !reflect.DeepEqual(got, []string{"unchanged"}) {
		t.Fatalf("/s declined saved %#v, err %v", got, err)
	}
	save.reader = keyReaderWith('Y')
	if quit, err := save.handle(keyEnter, 0); err != nil || !quit {
		t.Fatalf("/s result = quit %v, err %v", quit, err)
	}
	got, err := readBody(bodyFile)
	if err != nil || !reflect.DeepEqual(got, []string{"saved body"}) {
		t.Fatalf("/s saved %#v, err %v", got, err)
	}

	prefixed := &editor{
		conn:   &bytes.Buffer{},
		reader: keyReaderWith('y'),
		lines:  []string{"/s Keep this", "next"},
	}
	if quit, err := prefixed.handle(keyEnter, 0); err != nil || !quit {
		t.Fatalf("/s prefix result = quit %v, err %v", quit, err)
	}
	if got, err := readBody(bodyFile); err != nil || !reflect.DeepEqual(got, []string{"Keep this", "next"}) {
		t.Fatalf("/s prefix saved %#v, err %v", got, err)
	}

	abort := &editor{lines: []string{"changed body"}}
	if err := abort.save(); err != nil {
		t.Fatal(err)
	}
	abort.lines = []string{"/a"}
	abort.conn = &bytes.Buffer{}
	abort.reader = keyReaderWith('n')
	if quit, err := abort.handle(keyEnter, 0); err != nil || quit {
		t.Fatalf("/a declined result = quit %v, err %v", quit, err)
	}
	if _, err := os.Stat(bodyFile); err != nil {
		t.Fatalf("/a declined removed msgtmp, stat err %v", err)
	}
	abort.reader = keyReaderWith('y')
	if quit, err := abort.handle(keyEnter, 0); err != nil || !quit {
		t.Fatalf("/a result = quit %v, err %v", quit, err)
	}
	if _, err := os.Stat(bodyFile); !os.IsNotExist(err) {
		t.Fatalf("/a left msgtmp present, stat err %v", err)
	}

	escape := &editor{
		conn:   &bytes.Buffer{},
		lines:  []string{"saved during session"},
		reader: &keyReader{input: make(chan inputEvent, 3)},
	}
	if err := escape.save(); err != nil {
		t.Fatal(err)
	}
	escape.lines = []string{"unsaved change"}
	for _, value := range []byte{0x1b, 'N', 'X'} {
		escape.reader.input <- inputEvent{b: value}
	}
	action, choice, err := escape.reader.readKey()
	if err != nil || action != keyEscape {
		t.Fatalf("Escape input = %v, %v; want Escape", action, err)
	}
	if quit, err := escape.handle(action, choice); err != nil || !quit {
		t.Fatalf("Escape then N result = quit %v, err %v", quit, err)
	}
	if _, err := os.Stat(bodyFile); !os.IsNotExist(err) {
		t.Fatalf("Escape-N left msgtmp present, stat err %v", err)
	}
}

func TestPipeColorIsShownAndKeptOutOfTheColumnCount(t *testing.T) {
	rendered, attr := renderBodyLine("|0FHi", -1, 7)
	if attr != 0x0F || strings.Contains(rendered, "|0F") || !strings.Contains(rendered, "Hi") {
		t.Fatalf("renderBodyLine() = %q, attr %#x", rendered, attr)
	}
	if !strings.Contains(rendered, "\x1b[0;97;40m") {
		t.Fatalf("bright white sequence missing in %q", rendered)
	}
	if visualColumn("|0FHi", len("|0FHi")) != 2 {
		t.Fatalf("visual width = %d", visualColumn("|0FHi", len("|0FHi")))
	}
	colored := "|0A" + strings.Repeat("a", 5) + "|1Eb"
	if byteIndexAtColumn(colored, 5) != strings.Index(colored, "b") {
		t.Fatalf("column of b = %d", byteIndexAtColumn(colored, 5))
	}
	plain, _ := renderBodyLine("|0", -1, 7)
	if !strings.Contains(plain, "|0") {
		t.Fatalf("incomplete code was hidden: %q", plain)
	}
}

func TestColorCodesDoNotShortenTheVisibleLine(t *testing.T) {
	line := "|0F" + strings.Repeat("a", screenCols)
	e := &editor{lines: []string{line}, x: len(line)}
	e.wrapCurrentLine([]byte(line))
	if e.lines[0] != "|0F"+strings.Repeat("a", screenCols-1) || e.lines[1] != "a" {
		t.Fatalf("wrapped colored line = %#v", e.lines)
	}
}
