package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type headers struct {
	From          string
	To            string
	Subject       string
	MessageNumber string
	Area          string
	Private       string
}

const (
	headerFile = "msginf"
	bodyFile   = "msgtmp"
)

func readHeaders(path string) (headers, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return headers{}, nil
		}
		return headers{}, err
	}
	defer file.Close()
	return parseHeaders(file)
}

func parseHeaders(input io.Reader) (headers, error) {
	var h headers
	scanner := bufio.NewScanner(input)
	fields := []*string{&h.From, &h.To, &h.Subject, &h.MessageNumber, &h.Area, &h.Private}
	for i, field := range fields {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return headers{}, err
			}
			return headers{}, fmt.Errorf("msginf is missing line %d", i+1)
		}
		*field = strings.TrimRight(scanner.Text(), "\r")
	}
	return h, scanner.Err()
}

func readBody(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func writeBody(path string, lines []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return w.Flush()
}
