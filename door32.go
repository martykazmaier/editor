package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func readDoor32(path string) (uintptr, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	return parseDoor32(file)
}

func parseDoor32(input io.Reader) (uintptr, string, error) {
	scanner := bufio.NewScanner(input)
	var lines []string
	for scanner.Scan() && len(lines) < 6 {
		lines = append(lines, strings.TrimRight(scanner.Text(), "\r"))
	}
	if err := scanner.Err(); err != nil {
		return 0, "", err
	}
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("door32.sys is missing the communication handle")
	}
	if len(lines) < 6 {
		return 0, "", fmt.Errorf("door32.sys is missing the user name")
	}
	handle, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 32)
	if err != nil {
		return 0, "", fmt.Errorf("invalid communication handle: %w", err)
	}
	if handle == 0 {
		return 0, "", fmt.Errorf("communication handle is zero")
	}
	userName := strings.TrimSpace(lines[5])
	if userName == "" {
		return 0, "", fmt.Errorf("door32.sys user name is blank")
	}
	return uintptr(handle), userName, nil
}
