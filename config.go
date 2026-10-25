package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

type quoteConfig struct {
	Prefix   string
	Strip    bool
	QuoteAll bool
}

type signatureConfig struct {
	Text   string
	Append bool
}

type config struct {
	Quote     quoteConfig
	Signature signatureConfig
}

func loadConfig(path string) (config, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return config{}, err
	}
	defer file.Close()
	return parseConfig(file)
}

func configPathForUser(username string) string {
	identity := strings.ToLower(strings.TrimSpace(username))
	digest := sha256.Sum256([]byte(identity))
	return "eleedit-user-" + hex.EncodeToString(digest[:]) + ".ini"
}

func saveConfig(path string, cfg config) error {
	contents := fmt.Sprintf("quote_prefix = %s\nquote_strip = %s\nquote_all = %s\nsig_text = %s\nsig_append = %s\n",
		cfg.Quote.Prefix,
		boolSetting(cfg.Quote.Strip),
		boolSetting(cfg.Quote.QuoteAll),
		encodeSignature(cfg.Signature.Text),
		boolSetting(cfg.Signature.Append),
	)
	return os.WriteFile(path, []byte(contents), 0600)
}

func encodeSignature(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func decodeSignature(value string) string {
	var decoded strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+1 < len(value) {
			if value[index+1] == 'n' {
				decoded.WriteByte('\n')
				index++
				continue
			}
			if value[index+1] == '\\' {
				decoded.WriteByte('\\')
				index++
				continue
			}
		}
		decoded.WriteByte(value[index])
	}
	return decoded.String()
}

func boolSetting(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func defaultConfig() config {
	return config{Quote: quoteConfig{Prefix: ">", Strip: true}}
}

func parseConfig(input io.Reader) (config, error) {
	cfg := defaultConfig()
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		sep := strings.Index(line, "=>")
		skip := 2
		if sep < 0 {
			sep = strings.IndexByte(line, '=')
			skip = 1
		}
		if sep < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:sep]))
		value := strings.TrimSpace(line[sep+skip:])
		switch key {
		case "quote_prefix":
			cfg.Quote.Prefix = value
		case "quote_strip":
			cfg.Quote.Strip = enabled(value)
		case "quote_all":
			cfg.Quote.QuoteAll = enabled(value)
		case "sig_text":
			cfg.Signature.Text = decodeSignature(value)
		case "sig_append":
			cfg.Signature.Append = enabled(value)
		}
	}
	return cfg, scanner.Err()
}

func enabled(value string) bool {
	return strings.EqualFold(value, "yes") || value == "1" || strings.EqualFold(value, "true")
}

func (cfg config) appendSignature(lines []string) []string {
	if !cfg.Signature.Append || cfg.Signature.Text == "" {
		return lines
	}
	return append(append([]string(nil), lines...), strings.Split(cfg.Signature.Text, "\n")...)
}

func (cfg config) stripPrefix(line string) string {
	if cfg.Quote.Strip && cfg.Quote.Prefix != "" && strings.HasPrefix(line, cfg.Quote.Prefix) {
		return line[len(cfg.Quote.Prefix):]
	}
	return line
}
