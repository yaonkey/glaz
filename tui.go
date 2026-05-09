package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type refreshMsg struct{}

type searchDoneMsg struct {
	result SearchResult
}

type dateSwitchedMsg struct {
	err        error
	targetDate string
	actualDate string
	isFallback bool
}

type inputMode int

const (
	inputNone inputMode = iota
	inputSearch
	inputDate
)

type screenMode int

const (
	screenMonitor screenMode = iota
	screenSearch
)

type groupSortMode int

const (
	sortByLast groupSortMode = iota
	sortByLevel
	sortByCount
)

var (
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	highlightStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	secondaryStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	noticeStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	infoStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
	searchTitle      = lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true)
	newErrorTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	levelFromTextRe  = regexp.MustCompile(`(?i)\[(error|critical|crit|alert|emerg|warning|warn|notice|info|debug)\]|\b(error|critical|crit|alert|emerg|warning|warn|notice|info|debug)\b`)
	levelBracketRe   = regexp.MustCompile(`(?i)\[(error|critical|crit|alert|emerg|warning|warn|notice|info|debug)\]`)
)

// model is Bubble Tea application state.
type model struct {
	ctx         context.Context
	coordinator *Coordinator

	width  int
	height int

	meta      SessionMeta
	snapshot  AnalyzerSnapshot
	mode      inputMode
	screen    screenMode
	sortMode  groupSortMode
	inputText string
	search    SearchResult
	info      string

	groupSelected  int
	groupOffset    int
	searchSelected int
	searchOffset   int
}

// RunTUI starts the terminal UI and blocks until program exits.
func RunTUI(ctx context.Context, coordinator *Coordinator) error {
	m := model{
		ctx:         ctx,
		coordinator: coordinator,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Init wires periodic refresh flow.
func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.refreshCmd())
}

// tickCmd schedules one-second refresh ticks.
func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
		return refreshMsg{}
	})
}

// refreshCmd requests a state refresh from coordinator.
func (m model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return refreshMsg{}
	}
}

// Update is the main Bubble Tea update loop.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case refreshMsg:
		m.meta = m.coordinator.Meta()
		m.snapshot = m.coordinator.Snapshot()
		m.sortGroups()
		m.clampSelections()
		return m, tickCmd()

	case searchDoneMsg:
		m.search = msg.result
		return m, nil

	case dateSwitchedMsg:
		if msg.err != nil {
			m.info = "Ошибка переключения даты: " + msg.err.Error()
		} else {
			if msg.isFallback {
				m.info = fmt.Sprintf("Логи за %s не найдены, открыт %s", msg.targetDate, msg.actualDate)
			} else {
				m.info = "Переключено на дату: " + msg.actualDate
			}
			m.search = SearchResult{}
			m.searchSelected = 0
			m.searchOffset = 0
		}
		m.mode = inputNone
		m.inputText = ""
		return m, m.refreshCmd()

	case tea.KeyMsg:
		switch m.mode {
		case inputSearch:
			return m.updateSearchInput(msg)
		case inputDate:
			return m.updateDateInput(msg)
		default:
			return m.updateNormal(msg)
		}
	}

	return m, nil
}

// updateNormal handles key events outside inline input modes.
func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
	case "down", "j":
		m.moveSelection(1)
	case "left", "h":
		if m.screen == screenMonitor {
			m.sortMode = prevSortMode(m.sortMode)
			m.sortGroups()
			m.info = "Сортировка: " + m.sortModeLabel()
		}
	case "right", "l":
		if m.screen == screenMonitor {
			m.sortMode = nextSortMode(m.sortMode)
			m.sortGroups()
			m.info = "Сортировка: " + m.sortModeLabel()
		}
	case "/":
		if m.screen == screenMonitor {
			m.screen = screenSearch
			if strings.TrimSpace(m.search.Keyword) == "" {
				m.mode = inputSearch
				m.inputText = ""
				m.info = "Режим поиска: введите ключевое слово и нажмите Enter"
			} else {
				m.info = "Режим поиска"
			}
		} else {
			m.screen = screenMonitor
			m.mode = inputNone
			m.info = "Режим мониторинга"
		}
	case "d":
		m.mode = inputDate
		m.inputText = m.meta.Date
		m.info = "Введите дату в формате YYYY-MM-DD"
	case "enter":
		if m.screen == screenSearch {
			m.mode = inputSearch
			m.inputText = m.search.Keyword
			m.info = "Введите новый поисковый запрос и нажмите Enter"
		}
	case "r":
		return m, m.refreshCmd()
	}
	return m, nil
}

// updateSearchInput handles search prompt editing and submission.
func (m model) updateSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = inputNone
		m.inputText = ""
		return m, nil
	case "enter":
		query := strings.TrimSpace(m.inputText)
		m.mode = inputNone
		if query == "" {
			m.search = SearchResult{}
			return m, nil
		}
		return m, func() tea.Msg {
			return searchDoneMsg{
				result: m.coordinator.Search(query, 2000),
			}
		}
	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.inputText += msg.String()
		}
	}
	return m, nil
}

// updateDateInput handles date switch prompt editing and submission.
func (m model) updateDateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = inputNone
		m.inputText = ""
		return m, nil
	case "enter":
		date := strings.TrimSpace(m.inputText)
		return m, func() tea.Msg {
			actualDate, fallbackDate, err := m.coordinator.SwitchDate(date)
			return dateSwitchedMsg{
				err:        err,
				targetDate: date,
				actualDate: actualDate,
				isFallback: fallbackDate != "",
			}
		}
	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.inputText += msg.String()
		}
	}
	return m, nil
}

// View renders a full frame for the current screen mode.
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Инициализация..."
	}

	var out strings.Builder
	out.WriteString(titleStyle.Render("Glaz — мониторинг логов"))
	out.WriteByte('\n')

	modeText := "архив"
	if m.meta.Live {
		modeText = "realtime"
	}
	screen := "мониторинг"
	if m.screen == screenSearch {
		screen = "поиск"
	}
	out.WriteString(secondaryStyle.Render(fmt.Sprintf(
		"Дата: %s | Режим: %s | Экран: %s | Файлов: %d | Директорий: %d",
		m.meta.Date,
		modeText,
		screen,
		len(m.meta.Files),
		len(m.meta.Dirs),
	)))
	out.WriteByte('\n')
	out.WriteString(secondaryStyle.Render("Управление: ↑/↓ список, ←/→ сортировка (monitor), / экран, Enter в поиске — запрос, d дата, r обновить, q выход"))
	out.WriteString("\n\n")

	if m.screen == screenMonitor {
		out.WriteString(m.renderGroups())
		out.WriteString("\n\n")
		out.WriteString(m.renderNewErrors())
	} else {
		out.WriteString(m.renderSearch())
	}

	if m.mode == inputSearch {
		out.WriteString("\n\n")
		out.WriteString(searchTitle.Render("Поиск > " + m.inputText))
	} else if m.mode == inputDate {
		out.WriteString("\n\n")
		out.WriteString(searchTitle.Render("Дата > " + m.inputText))
	}

	if m.info != "" {
		out.WriteString("\n\n")
		out.WriteString(secondaryStyle.Render(m.info))
	}
	if m.meta.LastError != "" {
		out.WriteString("\n")
		out.WriteString(errorStyle.Render("Последняя ошибка: " + m.meta.LastError))
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(out.String())
}

// renderGroups renders aggregated errors table for monitor screen.
func (m model) renderGroups() string {
	var out strings.Builder
	out.WriteString(highlightStyle.Render("Группировка ошибок"))
	out.WriteByte('\n')
	out.WriteString(secondaryStyle.Render("Сортировка: " + m.sortModeLabel()))
	out.WriteByte('\n')

	if len(m.snapshot.Groups) == 0 {
		out.WriteString(secondaryStyle.Render("Данные пока не найдены"))
		return out.String()
	}

	out.WriteString(secondaryStyle.Render("   LAST                 LVL      COUNT  TITLE"))
	out.WriteByte('\n')

	maxRows := max(5, m.height-16)
	start, end := window(len(m.snapshot.Groups), m.groupSelected, m.groupOffset, maxRows)
	for i := start; i < end; i++ {
		row := m.snapshot.Groups[i]
		prefix := " "
		if i == m.groupSelected {
			prefix = ">"
		}
		line := fmt.Sprintf(
			"%s  %-19s %-6s %-6d %s",
			prefix,
			row.LastSeen.Format("15:04:05 2006-01-02"),
			formatLevel(row.Level),
			row.Count,
			row.Title,
		)
		if i == m.groupSelected {
			line = selectedRowStyle.Render(line)
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteString(secondaryStyle.Render(fmt.Sprintf("Показано %d-%d из %d", start+1, end, len(m.snapshot.Groups))))

	return strings.TrimRight(out.String(), "\n")
}

// renderNewErrors renders the latest "first-seen" error groups.
func (m model) renderNewErrors() string {
	var out strings.Builder
	out.WriteString(newErrorTitle.Render("Новые ошибки (первые появления)"))
	out.WriteByte('\n')
	if len(m.snapshot.NewEvents) == 0 {
		out.WriteString(secondaryStyle.Render("Нет новых ошибок в текущей сессии"))
		return out.String()
	}

	for i, item := range m.snapshot.NewEvents {
		if i >= 4 {
			out.WriteString(secondaryStyle.Render(fmt.Sprintf("... ещё %d новых ошибок", len(m.snapshot.NewEvents)-4)))
			break
		}
		full := item.FullText
		if len(full) > 240 {
			full = full[:240] + "..."
		}
		out.WriteString(fmt.Sprintf(
			"[%s] %s (%s)\n%s\n",
			item.LastSeen.Format("15:04:05"),
			item.Title,
			formatLevel(item.Level),
			full,
		))
	}
	return strings.TrimRight(out.String(), "\n")
}

// renderSearch renders search mode with scrollable hit list.
func (m model) renderSearch() string {
	if m.search.Keyword == "" {
		return secondaryStyle.Render("Поиск: нажмите Enter, введите запрос и нажмите Enter ещё раз")
	}

	var out strings.Builder
	out.WriteString(searchTitle.Render(fmt.Sprintf("Поиск `%s`: совпадений %d", m.search.Keyword, m.search.Total)))
	out.WriteByte('\n')
	if m.search.Truncated {
		out.WriteString(warnStyle.Render(fmt.Sprintf("Слишком много совпадений, показаны первые %d строк", len(m.search.Hits))))
		out.WriteByte('\n')
	}
	if len(m.search.Hits) == 0 {
		out.WriteString(secondaryStyle.Render("Совпадений не найдено"))
		return out.String()
	}
	maxRows := max(5, m.height-12)
	start, end := window(len(m.search.Hits), m.searchSelected, m.searchOffset, maxRows)
	for i := start; i < end; i++ {
		prefix := " "
		if i == m.searchSelected {
			prefix = ">"
		}
		line := prefix + " " + colorizeSearchHit(m.search.Hits[i])
		if i == m.searchSelected {
			line = selectedRowStyle.Render(line)
		}
		out.WriteString(line + "\n")
	}
	out.WriteString(secondaryStyle.Render(fmt.Sprintf("Показано %d-%d из %d", start+1, end, len(m.search.Hits))))
	return strings.TrimRight(out.String(), "\n")
}

// clampSelections keeps selected indexes within valid list ranges.
func (m *model) clampSelections() {
	if m.groupSelected >= len(m.snapshot.Groups) && len(m.snapshot.Groups) > 0 {
		m.groupSelected = len(m.snapshot.Groups) - 1
	}
	if m.groupSelected < 0 {
		m.groupSelected = 0
	}
	if m.searchSelected >= len(m.search.Hits) && len(m.search.Hits) > 0 {
		m.searchSelected = len(m.search.Hits) - 1
	}
	if m.searchSelected < 0 {
		m.searchSelected = 0
	}
}

// sortGroups applies user-selected ordering to monitor table.
func (m *model) sortGroups() {
	sort.SliceStable(m.snapshot.Groups, func(i, j int) bool {
		left := m.snapshot.Groups[i]
		right := m.snapshot.Groups[j]

		switch m.sortMode {
		case sortByLevel:
			lr, rr := levelRank(left.Level), levelRank(right.Level)
			if lr == rr {
				return left.LastSeen.After(right.LastSeen)
			}
			return lr < rr
		case sortByCount:
			if left.Count == right.Count {
				return left.LastSeen.After(right.LastSeen)
			}
			return left.Count > right.Count
		case sortByLast:
			fallthrough
		default:
			if left.LastSeen.Equal(right.LastSeen) {
				return left.Count > right.Count
			}
			return left.LastSeen.After(right.LastSeen)
		}
	})
}

// moveSelection moves cursor in the active list.
func (m *model) moveSelection(delta int) {
	if m.screen == screenMonitor {
		if len(m.snapshot.Groups) == 0 {
			return
		}
		m.groupSelected += delta
		if m.groupSelected < 0 {
			m.groupSelected = 0
		}
		if m.groupSelected >= len(m.snapshot.Groups) {
			m.groupSelected = len(m.snapshot.Groups) - 1
		}
		return
	}

	if len(m.search.Hits) == 0 {
		return
	}
	m.searchSelected += delta
	if m.searchSelected < 0 {
		m.searchSelected = 0
	}
	if m.searchSelected >= len(m.search.Hits) {
		m.searchSelected = len(m.search.Hits) - 1
	}
}

// sortModeLabel returns compact UI label for current sort mode.
func (m model) sortModeLabel() string {
	switch m.sortMode {
	case sortByLevel:
		return "lvl"
	case sortByCount:
		return "count"
	case sortByLast:
		fallthrough
	default:
		return "last"
	}
}

// nextSortMode cycles sort mode forward.
func nextSortMode(current groupSortMode) groupSortMode {
	switch current {
	case sortByLast:
		return sortByLevel
	case sortByLevel:
		return sortByCount
	case sortByCount:
		return sortByLast
	default:
		return sortByLast
	}
}

// prevSortMode cycles sort mode backward.
func prevSortMode(current groupSortMode) groupSortMode {
	switch current {
	case sortByLast:
		return sortByCount
	case sortByLevel:
		return sortByLast
	case sortByCount:
		return sortByLevel
	default:
		return sortByLast
	}
}

// window computes visible list range for scrollable rendering.
func window(total, selected, offset, size int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	if size <= 0 {
		size = 10
	}
	if selected < offset {
		offset = selected
	}
	if selected >= offset+size {
		offset = selected - size + 1
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + size
	if end > total {
		end = total
		offset = max(0, end-size)
	}
	return offset, end
}

// levelRank maps severities to deterministic sort priority.
func levelRank(level string) int {
	switch strings.ToLower(level) {
	case "emerg", "alert", "critical", "crit", "error":
		return 0
	case "warning", "warn":
		return 1
	case "notice":
		return 2
	case "info":
		return 3
	case "debug":
		return 4
	default:
		return 5
	}
}

// formatLevel colorizes severity labels for monitor screen.
func formatLevel(level string) string {
	upper := strings.ToUpper(level)
	switch strings.ToLower(level) {
	case "error", "crit", "critical", "alert", "emerg":
		return errorStyle.Render(upper)
	case "warning", "warn":
		return warnStyle.Render(upper)
	case "notice":
		return noticeStyle.Render(upper)
	case "info", "debug":
		return infoStyle.Render(upper)
	default:
		return secondaryStyle.Render(upper)
	}
}

// colorizeSearchHit highlights bracketed severity token in search rows.
func colorizeSearchHit(hit string) string {
	return levelBracketRe.ReplaceAllStringFunc(hit, func(token string) string {
		inner := strings.Trim(token, "[]")
		switch strings.ToLower(inner) {
		case "error", "critical", "crit", "alert", "emerg":
			return errorStyle.Render(token)
		case "warning", "warn":
			return warnStyle.Render(token)
		case "notice":
			return noticeStyle.Render(token)
		case "info", "debug":
			return infoStyle.Render(token)
		default:
			return token
		}
	})
}
