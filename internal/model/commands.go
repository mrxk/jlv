package model

import (
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type groupsContent struct {
	items []list.Item
}

type groupsError struct {
	err     error
	message string
}

type outputContent struct {
	jq      string
	content string
}

type outputError struct {
	err     error
	message string
}

func createGroupsSelectorArg(selector string) string {
	if selector == "" {
		return "."
	}
	return fmt.Sprintf(".|select(%s)|%s", selector, selector)
}

func unique(items []string) []string {
	m := map[string]struct{}{}
	for _, item := range items {
		m[item] = struct{}{}
	}
	return slices.Sorted(maps.Keys(m))
}

func loadGroups(selector, path string) tea.Cmd {
	return func() tea.Msg {
		selector = createGroupsSelectorArg(selector)
		cmd := *exec.Command("jq", "-r", selector, path)
		content, err := cmd.CombinedOutput()
		if err != nil {
			return groupsError{
				message: string(content),
				err:     err,
			}
		}
		contentS := string(content)
		contentS = strings.TrimSpace(contentS)
		if contentS == "" {
			return groupsContent{
				items: []list.Item{},
			}
		}
		if contentS[0] == '{' || contentS[0] == '[' {
			return groupsContent{
				items: []list.Item{},
			}
		}
		groups := strings.Split(contentS, "\n")
		groups = unique(groups)
		items := make([]list.Item, 0, len(groups))
		for _, group := range groups {
			items = append(items, item(group))
		}
		return groupsContent{
			items: items,
		}
	}
}

func createContentArg(selector, group, format string) string {
	if selector == "" {
		selector = "."
	}
	if format == "" {
		format = "."
	}
	return fmt.Sprintf(".|select(%s==\"%s\")|%s", selector, group, format)
}

func loadContent(selector, group, format, path string) tea.Cmd {
	return func() tea.Msg {
		arg := createContentArg(selector, group, format)
		cmd := *exec.Command("jq", "-r", arg, path)
		content, err := cmd.CombinedOutput()
		if err != nil {
			return outputError{
				message: string(content),
				err:     err,
			}
		}
		contentS := string(content)
		return outputContent{
			jq:      "jq -r '" + arg + "' '" + path + "'",
			content: contentS,
		}
	}
}
