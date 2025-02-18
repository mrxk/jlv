package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mrxk/jlv/internal/processor"
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
	selectorModel    textinput.Model
	formatModel      textinput.Model
	groupsModel      list.Model
	outputModel      viewport.Model
	selectedWindow   selectedWindowIndex
	groups           map[string]struct{}
	rawOutputContent []string
	outputContent    []string
	path             string
	jq               string
	zoomed           bool
	wrap             bool
	lineNumbers      bool
	width            int
	height           int
	atBottom         bool
	processorCmdChan chan<- processor.Command
	contentStopped   bool
	groupsStopped    bool
}

// ModelOpts defines the options that can be set on a Model.
type ModelOpts struct {
	Selector    string
	Output      string
	Path        string
	LineNumbers bool
	Wrap        bool
}

// NewModel returns a new Model configured with the given ModelOpts.
func NewModel(opts ModelOpts) *Model {
	m := &Model{}
	m.selectorModel = textinput.New()
	m.selectorModel.Prompt = "Group by path> "
	m.selectorModel.Cursor.SetMode(cursor.CursorStatic)
	m.selectorModel.SetValue(opts.Selector)
	m.formatModel = textinput.New()
	m.formatModel.Prompt = "Output format> "
	m.formatModel.Cursor.SetMode(cursor.CursorStatic)
	m.formatModel.SetValue(opts.Output)
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0) // compact lists
	m.groups = map[string]struct{}{}
	m.groups["*"] = struct{}{}
	m.groupsModel = list.New(getGroupItems(m.groups), delegate, 10, 20)
	m.groupsModel.Title = "groups"
	m.groupsModel.SetShowHelp(false)
	m.groupsModel.SetShowTitle(false)
	m.groupsModel.SetShowStatusBar(false)
	m.outputModel = viewport.New(0, 0)
	m.path = opts.Path
	m.lineNumbers = opts.LineNumbers
	m.wrap = opts.Wrap
	m.atBottom = true
	return m
}

// Init initializes the application. It focuses on the selector element.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("jlv "+m.path),
		m.selectorModel.Focus())
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case processor.CommandChannel:
		return m.handleCommandChannel(msg)
	case processor.ContentStart:
		return m.handleProcessorContentStart(msg)
	case processor.ContentError:
		return m.handleProcessorContentError(msg)
	case processor.ContentLine:
		return m.handleProcessorContentLine(msg)
	case processor.GroupsStart:
		return m.handleProcessorGroupsStart(msg)
	case processor.GroupsError:
		return m.handleProcessorGroupError(msg)
	case processor.GroupsLine:
		return m.handleProcessorGroupLine(msg)
	case processor.ContentStopped:
		m.contentStopped = true
		if m.groupsStopped {
			cmd = tea.Quit
		}
		return m, cmd
	case processor.GroupsStopped:
		m.groupsStopped = true
		if m.contentStopped {
			cmd = tea.Quit
		}
		return m, cmd
	case processor.JQCommand:
		return m.handleProcessorJQCommand(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		newModel, cmd, handled := m.handleGlobalKey(msg)
		if handled {
			return newModel, cmd
		}
	}
	if m.zoomed {
		return m.handleOutputMessage(msg)
	}
	switch m.selectedWindow {
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
		border := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true).BorderForeground(lipgloss.Color("#6CB0D2"))
		return lipgloss.JoinVertical(lipgloss.Top,
			border.Render(m.outputModel.View()),
			m.footerView(),
		)
	}
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(lipgloss.Color("#6CB0D2"))
	faint := border.Faint(true).BorderForeground(lipgloss.Color("#505050"))
	var selectorView, formatView, groupsView, outputView string
	switch m.selectedWindow {
	case selectorWindow:
		selectorView = border.Width(m.selectorModel.Width).Render(m.selectorModel.View())
		formatView = faint.Width(m.formatModel.Width).Render(m.formatModel.View())
		groupsView = faint.Width(m.groupsModel.Width()).Render(m.groupsModel.View())
		outputView = faint.Width(m.outputModel.Width).Render(m.outputModel.View())
	case formatWindow:
		selectorView = faint.Width(m.selectorModel.Width).Render(m.selectorModel.View())
		formatView = border.Width(m.formatModel.Width).Render(m.formatModel.View())
		groupsView = faint.Width(m.groupsModel.Width()).Render(m.groupsModel.View())
		outputView = faint.Width(m.outputModel.Width).Render(m.outputModel.View())
	case groupsWindow:
		selectorView = faint.Width(m.selectorModel.Width).Render(m.selectorModel.View())
		formatView = faint.Width(m.formatModel.Width).Render(m.formatModel.View())
		groupsView = border.Width(m.groupsModel.Width()).Render(m.groupsModel.View())
		outputView = faint.Width(m.outputModel.Width).Render(m.outputModel.View())
	case outputWindow:
		selectorView = faint.Width(m.selectorModel.Width).Render(m.selectorModel.View())
		formatView = faint.Width(m.formatModel.Width).Render(m.formatModel.View())
		groupsView = faint.Width(m.groupsModel.Width()).Render(m.groupsModel.View())
		outputView = border.Width(m.outputModel.Width).Render(m.outputModel.View())
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

// handleProcessorJQCommand handles the processor.JQCommand. This message
// conveys the jq command that would result in the output being displayed.
func (m *Model) handleProcessorJQCommand(msg processor.JQCommand) (tea.Model, tea.Cmd) {
	m.jq = msg.Jq
	return m, nil
}

// handleProcessorContentStart handles the processor.ContentStart messge. This
// message means that the processor has started new read through the watched
// file. We clear our the content related state from the old processing.
func (m *Model) handleProcessorContentStart(msg processor.ContentStart) (tea.Model, tea.Cmd) {
	m.rawOutputContent = msg.InitialContent
	m.updateOutputModelContent()
	return m, nil
}

// handleProcessorContentError handles the processor.ContentError message. This
// message means that the processor encountered an error when trying to read
// content from the watched file.
func (m *Model) handleProcessorContentError(msg processor.ContentError) (tea.Model, tea.Cmd) {
	m.jq = msg.Jq
	cmd := m.groupsModel.SetItems(getGroupItems(m.groups))
	m.outputModel.SetContent(msg.Err.Error() + "\n" + msg.Message)
	return m, cmd
}

// handleProcessorContentLine handles the processor.ContentLine message. This
// message conveys a new line from the processor that should be displayed in the
// output window. If we are currently at the bottom then stay there.
func (m *Model) handleProcessorContentLine(msg processor.ContentLine) (tea.Model, tea.Cmd) {
	m.rawOutputContent = append(m.rawOutputContent, msg.Line)
	m.outputContent = append(m.outputContent, formatContentLine(m.wrap, m.lineNumbers, len(m.outputContent)+1, m.outputModel.Width, msg.Line)...)
	m.outputModel.SetContent(strings.Join(m.outputContent, "\n"))
	if m.atBottom {
		m.outputModel.GotoBottom()
	}
	return m, nil
}

// handleProcessorGroupsStart handles the processor.GroupsStart message. This
// message means that the processor has started a new read throughthe watched
// file for groups. We clear out our group related state from the old
// processing.
func (m *Model) handleProcessorGroupsStart(msg processor.GroupsStart) (tea.Model, tea.Cmd) {
	m.groups = map[string]struct{}{}
	m.groups["*"] = struct{}{}
	for _, group := range msg.InitialGroups {
		m.groups[group] = struct{}{}
	}
	cmd := m.groupsModel.SetItems(getGroupItems(m.groups))
	m.groupsModel.ResetSelected()
	m.updateGroupWidth()
	return m, tea.Batch(cmd, m.reloadContent)
}

// handleProcessorGroupError handles the processor.GroupError message. This
// message means that the processor encountered an error when trying to read
// groups from the watched file.
func (m *Model) handleProcessorGroupError(msg processor.GroupsError) (tea.Model, tea.Cmd) {
	m.jq = msg.Jq
	m.groups = map[string]struct{}{}
	m.groups["*"] = struct{}{}
	cmd := m.groupsModel.SetItems(getGroupItems(m.groups))
	m.outputModel.SetContent(msg.Err.Error() + "\n" + msg.Message)
	return m, cmd
}

// handleProcessorGroupLine handles the processor.GroupLine message. This
// message conveys a new group the processor that should be displayed in the
// groups window.
func (m *Model) handleProcessorGroupLine(msg processor.GroupsLine) (tea.Model, tea.Cmd) {
	m.groups[msg.Line] = struct{}{}
	groupItems := getGroupItems(m.groups)
	cmd := m.groupsModel.SetItems(groupItems)
	m.updateGroupWidth()
	return m, cmd
}

// handleCommandChannel handles the processor.CommandChannel message. This
// message conveys the channel that the processor will be listening on for
// commands from the application.
func (m *Model) handleCommandChannel(msg processor.CommandChannel) (tea.Model, tea.Cmd) {
	m.processorCmdChan = msg.CmdChan
	return m, m.reloadContent
}

// handleWindowSize handles window size messages. It resizes all elements based
// on the new size and whether the output window is zoomed or not.
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.selectorModel.Width = m.width - 2
	m.formatModel.Width = m.width - 2
	m.groupsModel.SetHeight(m.height - 10)
	if m.zoomed {
		m.outputModel.Height = m.height - 2
		m.outputModel.Width = m.width
	} else {
		m.outputModel.Width = m.width - m.groupsModel.Width() - 4
		m.outputModel.Height = m.height - 10
	}
	m.updateOutputModelContent()
	return m, nil
}

// handleGlobalKey handles global key presses. If the key is handled then a new
// model and command are returned along with true. If the key is not handled
// then false is returned and the caller must pass the message to the focused
// component.
// * tab and shift-tab cycle focus
// * escape backs out of a form or exits the application
// * f, when the output window has focus, toggles fullscreen
// * w, when the output window has focus, toggles wrapped
// * l, when the output window has focus, toggles line numbers
// * g, when the output window has focus, goes to the top
// * G, when the output window has focus, goes to the bottom
func (m *Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	var cmd tea.Cmd
	switch msg.String() {
	case "tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedWindow {
		case selectorWindow:
			m.selectedWindow = 1
			m.formatModel.Blur()
			cmd = m.formatModel.Focus()
		case formatWindow:
			m.selectedWindow = 2
			m.selectorModel.Blur()
		case groupsWindow:
			m.selectedWindow = 3
		case outputWindow:
			m.selectedWindow = 0
			cmd = m.selectorModel.Focus()
		}
		return m, cmd, true
	case "shift+tab":
		if m.zoomed {
			return m, cmd, false
		}
		switch m.selectedWindow {
		case selectorWindow:
			m.selectedWindow = 3
			m.selectorModel.Blur()
		case formatWindow:
			m.selectedWindow = 0
			m.formatModel.Blur()
			cmd = m.selectorModel.Focus()
		case groupsWindow:
			m.selectedWindow = 1
			cmd = m.formatModel.Focus()
		case outputWindow:
			m.selectedWindow = 2
		}
		return m, cmd, true
	case "esc":
		if m.zoomed {
			m.zoomed = false
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		if m.selectedWindow == groupsWindow && m.groupsModel.FilterState() == list.Filtering {
			m.groupsModel, cmd = m.groupsModel.Update(msg)
			return m, cmd, true
		}
		m.stopProcessor()
		return m, cmd, true
	case "f":
		if m.selectedWindow == outputWindow {
			m.zoomed = !m.zoomed
			newModel, cmd := m.handleWindowSize(tea.WindowSizeMsg{Height: m.height, Width: m.width})
			return newModel, cmd, true
		}
		return m, cmd, false
	case "w":
		if m.selectedWindow == outputWindow {
			m.wrap = !m.wrap
			m.updateOutputModelContent()
			return m, cmd, true
		}
		return m, cmd, false
	case "l":
		if m.selectedWindow == outputWindow {
			m.lineNumbers = !m.lineNumbers
			m.updateOutputModelContent()
			return m, cmd, true
		}
		return m, cmd, false
	case "G":
		if m.selectedWindow == outputWindow {
			m.outputModel.GotoBottom()
			m.atBottom = true
			return m, cmd, true
		}
		return m, cmd, false
	case "g":
		if m.selectedWindow == outputWindow {
			m.atBottom = false
			m.outputModel.GotoTop()
			return m, cmd, true
		}
		return m, cmd, false
	}
	return m, cmd, false
}

// handleSelectorMessage handles messages sent to the selector window. If the
// value of the selector changed based on the message, then a command is sent to
// the processor to re-start watching the file for groups.
func (m *Model) handleSelectorMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.selectorModel.Value()
	m.selectorModel, cmd = m.selectorModel.Update(msg)
	newValue := m.selectorModel.Value()
	if origValue == newValue {
		return m, cmd
	}
	// A selector that ends in a '.' is never valid.
	if len(newValue) > 1 && strings.HasSuffix(newValue, ".") {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.reloadGroups)
}

// handleFormatMessage handles messages sent to the format window. If the value
// of the format changed based on the message, then a comnmand is sent to the
// processor to re-start watching the file for content.
func (m *Model) handleFormatMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.formatModel.Value()
	m.formatModel, cmd = m.formatModel.Update(msg)
	newValue := m.formatModel.Value()
	if origValue == newValue {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.reloadContent)
}

// handleGroupsMessage handles messages sent to the groups list window. If the
// value of the list changed based on the message, then a comnmand is sent to
// the processor to re-start watching the file for content.
func (m *Model) handleGroupsMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	origValue := m.groupsModel.SelectedItem()
	m.groupsModel, cmd = m.groupsModel.Update(msg)
	newValue := m.groupsModel.SelectedItem()
	if origValue == newValue {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.reloadContent)
}

// hadleOutputMessage handles messages sent to the output window. If the message
// put us at the bottom of the window then we remember that we are at the bottom
// so we can stay there as new lines are added.
func (m *Model) handleOutputMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.outputModel, cmd = m.outputModel.Update(msg)
	m.atBottom = (m.outputModel.ScrollPercent() == 1.0)
	return m, cmd
}

// footerView returns the view of the footer. It contains the current jq command
// and the current scroll percentage of the output window with enough space
// between them to put the percentage at the right of the screen.
func (m *Model) footerView() string {
	scrollPercent := fmt.Sprintf("%3.f%%", m.outputModel.ScrollPercent()*100)
	spaceCount := m.selectorModel.Width - len(m.jq) - len(scrollPercent)
	if spaceCount < 0 {
		return ""
	}
	return fmt.Sprintf(" %s%s%s", m.jq, strings.Repeat(" ", spaceCount), scrollPercent)
}

// updateGroupWidth sizes the groups window to fit the current list of groups.
// If there is a change then it also resizes the output window and re-formats
// the content in that window.
func (m *Model) updateGroupWidth() {
	currentWidth := m.groupsModel.Width()
	newWidth := getGroupWidth(m.groups)
	if currentWidth != newWidth {
		m.groupsModel.SetWidth(newWidth)
		m.outputModel.Width = m.width - m.groupsModel.Width() - 4
		m.updateOutputModelContent()
	}
}

// updateOutputModelContent re-formats all of the cached content lines for the
// current state of the applicaton (window sizes, line numbers, wrapping, etc).
// This is only necessary because the viewport does not correctly handle scroll
// position when doing its own wrapping.
// (https://github.com/charmbracelet/bubbletea/issues/1017)
func (m *Model) updateOutputModelContent() {
	// reformat all lines
	m.outputContent = make([]string, 0, max(len(m.rawOutputContent), len(m.outputContent)))
	for idx, line := range m.rawOutputContent {
		m.outputContent = append(m.outputContent, formatContentLine(m.wrap, m.lineNumbers, idx+1, m.outputModel.Width, line)...)
	}
	m.outputModel.SetContent(strings.Join(m.outputContent, "\n"))
	if m.atBottom {
		m.outputModel.GotoBottom()
	}
}

// stopProcessor is a tea.Cmd that issues a processor.StopOperation to the
// currently connected processor. This begins the process of stopping the
// application.
func (m *Model) stopProcessor() {
	m.processorCmdChan <- processor.Command{
		Operation: processor.StopOperation,
	}
}

// reloadGroups is a tea.Cmd that issues a processor.StartGroupsOperation to the
// currently connected processor. This begins the process of re-reading groups
// from the file. It returns no message.
func (m *Model) reloadGroups() tea.Msg {
	m.groups = map[string]struct{}{}
	m.groups["*"] = struct{}{}
	m.processorCmdChan <- processor.Command{
		Operation: processor.StartGroupsOperation,
		Selector:  m.selectorModel.Value(),
		Path:      m.path,
	}
	return nil
}

// reloadContent is a tea.Cmd that issues a processor.StartContentOperation to
// the currently connected processor. This begins the process of re-reading
// content from the file. It returns no message.
func (m *Model) reloadContent() tea.Msg {
	m.rawOutputContent = []string{"Loading..."}
	m.outputContent = []string{"Loading..."}
	m.outputModel.SetContent("Loading...")
	selectedItem := m.groupsModel.SelectedItem()
	selectedItemText := "*"
	if selectedItem != nil {
		selectedItemText = selectedItem.FilterValue()
	}
	m.processorCmdChan <- processor.Command{
		Operation: processor.StartContentOperation,
		Selector:  m.selectorModel.Value(),
		Format:    m.formatModel.Value(),
		Group:     selectedItemText,
		Path:      m.path,
	}
	return nil
}

// formatContentLine returns the given line formatted with the given
// characteristics.
func formatContentLine(wrapped, lineNumbers bool, idx, width int, line string) []string {
	if lineNumbers {
		line = fmt.Sprintf("%5d: %s", idx, line)
	}
	if !wrapped {
		return []string{line[:min(len(line), width)]}
	}
	line = ansi.Hardwrap(line, width, true)
	return []string{line}
}

// getGroupItems returns the groups represented by the groups map as a slice of
// list.Item.
func getGroupItems(groups map[string]struct{}) []list.Item {
	var items []list.Item
	for _, k := range slices.Sorted(maps.Keys(groups)) {
		items = append(items, item(k))
	}
	return items
}

func getGroupWidth(items map[string]struct{}) int {
	minWidth := 10
	maxWidth := 100
	width := 0
	for i := range maps.Keys(items) {
		width = max(width, len(i))
	}
	if width < minWidth {
		width = minWidth
	} else if width > maxWidth {
		width = maxWidth
	}
	return width + 3
}
