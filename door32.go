package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func readDoor32Socket(path string) (uintptr, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return parseDoor32Socket(file)
}

func parseDoor32Socket(input io.Reader) (uintptr, error) {
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("missing communication type line")
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("missing communication handle line")
	}
	handle, err := strconv.ParseUint(strings.TrimSpace(scanner.Text()), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid communication handle: %w", err)
	}
	if handle == 0 {
		return 0, fmt.Errorf("communication handle is zero")
	}
	return uintptr(handle), nil
}
