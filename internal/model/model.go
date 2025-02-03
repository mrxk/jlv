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
	"github.com/davecgh/go-spew/spew"
)

var _ tea.Model = (*Model)(nil)

type Model struct {
	groups      list.Model
	content     string
	output      viewport.Model
	selector    textinput.Model
	format      textinput.Model
	selectedIdx int
	path        string
	jq          string
	log         io.Writer
	zoomed      bool
	wrapped     bool
	width       int
	height      int
}

type ModelOpts struct {
	Selector string
	Format   string
	Path     string
}

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

func (m *Model) Init() tea.Cmd {
	return tea.Batch(loadGroups(m.selector.Value(), m.path), m.selector.Focus())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	spew.Fdump(m.log, msg)
	//spew.Fdump(m.log, m.selector.CurrentSuggestion())
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
	case 0:
		return m.handleSelectorMessage(msg)
	case 1:
		return m.handleFormatMessage(msg)
	case 2:
		return m.handleGroupsMessage(msg)
	case 3:
		return m.handleOutputMessage(msg)
	}
	return m, cmd
}

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
	m.output.SetContent(wrap(m.content, m.output.Width))
	return m, nil
}

func (m *Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg.String() {
	case "tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedIdx {
		case 0:
			m.selectedIdx = 1
			m.format.Blur()
			cmd = m.format.Focus()
		case 1:
			m.selectedIdx = 2
			m.selector.Blur()
		case 2:
			m.selectedIdx = 3
		case 3:
			m.selectedIdx = 0
			cmd = m.selector.Focus()
		}
		return m, cmd, true
	case "shift+tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedIdx {
		case 0:
			m.selectedIdx = 3
			m.selector.Blur()
		case 1:
			m.selectedIdx = 0
			m.format.Blur()
			cmd = m.selector.Focus()
		case 2:
			m.selectedIdx = 1
			cmd = m.format.Focus()
		case 3:
			m.selectedIdx = 2
		}
		return m, cmd, true
	case "esc":
		if m.zoomed {
			m.zoomed = false
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		cmd = tea.Quit
		return m, cmd, true
	case "f":
		if m.selectedIdx == 3 {
			m.zoomed = !m.zoomed
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		return m, cmd, false
	}
	return m, cmd, false
}

func (m *Model) handleGroupsContent(msg groupsContent) (tea.Model, tea.Cmd) {
	cmd := m.groups.SetItems(msg.items)
	selectedItem := m.groups.SelectedItem()
	selectedItemText := "*"
	if selectedItem != nil {
		spew.Fdump(m.log, m.groups.Index())
		selectedItemText = selectedItem.FilterValue()
	}
	return m, tea.Batch(loadContent(m.selector.Value(), selectedItemText, m.format.Value(), m.path), cmd)
}

func (m *Model) handleGroupsError(msg groupsError) (tea.Model, tea.Cmd) {
	cmd := m.groups.SetItems([]list.Item{})
	m.jq = msg.jq
	m.output.SetContent(msg.err.Error() + "\n" + msg.message)
	return m, cmd
}

func (m *Model) handleOutputContent(msg outputContent) (tea.Model, tea.Cmd) {
	m.jq = msg.jq
	m.content = msg.content
	m.output.SetContent(wrap(msg.content, m.output.Width))
	return m, nil
}

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

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func (m *Model) handleOutputError(msg outputError) (tea.Model, tea.Cmd) {
	m.jq = msg.jq
	m.output.SetContent(msg.err.Error() + "\n" + msg.message)
	return m, nil
}

func (m *Model) handleSelectorMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.selector.Value()
	m.selector, cmd = m.selector.Update(msg)
	newValue := m.selector.Value()
	if origValue != newValue {
		return m, tea.Batch(loadGroups(newValue, m.path))
	}
	return m, cmd
}

func (m *Model) handleFormatMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.format.Value()
	m.format, cmd = m.format.Update(msg)
	newValue := m.format.Value()
	if origValue != newValue {
		selectedItem := m.groups.SelectedItem()
		if selectedItem == nil {
			m.output.SetContent("")
			return m, cmd
		}
		return m, tea.Batch(loadContent(m.selector.Value(), selectedItem.FilterValue(), newValue, m.path), cmd)
	}
	return m, cmd
}

func (m *Model) handleGroupsMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.groups.SelectedItem()
	m.groups, cmd = m.groups.Update(msg)
	newValue := m.groups.SelectedItem()
	if origValue != newValue {
		return m, tea.Batch(loadContent(m.selector.Value(), newValue.FilterValue(), m.format.Value(), m.path))
	}
	return m, cmd
}

func (m *Model) handleOutputMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	spew.Fdump(m.log, msg)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "pgdown":
			m.output.SetYOffset(m.output.YOffset + m.output.Height)
			return m, cmd
		}
	}
	m.output, cmd = m.output.Update(msg)
	return m, cmd
}

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
	var selectorView, formatView, loggersView, outputView string
	switch m.selectedIdx {
	case 0:
		selectorView = border.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		loggersView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case 1:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = border.Width(m.format.Width).Render(m.format.View())
		loggersView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case 2:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		loggersView = border.Width(m.groups.Width()).Render(m.groups.View())
		outputView = faint.Width(m.output.Width).Render(m.output.View())
	case 3:
		selectorView = faint.Width(m.selector.Width).Render(m.selector.View())
		formatView = faint.Width(m.format.Width).Render(m.format.View())
		loggersView = faint.Width(m.groups.Width()).Render(m.groups.View())
		outputView = border.Width(m.output.Width).Render(m.output.View())
	}
	return strings.Join(
		[]string{
			lipgloss.JoinVertical(lipgloss.Top,
				lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(m.path),
				selectorView,
				formatView,
				lipgloss.JoinHorizontal(lipgloss.Top,
					loggersView,
					outputView,
				),
				m.footerView(),
			),
		}, "\n")
}

func (m *Model) footerView() string {
	scrollPercent := fmt.Sprintf("%3.f%%", m.output.ScrollPercent()*100)
	spaceCount := m.selector.Width - len(m.jq) - len(scrollPercent)
	if spaceCount < 0 {
		return ""
	}
	return fmt.Sprintf(" %s%s%s", m.jq, strings.Repeat(" ", spaceCount), scrollPercent)
}
