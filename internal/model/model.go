package model

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Ensure that Model implements tea.Model.
var _ tea.Model = (*Model)(nil)

// selectedWindowIndex indicates which window has focus.
type selectedWindowIndex int

// Possible selected window indexes.
const (
	selectorWindow selectedWindowIndex = iota
	formatWindow
	groupsWindow
	outputWindow
)

// Model holds the state of the application.
type Model struct {
	groups      list.Model
	content     string
	output      viewport.Model
	selector    textinput.Model
	format      textinput.Model
	selectedIdx selectedWindowIndex
	path        string
	jq          string
	log         io.Writer
	zoomed      bool
	wrapped     bool
	width       int
	height      int
}

// ModelOpts defines the options that can be set on a Model.
type ModelOpts struct {
	Selector string
	Format   string
	Path     string
}

// NewModel returns a new Model configured with the given ModelOpts.
func NewModel(opts ModelOpts) *Model {
	m := &Model{}
	m.log, _ = os.OpenFile("messages.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	m.selector = textinput.New()
	m.selector.Prompt = "Group by path> "
	m.selector.Cursor.SetMode(cursor.CursorStatic)
	m.selector.SetValue(opts.Selector)
	m.format = textinput.New()
	m.format.Prompt = "output format> "
	m.format.Cursor.SetMode(cursor.CursorStatic)
	m.format.SetValue(opts.Format)
	m.path = opts.Path
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0) // compact lists
	m.groups = list.New([]list.Item{}, delegate, 10, 20)
	m.groups.Title = "groups"
	m.groups.SetShowHelp(false)
	m.groups.SetShowTitle(false)
	m.groups.SetShowStatusBar(false)
	m.output = viewport.New(0, 0)
	return m
}

// Init initializes the application. It focuses on the selector element and
// returns a command that populates the groups list from any values specified in
// ModelOpts at NewModel time.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(loadGroups(m.selector.Value(), m.path), m.selector.Focus())
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		newModel, cmd, handled := m.handleGlobalKey(msg)
		if handled {
			return newModel, cmd
		}
	case groupsContent:
		return m.handleGroupsContent(msg)
	case groupsError:
		return m.handleGroupsError(msg)
	case outputContent:
		return m.handleOutputContent(msg)
	case outputError:
		return m.handleOutputError(msg)
	}
	if m.zoomed {
		return m.handleOutputMessage(msg)
	}
	switch m.selectedIdx {
	case selectorWindow:
		return m.handleSelectorMessage(msg)
	case formatWindow:
		return m.handleFormatMessage(msg)
	case groupsWindow:
		return m.handleGroupsMessage(msg)
	case outputWindow:
		return m.handleOutputMessage(msg)
	}
	return m, cmd
}

// View returns the view for this model. If the application is zoomed on the
// output window then just the output window and footer are rendered.
// Otherwise, all of the windows are rendered, with the unfocused windows shown
// with a faint style.
func (m *Model) View() string {
	if m.zoomed {
		border := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true).BorderForeground(lipgloss.Color("#9ACD32"))
		return lipgloss.JoinVertical(lipgloss.Top,
			border.Render(m.output.View()),
			m.footerView(),
		)
	}
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(lipgloss.Color("#9ACD32"))
	faint := border.Faint(true).BorderForeground(lipgloss.Color("#50545c"))
	var selectorView, formatView, groupsView, outputView string
	switch m.selectedIdx {
	case selectorWindow:
		selectorView = border.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		groupsView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case formatWindow:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = border.Width(m.format.Width).Render(m.format.View())
		groupsView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case groupsWindow:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		groupsView = border.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case outputWindow:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		groupsView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = border.Width(m.output.Width).Render(m.output.View())
	}
	return strings.Join(
		[]string{
			lipgloss.JoinVertical(lipgloss.Top,
				lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(m.path),
				selectorView,
				formatView,
				lipgloss.JoinHorizontal(lipgloss.Top,
					groupsView,
					outputView,
				),
				m.footerView(),
			),
		}, "\n")
}

// handleWindowSize handles window size messages. It resizes all elements based
// on the new size and whether the output window is zoomed or not. It also
// re-sets the content in the output window because we must handle wrapping
// ourselves (https://github.com/charmbracelet/bubbletea/issues/1017).
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.selector.Width = m.width - 2
	m.format.Width = m.width - 2
	m.groups.SetWidth(40)
	m.groups.SetHeight(m.height - 10)
	if m.zoomed {
		m.output.Height = m.height - 2
		m.output.Width = m.width
	} else {
		m.output.Width = m.width - 40 - 4
		m.output.Height = m.height - 10
	}
	m.output.SetContent(m.wrapOrTrunc(m.content, m.output.Width))
	return m, nil
}

// handleGlobalKey handles global key presses. If the key is handled then a new
// model and command are returned along with true. If the key is not handled
// then false is returned and the caller must pass the message to the focused
// component.
// * tab and shift-tab cycle focus
// * escape exits the application
// * f, when the output window has focus, toggles fullscreen
// * w, when the output window has focus, toggles wrapped
func (m *Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg.String() {
	case "tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedIdx {
		case selectorWindow:
			m.selectedIdx = 1
			m.format.Blur()
			cmd = m.format.Focus()
		case formatWindow:
			m.selectedIdx = 2
			m.selector.Blur()
		case groupsWindow:
			m.selectedIdx = 3
		case outputWindow:
			m.selectedIdx = 0
			cmd = m.selector.Focus()
		}
		return m, cmd, true
	case "shift+tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedIdx {
		case selectorWindow:
			m.selectedIdx = 3
			m.selector.Blur()
		case formatWindow:
			m.selectedIdx = 0
			m.format.Blur()
			cmd = m.selector.Focus()
		case groupsWindow:
			m.selectedIdx = 1
			cmd = m.format.Focus()
		case outputWindow:
			m.selectedIdx = 2
		}
		return m, cmd, true
	case "esc":
		if m.zoomed {
			m.zoomed = false
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		if m.selectedIdx == groupsWindow && m.groups.FilterState() == list.Filtering {
			m.groups, cmd = m.groups.Update(msg)
			return m, cmd, true
		}
		cmd = tea.Quit
		return m, cmd, true
	case "f":
		if m.selectedIdx == outputWindow {
			m.zoomed = !m.zoomed
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		return m, cmd, false
	case "w":
		if m.selectedIdx == outputWindow {
			m.wrapped = !m.wrapped
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		return m, cmd, false
	case "G":
		if m.selectedIdx == outputWindow {
			m.output.GotoBottom()
			return m, cmd, true
		}
		return m, cmd, false
	case "g":
		if m.selectedIdx == outputWindow {
			m.output.GotoTop()
			return m, cmd, true
		}
		return m, cmd, false
	}
	return m, cmd, false
}

// handleGroupsContent handles the groupsContent message. It sets the group list
// content and issues a loadContent command. If there is no item selected then
// "*" is passed as the group to loadContent.
func (m *Model) handleGroupsContent(msg groupsContent) (tea.Model, tea.Cmd) {
	cmd := m.groups.SetItems(msg.items)
	// Handle the page indicator if more than one page is present
	if m.groups.Paginator.TotalPages > 1 {
		m.groups.SetHeight(m.height - 11)
	}
	selectedItem := m.groups.SelectedItem()
	selectedItemText := "*"
	if selectedItem != nil {
		selectedItemText = selectedItem.FilterValue()
	}
	return m, tea.Batch(loadContent(m.selector.Value(), selectedItemText, m.format.Value(), m.path), cmd)
}

// handleGroupsError handles the groupsError message. It clears the list of
// groups, sets the jq command in the model, and sets the output window to
// display the error.
func (m *Model) handleGroupsError(msg groupsError) (tea.Model, tea.Cmd) {
	cmd := m.groups.SetItems([]list.Item{})
	m.jq = msg.jq
	m.output.SetContent(msg.err.Error() + "\n" + msg.message)
	return m, cmd
}

// handleOutputContent handles the outputContent message. It saves the jq
// command and the original content. It then sets the output window to the
// wrapped version of that content.  We have to handle wrapping ourselves
// (https://github.com/charmbracelet/bubbletea/issues/1017).
func (m *Model) handleOutputContent(msg outputContent) (tea.Model, tea.Cmd) {
	m.jq = msg.jq
	m.content = msg.content
	m.output.SetContent(m.wrapOrTrunc(msg.content, m.output.Width))
	return m, nil
}

// handleOutputError handles the outputError message. It sets the jq command in
// the model and sets the output window to display the error.
func (m *Model) handleOutputError(msg outputError) (tea.Model, tea.Cmd) {
	m.jq = msg.jq
	m.output.SetContent(msg.err.Error() + "\n" + msg.message)
	return m, nil
}

// handleSelectorMessage handles messages sent to the selector window. If the
// selector value changed based on the message, then a loadGroups command is
// returned.
func (m *Model) handleSelectorMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.selector.Value()
	m.selector, cmd = m.selector.Update(msg)
	newValue := m.selector.Value()
	if origValue == newValue {
		return m, cmd
	}
	return m, tea.Batch(loadGroups(newValue, m.path), cmd)
}

// handleFormatMessage handles messages sent to the format window. If the format
// value changed based on the message then a check is made to see if a an item
// is selected in the list. If no item is selected then the output window is
// cleared. If an item is selected then a loadContent command is returned.
func (m *Model) handleFormatMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.format.Value()
	m.format, cmd = m.format.Update(msg)
	newValue := m.format.Value()
	if origValue == newValue {
		return m, cmd
	}
	selectedItem := m.groups.SelectedItem()
	selectedItemText := "*"
	if selectedItem != nil {
		selectedItemText = selectedItem.FilterValue()
	}
	return m, tea.Batch(loadContent(m.selector.Value(), selectedItemText, newValue, m.path), cmd)
}

// handleGroupsMessage handles messages sent to the groups list window. If the
// value of the list changed based on the message then a loadContent message is
// returned.
func (m *Model) handleGroupsMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.groups.SelectedItem()
	m.groups, cmd = m.groups.Update(msg)
	newValue := m.groups.SelectedItem()
	if origValue == newValue {
		return m, cmd
	}
	selectedItemText := "*"
	if newValue != nil {
		selectedItemText = newValue.FilterValue()
	}
	if origValue != newValue {
		return m, tea.Batch(loadContent(m.selector.Value(), selectedItemText, m.format.Value(), m.path), cmd)
	}
	return m, cmd
}

// hadleOutputMessage handles messages sent to the output window. Currently the
// message is passed to the output window and no other action is taken.
func (m *Model) handleOutputMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.output, cmd = m.output.Update(msg)
	return m, cmd
}

// footerView returns the view of the footer. It contains the current jq command
// and the current scroll percentage of the output window with enough space
// between them to put the percentage at the right of the screen.
func (m *Model) footerView() string {
	scrollPercent := fmt.Sprintf("%3.f%%", m.output.ScrollPercent()*100)
	spaceCount := m.selector.Width - len(m.jq) - len(scrollPercent)
	if spaceCount < 0 {
		return ""
	}
	return fmt.Sprintf(" %s%s%s", m.jq, strings.Repeat(" ", spaceCount), scrollPercent)
}

// wrapOrTrunc returns the given string with either new lines inserted to wrap
// at the given column width to wrap lines or characters that would extend
// beyond the column width removed. We have to handle wrapping ourselves
// (https://github.com/charmbracelet/bubbletea/issues/1017).
func (m *Model) wrapOrTrunc(content string, width int) string {
	if m.wrapped {
		return wrap(content, width)
	}
	return trunc(content, width)
}

// wrap returns the given string with new lines inserted to wrap at the given
// column width.  We have to handle wrapping ourselves
// (https://github.com/charmbracelet/bubbletea/issues/1017).
func wrap(content string, width int) string {
	origLines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(origLines)*2)
	for _, origLine := range origLines {
		runes := []rune(origLine)
		runesL := len(runes)
		for i := 0; i < runesL; i += width {
			max := min(runesL, i+width)
			newLines = append(newLines, string(runes[i:max]))
		}
	}
	return strings.Join(newLines, "\n")
}

// trunc returns the given string with characters that would extend beyond the
// column width removed.
func trunc(content string, width int) string {
	origLines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(origLines))
	for _, origLine := range origLines {
		runes := []rune(origLine)
		runesL := len(runes)
		max := min(runesL, width)
		newLines = append(newLines, string(runes[:max]))
	}
	return strings.Join(newLines, "\n")
}

// min returns the minimum of the two given integers.
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
