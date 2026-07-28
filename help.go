package main

import (
	"context"
	_ "embed"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/unxed/vtui"
)

//go:embed help.hlf
var helpData string

//go:embed README.md
var readmeData string

type memoryHelpVFS struct {
	files map[string]string
}

func (m *memoryHelpVFS) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	content, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

var mdLinkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

func convertMarkdownLinks(line string) string {
	return mdLinkRegex.ReplaceAllString(line, "~$1~$2@")
}

func parseMarkdownToHelpTopic(name string, mdContent string) *vtui.HelpTopic {
	topic := &vtui.HelpTopic{
		Name: name,
	}
	lines := strings.Split(mdContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			headerText := strings.TrimSpace(trimmed[level:])
			line = "#" + headerText + "#"
			if level == 1 && topic.StickyRows == 0 {
				topic.StickyRows = 1
				topic.Lines = append([]string{headerText}, topic.Lines...)
				continue
			}
			line = convertMarkdownLinks(line)
			topic.Lines = append(topic.Lines, line)
			continue
		}

		wrapped := vtui.WrapText(line, 70)
		for _, wLine := range wrapped {
			wLine = convertMarkdownLinks(wLine)
			topic.Lines = append(topic.Lines, wLine)
		}
	}
	return topic
}

func InitHelpSystem() {
	versionedHelp := strings.ReplaceAll(helpData, "%Ver", getFormattedVersionInfo())
	v := &memoryHelpVFS{
		files: map[string]string{
			"help.hlf": versionedHelp,
		},
	}
	vtui.GlobalHelpEngine = vtui.NewHelpEngine(v)
	_ = vtui.GlobalHelpEngine.LoadFile("help.hlf")

	readmeTopic := parseMarkdownToHelpTopic("README", readmeData)
	vtui.GlobalHelpEngine.AddTopic(readmeTopic)
}
