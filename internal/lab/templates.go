package lab

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*
var templateFS embed.FS

func renderTemplate(name string, data any) ([]byte, error) {
	tpl, err := template.ParseFS(templateFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func yamlBlock(value any, indent int) (string, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	prefix := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n"), nil
}

func shellAssign(name string, value string) string {
	return fmt.Sprintf("%s=%s", name, shellQuote(value))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func cloudPlaneProviderSpec(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		if key == "provider" || key == "instanceType" {
			continue
		}
		out[key] = value
	}
	return out
}
