package main

import (
	"fmt"
	"strconv"
)

// EleBBS color codes are a pipe and two hexadecimal digits. The value is a
// BIOS attribute: low nibble foreground, high nibble background, bit 3 bright,
// bit 7 blink. The code is kept in the message and takes no screen column.
func pipeAttr(line string, index int) (byte, bool) {
	if index+2 >= len(line) || line[index] != '|' || !isHex(line[index+1]) || !isHex(line[index+2]) {
		return 0, false
	}
	value, err := strconv.ParseUint(line[index+1:index+3], 16, 8)
	if err != nil {
		return 0, false
	}
	return byte(value), true
}

func isHex(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'A' && value <= 'F') || (value >= 'a' && value <= 'f')
}

// sgrSequence returns a complete ANSI color sequence at index. Other control
// sequences are reported so the display can skip them instead of sending them.
func sgrSequence(line string, index int) (string, int, bool) {
	width := csiWidth(line, index)
	if width == 0 || line[index+width-1] != 'm' {
		return "", width, false
	}
	return line[index : index+width], width, true
}

func csiWidth(line string, index int) int {
	if index+1 >= len(line) || line[index] != 0x1b || line[index+1] != '[' {
		return 0
	}
	for end := index + 2; end < len(line) && end-index <= 32; end++ {
		value := line[end]
		if value >= 0x40 && value <= 0x7e {
			return end - index + 1
		}
		if (value < '0' || value > '9') && value != ';' && value != '?' {
			return 0
		}
	}
	return 0
}

func hiddenWidth(line string, index int) int {
	if _, ok := pipeAttr(line, index); ok {
		return 3
	}
	return csiWidth(line, index)
}

func visualColumn(line string, index int) int {
	if index > len(line) {
		index = len(line)
	}
	column := 0
	for i := 0; i < index; {
		if width := hiddenWidth(line, i); width > 0 {
			if i+width > index {
				break
			}
			i += width
			continue
		}
		column++
		i++
	}
	return column
}

func visibleWidth(line string) int {
	return visualColumn(line, len(line))
}

func byteIndexAtColumn(line string, column int) int {
	current := 0
	for index := 0; index < len(line); {
		if width := hiddenWidth(line, index); width > 0 {
			index += width
			continue
		}
		if current == column {
			return index
		}
		current++
		index++
	}
	return len(line)
}

func ansiForAttr(attr byte) string {
	foreground := [...]int{30, 34, 32, 36, 31, 35, 33, 37}
	background := [...]int{40, 44, 42, 46, 41, 45, 43, 47}
	fg := foreground[attr&7]
	bg := background[(attr>>4)&7]
	if attr&0x08 != 0 {
		fg += 60
	}
	blink := ""
	if attr&0x80 != 0 {
		blink = ";5"
	}
	return fmt.Sprintf("\x1b[0;%d;%d%sm", fg, bg, blink)
}
