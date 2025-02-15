package model

import (
	"cmp"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// groupContent is a tea.Msg that is returned by the loadGroups command. It
// contains the list of groups to display in the list.
type groupsContent struct {
	items []list.Item
}

// groupsError is a tea.Msg that is returned by the loadGroups command when
// there is an error. It contains the eror, an optional message, and an optional
// jq string that produced the error.
type groupsError struct {
	err     error
	message string
	jq      string
}

// outputContent  is a tea.Msg that is returned by the loadContent command. It
// contains the jq command that produced the content and the result of executing
// that jq command.
type outputContent struct {
	jq      string
	content string
}

// outputError is a tea.Msg that is returned by the loadContent command when
// there is an error. It contains the eror, an optional message, and an optional
// jq string that produced the error.
type outputError struct {
	err     error
	message string
	jq      string
}

// createGroupsSelectorArg returns a jq query string for the given selector. It
// is expected that this selector identifies a field in a JSON object. Like
// ".level" or ".object.field". The returned string, when passed to jq, will
// produce a newline delimited list of strings that can be used to select
// objects where the selector matches the value.
func createGroupsSelectorArg(selector string) string {
	if selector == "" {
		return "."
	}
	return fmt.Sprintf(".|select(%s)|%s", selector, selector)
}

// loadGroups returns a tea.Cmd that, when executed, will produce either a
// groupsContent message or a groupsError message. The command creates a jq
// command from the given selector and path, executes that jq command, and
// returns the result.
func loadGroups(selector, path string) tea.Cmd {
	return func() tea.Msg {
		selector = createGroupsSelectorArg(selector)
		cmd := *exec.Command("jq", "-r", selector, path)
		content, err := cmd.CombinedOutput()
		if err != nil {
			return groupsError{
				message: string(content),
				err:     err,
				jq:      "jq -r '" + selector + "' '" + path + "'",
			}
		}
		contentS := string(content)
		contentS = strings.TrimSpace(contentS)
		if contentS == "" || contentS[0] == '{' || contentS[0] == '[' {
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

// createContentArg returns a jq query string for the given selector, group, and
// format. The selector identifies the field that must exist in the JSON
// objects, the group represents the value that the field must have, and the
// format represents the format of the object to return. For example,
// seletor:= ".level"
// group:="error"
// format:=".timeStamp + \":\" + .message"
func createContentArg(selector, group, format string) string {
	if selector == "" {
		selector = "."
	}
	if format == "" {
		format = "."
	}
	if group == "*" {
		return fmt.Sprintf(".|select(%s)|%s", selector, format)
	}
	return fmt.Sprintf(".|select(%s==\"%s\")|%s", selector, group, format)
}

// loadContent returns a tea.Cmd that, when executed, will produce either an
// outputError or an outputContent message.  The outputContent message will be
// cut at the given width.
func loadContent(selector, group, format, width, path string) tea.Cmd {
	return loadFilteredContent(selector, group, format, path, "cut", "-c", "-"+width)
}

// loadWrappedContent returns a tea.Cmd that, when executed, will produce either
// an outputError or an outputContent message.  The outputContent message will
// be wrapped at the given width.
func loadWrappedContent(selector, group, format, width, path string) tea.Cmd {
	return loadFilteredContent(selector, group, format, path, "fold", "-b"+width)
}

// loadFilteredContent returns a tea.Cmd that, when executed, will produce
// either an outputError or an outputContent message.  The command creates a jq
// command from the given args, pipes it through the given filter command, and
// returns the result.
func loadFilteredContent(selector, group, format, path, filterCommand string, filterArgs ...string) tea.Cmd {
	return func() tea.Msg {
		arg := createContentArg(selector, group, format)
		jqCmd := exec.Command("jq", "-r", arg, path)
		filterCmd := exec.Command(filterCommand, filterArgs...)
		filterStdinPipe, _ := filterCmd.StdinPipe()
		jqCmd.Stdout = filterStdinPipe
		_ = jqCmd.Start()
		go func() {
			defer filterStdinPipe.Close()
			_ = jqCmd.Wait()
		}()
		content, err := filterCmd.CombinedOutput()
		if err != nil {
			return outputError{
				message: string(content),
				err:     err,
				jq:      "jq -r '" + arg + "' '" + path + "'",
			}
		}
		contentS := string(content)
		return outputContent{
			jq:      "jq -r '" + arg + "' '" + path + "'",
			content: contentS,
		}
	}
}

// unique returns the unique items from the given slice.
func unique[T cmp.Ordered](items []T) []T {
	m := map[T]struct{}{}
	for _, item := range items {
		m[item] = struct{}{}
	}
	return slices.Sorted(maps.Keys(m))
}
