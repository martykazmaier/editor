package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	screenCols = 80
	screenRows = 24
	bodyRows   = 19
	maxLines   = 1000
	quotePath  = "msgquote"
)

type stream interface {
	io.Reader
	io.Writer
}

type editor struct {
	conn     stream
	reader   *keyReader
	header   headers
	lines    []string
	cfg      config
	x        int
	y        int
	top      int
	status   string
	overtype bool
}

type key int

const (
	keyOther key = iota
	keyEscape
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyDelete
	keyPageUp
	keyPageDown
	keyInsert
	keyF2
	keyF3
	keyBackspace
	keyEnter
	keySave
	keyQuit
	keyAbort
	keyDeleteLine
)

type inputEvent struct {
	b   byte
	err error
}

type keyReader struct {
	input  chan inputEvent
	writer io.Writer
}

func newKeyReader(conn stream) *keyReader {
	kr := &keyReader{input: make(chan inputEvent, 256), writer: conn}
	go func() {
		buffer := make([]byte, 128)
		for {
			n, err := conn.Read(buffer)
			for _, b := range buffer[:n] {
				kr.input <- inputEvent{b: b}
			}
			if err != nil {
				kr.input <- inputEvent{err: err}
				return
			}
		}
	}()
	return kr
}

func (r *keyReader) nextByte(wait time.Duration) (byte, error) {
	if wait <= 0 {
		event := <-r.input
		return event.b, event.err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case event := <-r.input:
		return event.b, event.err
	case <-timer.C:
		return 0, errors.New("key sequence timeout")
	}
}

func (r *keyReader) readKey() (key, byte, error) {
	b, err := r.nextDataByte()
	if err != nil {
		return keyOther, 0, err
	}
	switch b {
	case 0x1b:
		first, err := r.nextByte(45 * time.Millisecond)
		if err != nil {
			return keyEscape, 0, nil
		}
		if first != '[' && first != 'O' {
			return keyEscape, first, nil
		}
		sequence := []byte{first}
		for len(sequence) < 12 {
			next, err := r.nextByte(35 * time.Millisecond)
			if err != nil {
				break
			}
			sequence = append(sequence, next)
			if next >= 0x40 && next <= 0x7e {
				break
			}
		}
		return decodeSequence(sequence), 0, nil
	case 0x08, 0x7f:
		return keyBackspace, 0, nil
	case '\r', '\n':
		return keyEnter, 0, nil
	case 0x13:
		return keySave, 0, nil
	case 0x11:
		return keyQuit, 0, nil
	case 0x18:
		return keyAbort, 0, nil
	case 0x19:
		return keyDeleteLine, 0, nil
	default:
		return keyOther, b, nil
	}
}

func (r *keyReader) nextDataByte() (byte, error) {
	for {
		value, err := r.nextByte(0)
		if err != nil || value != 0xff {
			return value, err
		}
		command, err := r.nextByte(0)
		if err != nil {
			return 0, err
		}
		switch command {
		case 0xff:
			return 0xff, nil
		case 251, 252, 253, 254:
			option, err := r.nextByte(0)
			if err != nil {
				return 0, err
			}
			if r.writer != nil && (command == 251 || command == 253) {
				reply := byte(254) // Reject WILL.
				if command == 253 {
					reply = 252 // Reject DO.
				}
				if _, err := r.writer.Write([]byte{0xff, reply, option}); err != nil {
					return 0, err
				}
			}
		case 250:
			for {
				value, err := r.nextByte(0)
				if err != nil {
					return 0, err
				}
				if value == 0xff {
					end, err := r.nextByte(0)
					if err != nil {
						return 0, err
					}
					if end == 240 {
						break
					}
				}
			}
		}
	}
}

func decodeSequence(sequence []byte) key {
	if len(sequence) < 2 {
		return keyOther
	}
	last := sequence[len(sequence)-1]
	if sequence[0] == 'O' {
		switch last {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		case 'C':
			return keyRight
		case 'D':
			return keyLeft
		case 'H':
			return keyHome
		case 'F':
			return keyEnd
		case 'Q':
			return keyF2
		case 'R':
			return keyF3
		}
	}
	if sequence[0] != '[' {
		return keyOther
	}
	switch last {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		return keyRight
	case 'D':
		return keyLeft
	case 'H':
		return keyHome
	case 'F':
		return keyEnd
	case '~':
		switch string(sequence[1 : len(sequence)-1]) {
		case "1", "7":
			return keyHome
		case "2":
			return keyInsert
		case "3":
			return keyDelete
		case "4", "8":
			return keyEnd
		case "5":
			return keyPageUp
		case "6":
			return keyPageDown
		case "12":
			return keyF2
		case "13":
			return keyF3
		}
	}
	return keyOther
}

func main() {
	if err := run(); err != nil {
		logStartupError(err)
		os.Exit(1)
	}
}

func run() error {
	value, err := readDoor32Socket("door32.sys")
	if err != nil {
		return err
	}
	conn, err := openDoorSocket(value)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(defaultConfigPath)
	if err != nil {
		return reportSocketError(conn, fmt.Errorf("read config: %w", err))
	}
	header, err := readHeaders(headerFile)
	if err != nil {
		return reportSocketError(conn, fmt.Errorf("read headers: %w", err))
	}
	lines, err := readBody(bodyFile)
	if err != nil {
		return reportSocketError(conn, fmt.Errorf("read message: %w", err))
	}
	if len(lines) > maxLines {
		return reportSocketError(conn, fmt.Errorf("message has %d lines; maximum is %d", len(lines), maxLines))
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	e := &editor{conn: conn, reader: newKeyReader(conn), header: header, lines: lines, cfg: cfg, status: "Ready"}
	return e.run()
}

func reportSocketError(conn stream, err error) error {
	message := "\x1b[2J\x1b[HEleBBS editor startup failed\r\n\r\n" + cleanDisplay(err.Error()) + "\r\n\r\nPress any key to return."
	if _, writeErr := io.WriteString(conn, message); writeErr == nil {
		_, _, _ = newKeyReader(conn).readKey()
	}
	return err
}

func logStartupError(err error) {
	file, openErr := os.OpenFile("eleedit.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if openErr != nil {
		return
	}
	_, _ = fmt.Fprintf(file, "%s %s\r\n", time.Now().Format(time.RFC3339), err)
	_ = file.Close()
}

func (e *editor) run() error {
	if _, err := io.WriteString(e.conn, "\x1b[?25h\x1b[2J\x1b[H"); err != nil {
		return err
	}
	for {
		if err := e.render(); err != nil {
			return err
		}
		action, value, err := e.reader.readKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read terminal input: %w", err)
		}
		quit, err := e.handle(action, value)
		if err != nil {
			return err
		}
		if quit {
			_, _ = io.WriteString(e.conn, "\x1b[0m\x1b[?25h\x1b[2J\x1b[H")
			return nil
		}
	}
}

func (e *editor) render() error {
	headerLines := e.displayHeaderLines()
	visibleRows := max(1, min(bodyRows-len(headerLines)+1, screenRows-len(headerLines)-4))
	if e.y < e.top {
		e.top = e.y
	}
	if e.y >= e.top+visibleRows {
		e.top = e.y - visibleRows + 1
	}
	var b strings.Builder
	fmt.Fprint(&b, "\x1b[?25l\x1b[H\x1b[1;44;97m")
	fmt.Fprint(&b, " EleBBS Message Editor ")
	fmt.Fprint(&b, "\x1b[K\x1b[0m")
	for index, line := range headerLines {
		fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[K", index+2, cleanDisplay(line))
	}
	separatorRow := len(headerLines) + 2
	fmt.Fprintf(&b, "\x1b[%d;1H%s", separatorRow, strings.Repeat("-", screenCols))
	bodyStartRow := separatorRow + 1
	for row := 0; row < visibleRows; row++ {
		lineIndex := e.top + row
		fmt.Fprintf(&b, "\x1b[%d;1H", bodyStartRow+row)
		if lineIndex < len(e.lines) {
			cursorColumn := -1
			if lineIndex == e.y {
				cursorColumn = min(e.x, screenCols-1)
			}
			fmt.Fprint(&b, renderBodyLine(e.lines[lineIndex], cursorColumn))
		}
		fmt.Fprint(&b, "\x1b[K")
	}
	fmt.Fprintf(&b, "\x1b[%d;1H\x1b[7m Line %4d/%-4d  Col %2d  %s  %-45.45s \x1b[K\x1b[0m", screenRows-1, e.y+1, len(e.lines), e.x+1, modeName(e.overtype), cleanDisplay(e.status))
	fmt.Fprintf(&b, "\x1b[%d;1H F2 Save  F3 Quote  /s Save  /q Quote  /a Abort  Ctrl-Y Delete line\x1b[K", screenRows)
	row := bodyStartRow + e.y - e.top
	fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[?25h", row, min(e.x+1, screenCols))
	_, err := io.WriteString(e.conn, b.String())
	return err
}

func (e *editor) displayHeaderLines() []string {
	return []string{
		headerPair("From:", e.header.From, "To:", e.header.To),
		headerPair("Subject:", e.header.Subject, "Message #:", e.header.MessageNumber),
		headerPair("Area:", e.header.Area, "Private:", e.header.Private),
	}
}

func headerPair(leftLabel, leftValue, rightLabel, rightValue string) string {
	left := fmt.Sprintf("%-10s %-29.29s", leftLabel, headerDisplayValue(leftValue))
	right := fmt.Sprintf("%-10s %-29.29s", rightLabel, headerDisplayValue(rightValue))
	return left + right
}

func headerDisplayValue(value string) string {
	value = cleanDisplay(value)
	if strings.TrimSpace(value) == "" {
		return "(blank)"
	}
	return value
}

func renderBodyLine(line string, cursorColumn int) string {
	var b strings.Builder
	limit := min(len(line), screenCols)
	for column := 0; column < limit; column++ {
		if column == cursorColumn {
			b.WriteString("\x1b[7m")
		}
		if line[column] < 32 || line[column] == 127 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(line[column])
		}
		if column == cursorColumn {
			b.WriteString("\x1b[0m")
		}
	}
	if cursorColumn >= limit && cursorColumn >= 0 && cursorColumn < screenCols {
		b.WriteString("\x1b[7m \x1b[0m")
	}
	return b.String()
}

func (e *editor) handle(action key, value byte) (bool, error) {
	line := []byte(e.lines[e.y])
	switch action {
	case keyEscape, keyQuit:
		return e.quitPrompt()
	case keyAbort:
		return true, nil
	case keyUp:
		if e.y > 0 {
			e.y--
			e.clampX()
		}
	case keyDown:
		if e.y < len(e.lines)-1 {
			e.y++
			e.clampX()
		}
	case keyLeft:
		if e.x > 0 {
			e.x--
		} else if e.y > 0 {
			e.y--
			e.x = len(e.lines[e.y])
		}
	case keyRight:
		if e.x < len(line) {
			e.x++
		} else if e.y < len(e.lines)-1 {
			e.y++
			e.x = 0
		}
	case keyHome:
		e.x = 0
	case keyEnd:
		e.x = len(line)
	case keyPageUp:
		e.y = max(0, e.y-bodyRows)
		e.clampX()
	case keyPageDown:
		e.y = min(len(e.lines)-1, e.y+bodyRows)
		e.clampX()
	case keyInsert:
		e.overtype = !e.overtype
	case keyBackspace:
		if e.x > 0 {
			line = append(line[:e.x-1], line[e.x:]...)
			e.x--
			e.lines[e.y] = string(line)
		} else if e.y > 0 {
			previousLength := len(e.lines[e.y-1])
			e.lines[e.y-1] += e.lines[e.y]
			e.lines = append(e.lines[:e.y], e.lines[e.y+1:]...)
			e.y--
			e.x = previousLength
		}
	case keyDelete:
		if e.x < len(line) {
			line = append(line[:e.x], line[e.x+1:]...)
			e.lines[e.y] = string(line)
		} else if e.y < len(e.lines)-1 {
			e.lines[e.y] += e.lines[e.y+1]
			e.lines = append(e.lines[:e.y+1], e.lines[e.y+2:]...)
		}
	case keyEnter:
		switch strings.ToLower(strings.TrimSpace(e.lines[e.y])) {
		case "/s":
			e.removeCurrentLine()
			if err := e.save(); err != nil {
				e.status = "Save failed: " + err.Error()
				return false, nil
			}
			return true, nil
		case "/q":
			e.lines[e.y], e.x = "", 0
			e.insertQuote()
			return false, nil
		case "/a":
			return true, nil
		}
		if len(e.lines) >= maxLines {
			e.status = "Maximum line count reached"
			break
		}
		e.lines[e.y] = string(line[:e.x])
		remainder := string(line[e.x:])
		e.lines = append(e.lines, "")
		copy(e.lines[e.y+2:], e.lines[e.y+1:])
		e.lines[e.y+1] = remainder
		e.y++
		e.x = 0
	case keySave, keyF2:
		if err := e.save(); err != nil {
			e.status = "Save failed: " + err.Error()
		} else {
			e.status = "Message saved"
		}
	case keyF3:
		e.insertQuote()
	case keyDeleteLine:
		e.removeCurrentLine()
	case keyOther:
		if value >= 32 && value != 127 && len(line) < screenCols-1 {
			if e.overtype && e.x < len(line) {
				line[e.x] = value
			} else {
				line = append(line, 0)
				copy(line[e.x+1:], line[e.x:])
				line[e.x] = value
			}
			e.lines[e.y] = string(line)
			e.x++
		}
	}
	return false, nil
}

func (e *editor) quitPrompt() (bool, error) {
	if _, err := io.WriteString(e.conn, "\x1b[23;1H\x1b[7m Save changes?  Y=Save  N=Discard  Any other key=Continue \x1b[K\x1b[0m"); err != nil {
		return false, err
	}
	choice, value, err := e.reader.readKey()
	if err != nil {
		return false, err
	}
	if choice == keyOther && (value == 'y' || value == 'Y') {
		if err := e.save(); err != nil {
			e.status = "Save failed: " + err.Error()
			return false, nil
		}
		return true, nil
	}
	if choice == keyOther && (value == 'n' || value == 'N') {
		return true, nil
	}
	return false, nil
}

func (e *editor) save() error {
	return writeBody(bodyFile, e.cfg.appendSignature(e.lines))
}

func (e *editor) insertQuote() {
	quoted, err := readBody(quotePath)
	if err != nil || len(quoted) == 0 {
		e.status = "No quote text available"
		return
	}
	for i, line := range quoted {
		quoted[i] = e.cfg.stripPrefix(line)
		if e.cfg.Quote.QuoteAll && e.cfg.Quote.Prefix != "" {
			quoted[i] = e.cfg.Quote.Prefix + quoted[i]
		}
	}
	if len(e.lines)+len(quoted) > maxLines {
		e.status = "Quote exceeds maximum line count"
		return
	}
	current := e.lines[e.y]
	left, right := current[:e.x], current[e.x:]
	inserted := make([]string, 0, len(quoted))
	inserted = append(inserted, left+quoted[0])
	inserted = append(inserted, quoted[1:]...)
	inserted[len(inserted)-1] += right
	newLines := make([]string, 0, len(e.lines)+len(quoted)-1)
	newLines = append(newLines, e.lines[:e.y]...)
	newLines = append(newLines, inserted...)
	newLines = append(newLines, e.lines[e.y+1:]...)
	e.lines = newLines
	e.y += len(inserted) - 1
	e.x = len(inserted[len(inserted)-1]) - len(right)
	e.status = "Quote inserted"
}

func (e *editor) clampX() {
	if e.x > len(e.lines[e.y]) {
		e.x = len(e.lines[e.y])
	}
}

func (e *editor) removeCurrentLine() {
	if len(e.lines) == 1 {
		e.lines[0], e.x = "", 0
		return
	}
	e.lines = append(e.lines[:e.y], e.lines[e.y+1:]...)
	if e.y >= len(e.lines) {
		e.y = len(e.lines) - 1
	}
	e.clampX()
}

func modeName(overtype bool) string {
	if overtype {
		return "OVR"
	}
	return "INS"
}

func cleanDisplay(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] < 32 || value[i] == 127 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
