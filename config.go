package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// preferencesFile is the one preferences file, kept beside this program so it
// stays put when the caller changes nodes.
func preferencesFile() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "eleedit.ini"), nil
}

func userSection(username string) string {
	name := strings.ToLower(strings.TrimSpace(username))
	name = strings.NewReplacer("\r", " ", "\n", " ", "]", "_").Replace(name)
	return strings.TrimSpace(name)
}

func formatConfig(cfg config) string {
	return fmt.Sprintf("quote_prefix = %s\nquote_strip = %s\nquote_all = %s\nsig_text = %s\nsig_append = %s\n",
		cfg.Quote.Prefix,
		boolSetting(cfg.Quote.Strip),
		boolSetting(cfg.Quote.QuoteAll),
		encodeSignature(cfg.Signature.Text),
		boolSetting(cfg.Signature.Append),
	)
}

func loadUserConfig(path, username string) (config, error) {
	users, err := loadAllUsers(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return config{}, err
	}
	cfg, ok := users[userSection(username)]
	if !ok {
		return defaultConfig(), nil
	}
	return cfg, nil
}

func saveUserConfig(path, username string, cfg config) error {
	section := userSection(username)
	if section == "" {
		return fmt.Errorf("user name is blank")
	}
	users, err := loadAllUsers(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if users == nil {
		users = map[string]config{}
	}
	users[section] = cfg
	return writeAllUsers(path, users)
}

func loadAllUsers(path string) (map[string]config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	users := map[string]config{}
	scanner := bufio.NewScanner(file)
	section := ""
	var body strings.Builder
	flush := func() error {
		if section == "" {
			return nil
		}
		cfg, err := parseConfig(strings.NewReader(body.String()))
		if err != nil {
			return err
		}
		users[section] = cfg
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if err := flush(); err != nil {
				return nil, err
			}
			section = userSection(line[1 : len(line)-1])
			body.Reset()
			continue
		}
		if section == "" {
			continue
		}
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return users, nil
}

func writeAllUsers(path string, users map[string]config) error {
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	sort.Strings(names)
	var contents strings.Builder
	for _, name := range names {
		fmt.Fprintf(&contents, "[%s]\n%s", name, formatConfig(users[name]))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(contents.String()), 0600); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporary, path)
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
