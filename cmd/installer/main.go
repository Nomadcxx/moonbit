package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Theme colors - Eldritch (Lovecraftian horror inspired)
var (
	BgBase       = lipgloss.Color("#212337") // Sunken Depths Grey
	BgElevated   = lipgloss.Color("#323449") // Shallow Depths Grey
	Primary      = lipgloss.Color("#37f499") // Great Old One Green
	Secondary    = lipgloss.Color("#04d1f9") // Watery Tomb Blue
	Accent       = lipgloss.Color("#a48cf2") // Lovecraft Purple
	FgPrimary    = lipgloss.Color("#ebfafa") // Lighthouse White
	FgSecondary  = lipgloss.Color("#7081d0") // The Old One Purple
	FgMuted      = lipgloss.Color("#7081d0") // The Old One Purple (comments)
	ErrorColor   = lipgloss.Color("#f16c75") // R'lyeh Red
	WarningColor = lipgloss.Color("#f7c67f") // Dreaming Orange
	SuccessColor = lipgloss.Color("#37f499") // Great Old One Green
)

// Styles
var (
	checkMark   = lipgloss.NewStyle().Foreground(SuccessColor).SetString("[OK]")
	failMark    = lipgloss.NewStyle().Foreground(ErrorColor).SetString("[FAIL]")
	skipMark    = lipgloss.NewStyle().Foreground(WarningColor).SetString("[SKIP]")
	headerStyle = lipgloss.NewStyle().Foreground(FgPrimary)
)

type installStep int

const (
	stepWelcome installStep = iota
	stepScheduleSelect
	stepInstalling
	stepComplete
)

type taskStatus int

const (
	statusPending taskStatus = iota
	statusRunning
	statusComplete
	statusFailed
	statusSkipped
)

type installTask struct {
	name        string
	description string
	execute     func(*model) error
	optional    bool
	status      taskStatus
}

type model struct {
	step             installStep
	tasks            []installTask
	currentTaskIndex int
	width            int
	height           int
	spinner          spinner.Model
	errors           []string
	packageManager   string
	uninstallMode    bool
	selectedOption   int // 0 = Install, 1 = Uninstall
	scheduleIndex    int // Schedule selection: 0=Daily, 1=Weekly, 2=Manual
	scheduleName     string
}

type taskCompleteMsg struct {
	index   int
	success bool
	error   string
}

func newModel() model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(Secondary)
	s.Spinner = spinner.Dot

	tasks := []installTask{
		{name: "Check privileges", description: "Checking root access", execute: checkPrivileges, status: statusPending},
		{name: "Check dependencies", description: "Checking system dependencies", execute: checkDependencies, status: statusPending},
		{name: "Build binary", description: "Building moonbit", execute: buildBinary, status: statusPending},
		{name: "Install binary", description: "Installing to /usr/local/bin", execute: installBinary, status: statusPending},
		{name: "Install launcher", description: "Adding moonbit to the app menu", execute: installDesktopEntry, optional: true, status: statusPending},
		{name: "Install systemd", description: "Installing systemd units", execute: installSystemd, optional: true, status: statusPending},
		{name: "Configure schedule", description: "Configuring cleaning schedule", execute: configureSchedule, optional: true, status: statusPending},
		{name: "Enable service", description: "Enabling systemd timers", execute: enableService, optional: true, status: statusPending},
	}

	return model{
		step:             stepWelcome,
		tasks:            tasks,
		currentTaskIndex: -1,
		spinner:          s,
		errors:           []string{},
	}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.step == stepComplete || m.step == stepWelcome {
				return m, tea.Quit
			}
		case "up", "k":
			if m.step == stepWelcome && m.selectedOption > 0 {
				m.selectedOption--
			} else if m.step == stepScheduleSelect && m.scheduleIndex > 0 {
				m.scheduleIndex--
			}
		case "down", "j":
			if m.step == stepWelcome && m.selectedOption < 1 {
				m.selectedOption++
			} else if m.step == stepScheduleSelect {
				schedules := []string{"daemon", "daily", "weekly", "manual"}
				if m.scheduleIndex < len(schedules)-1 {
					m.scheduleIndex++
				}
			}
		case "enter":
			if m.step == stepWelcome {
				// Set mode based on selection
				m.uninstallMode = (m.selectedOption == 1)

				if m.uninstallMode {
					// Uninstall tasks
					m.tasks = []installTask{
						{name: "Check privileges", description: "Checking root access", execute: checkPrivileges, status: statusPending},
						{name: "Disable service", description: "Disabling systemd timers", execute: disableService, status: statusPending},
						{name: "Remove binary", description: "Removing moonbit binary", execute: removeBinary, status: statusPending},
						{name: "Remove launcher", description: "Removing app menu entry", execute: removeDesktopEntry, optional: true, status: statusPending},
						{name: "Remove systemd", description: "Removing systemd files", execute: removeSystemd, optional: true, status: statusPending},
					}
					// Skip schedule selection for uninstall
					m.step = stepInstalling
					m.currentTaskIndex = 0
					m.tasks[0].status = statusRunning
					return m, tea.Batch(
						m.spinner.Tick,
						executeTask(0, &m),
					)
				} else {
					// Go to schedule selection
					m.step = stepScheduleSelect
					return m, nil
				}
			} else if m.step == stepScheduleSelect {
				// Set schedule based on selection
				schedules := []string{"daemon", "daily", "weekly", "manual"}
				m.scheduleName = schedules[m.scheduleIndex]

				// Start installation
				m.step = stepInstalling
				m.currentTaskIndex = 0
				m.tasks[0].status = statusRunning
				return m, tea.Batch(
					m.spinner.Tick,
					executeTask(0, &m),
				)
			} else if m.step == stepComplete {
				return m, tea.Quit
			}
		}

	case taskCompleteMsg:
		// Update task status
		if msg.success {
			m.tasks[msg.index].status = statusComplete
		} else {
			if m.tasks[msg.index].optional {
				m.tasks[msg.index].status = statusSkipped
				m.errors = append(m.errors, fmt.Sprintf("%s (skipped): %s", m.tasks[msg.index].name, msg.error))
			} else {
				m.tasks[msg.index].status = statusFailed
				m.errors = append(m.errors, fmt.Sprintf("%s: %s", m.tasks[msg.index].name, msg.error))
				m.step = stepComplete
				return m, nil
			}
		}

		// Move to next task
		m.currentTaskIndex++
		if m.currentTaskIndex >= len(m.tasks) {
			m.step = stepComplete
			return m, nil
		}

		// Start next task
		m.tasks[m.currentTaskIndex].status = statusRunning
		return m, executeTask(m.currentTaskIndex, &m)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content strings.Builder

	// ASCII Header - sysc-greet pattern: style entire block, then use JoinVertical
	// Per-line Render() causes JoinVertical to miscalculate widths
	asciiArt := `█▀▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ █▄  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀`

	// Apply styling to entire ASCII block at once
	styledASCII := headerStyle.Render(asciiArt)

	// Use JoinVertical to center - it handles each line correctly when block is pre-styled
	centeredASCII := lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Center).
		Render(styledASCII)

	content.WriteString(centeredASCII)
	content.WriteString("\n\n")

	// Main content based on step
	var mainContent string
	switch m.step {
	case stepWelcome:
		mainContent = m.renderWelcome()
	case stepScheduleSelect:
		mainContent = m.renderScheduleSelect()
	case stepInstalling:
		mainContent = m.renderInstalling()
	case stepComplete:
		mainContent = m.renderComplete()
	}

	// Wrap in border (no background for TUI consistency)
	mainStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Primary).
		Foreground(FgPrimary).
		Width(m.width - 4)
	content.WriteString(mainStyle.Render(mainContent))
	content.WriteString("\n")

	// Help text
	helpText := m.getHelpText()
	if helpText != "" {
		helpStyle := lipgloss.NewStyle().
			Foreground(FgMuted).
			Italic(true).
			Align(lipgloss.Center)
		content.WriteString("\n" + helpStyle.Render(helpText))
	}

	// Wrap everything with centering (no background)
	wrapStyle := lipgloss.NewStyle().
		Foreground(FgPrimary).
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Top)

	return wrapStyle.Render(content.String())
}

func (m model) renderWelcome() string {
	var b strings.Builder

	b.WriteString("Select an option:\n\n")

	// Install option
	installPrefix := "  "
	if m.selectedOption == 0 {
		installPrefix = lipgloss.NewStyle().Foreground(Primary).Render("> ")
	}
	b.WriteString(installPrefix + "Install moonbit\n")
	b.WriteString("    Builds binary, installs to system, configures automation\n\n")

	// Uninstall option
	uninstallPrefix := "  "
	if m.selectedOption == 1 {
		uninstallPrefix = lipgloss.NewStyle().Foreground(Primary).Render("> ")
	}
	b.WriteString(uninstallPrefix + "Uninstall moonbit\n")
	b.WriteString("    Removes moonbit from your system\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(FgMuted).Render("Requires root privileges"))

	return b.String()
}

func (m model) renderScheduleSelect() string {
	var b strings.Builder

	b.WriteString("Configure auto-cleaning schedule:\n\n")

	schedules := []struct {
		name string
		desc string
	}{
		{"daemon", "Long-running background service (systemd)"},
		{"daily", "Scan daily at 2 AM, clean weekly on Sunday at 3 AM"},
		{"weekly", "Scan and clean weekly on Sunday at 3 AM"},
		{"manual", "No automation, run moonbit manually as needed"},
	}

	for i, sched := range schedules {
		prefix := "  "
		if i == m.scheduleIndex {
			prefix = lipgloss.NewStyle().Foreground(Primary).Render("> ")
		}
		b.WriteString(prefix + strings.ToUpper(sched.name[:1]) + sched.name[1:] + "\n")
		b.WriteString("    " + sched.desc + "\n\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(FgMuted).Render("You can always run 'sudo moonbit' manually anytime"))

	return b.String()
}

func (m model) renderInstalling() string {
	var b strings.Builder

	// Render all tasks with their current status
	for i, task := range m.tasks {
		var line string
		switch task.status {
		case statusPending:
			line = lipgloss.NewStyle().Foreground(FgMuted).Render("  " + task.name)
		case statusRunning:
			line = m.spinner.View() + " " + lipgloss.NewStyle().Foreground(Secondary).Render(task.description)
		case statusComplete:
			line = checkMark.String() + " " + task.name
		case statusFailed:
			line = failMark.String() + " " + task.name
		case statusSkipped:
			line = skipMark.String() + " " + task.name
		}

		b.WriteString(line)
		if i < len(m.tasks)-1 {
			b.WriteString("\n")
		}
	}

	// Show errors at bottom if any
	if len(m.errors) > 0 {
		b.WriteString("\n\n")
		for _, err := range m.errors {
			b.WriteString(lipgloss.NewStyle().Foreground(WarningColor).Render(err))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m model) renderComplete() string {
	// Check for critical failures
	hasCriticalFailure := false
	for _, task := range m.tasks {
		if task.status == statusFailed && !task.optional {
			hasCriticalFailure = true
			break
		}
	}

	if hasCriticalFailure {
		return lipgloss.NewStyle().Foreground(ErrorColor).Render(
			"Installation failed.\nCheck errors above.\n\nPress Enter to exit")
	}

	// Success
	if m.uninstallMode {
		// Be explicit about what was NOT removed. Backups can hold the only
		// remaining copy of files moonbit deleted, so keeping them is correct --
		// but "moonbit has been removed" overstated what happened.
		return `Uninstall complete.

Removed:
  /usr/local/bin/moonbit, /usr/bin/moonbit
  /etc/systemd/system/moonbit-*.{service,timer}
  /usr/share/applications/moonbit.desktop and its icon

Kept (may hold the only copy of deleted files, or your config):
  /var/log/moonbit           audit log
  /var/lib/moonbit           daemon state
  ~/.config/moonbit          configuration
  ~/.cache/moonbit           scan cache
  ~/.local/share/moonbit     backups of previously deleted files

To remove those too:
  sudo rm -rf /var/log/moonbit /var/lib/moonbit
  rm -rf ~/.config/moonbit ~/.cache/moonbit ~/.local/share/moonbit

Press Enter to exit`
	}

	scheduleMsg := ""
	switch m.scheduleName {
	case "daemon":
		scheduleMsg = "Daemon mode installed and started:\n  - Long-running background service (systemd)\n\n"
	case "daily":
		scheduleMsg = "Automated cleaning scheduled:\n  - Scan: Daily at 2 AM\n  - Clean: Weekly on Sunday at 3 AM\n\n"
	case "weekly":
		scheduleMsg = "Automated cleaning scheduled:\n  - Scan & Clean: Weekly on Sunday at 3 AM\n\n"
	case "manual":
		scheduleMsg = "No automation configured.\nRun 'sudo moonbit' manually as needed.\n\n"
	}

	return fmt.Sprintf(`Installation complete!

%sYou can now run 'moonbit' from any terminal.
If root access is needed, you'll be prompted for your password.

Press Enter to exit`, scheduleMsg)
}

func (m model) getHelpText() string {
	switch m.step {
	case stepWelcome:
		return "↑/↓: Navigate  |  Enter: Continue  |  Ctrl+C: Quit"
	case stepScheduleSelect:
		return "↑/↓: Navigate  |  Enter: Continue  |  Ctrl+C: Quit"
	case stepComplete:
		return "Enter: Exit  |  Ctrl+C: Quit"
	default:
		return "Ctrl+C: Cancel"
	}
}

func executeTask(index int, m *model) tea.Cmd {
	return func() tea.Msg {
		// Simulate work delay for visibility
		time.Sleep(200 * time.Millisecond)

		err := m.tasks[index].execute(m)

		if err != nil {
			return taskCompleteMsg{
				index:   index,
				success: false,
				error:   err.Error(),
			}
		}

		return taskCompleteMsg{
			index:   index,
			success: true,
		}
	}
}

// Task execution functions

func checkPrivileges(m *model) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("root privileges required - run with sudo")
	}
	return nil
}

func checkDependencies(m *model) error {
	missing := []string{}

	// Check critical deps. `make` belongs here: buildBinary shells out to
	// `make build`, which is the single source of truth for the version LDFLAGS.
	// Without it the user is told dependencies are satisfied and then hits
	// "make: command not found" two tasks later.
	for _, dep := range []struct{ bin, report string }{
		{"go", "go"},
		{"make", "make (Arch: base-devel, Debian: build-essential)"},
		{"systemctl", "systemd"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			missing = append(missing, dep.report)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing: %s", strings.Join(missing, ", "))
	}

	return nil
}

func buildBinary(m *model) error {
	cmd := exec.Command("make", "build")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %v - %s", err, string(output))
	}

	// Verify binary was created
	if _, err := os.Stat("moonbit"); err != nil {
		return fmt.Errorf("binary not found after build (run installer from moonbit project root)")
	}

	return nil
}

func installBinary(m *model) error {
	// Verify binary exists
	if _, err := os.Stat("moonbit"); err != nil {
		return fmt.Errorf("moonbit binary not found (run build first)")
	}

	// Copy binary to /usr/local/bin
	cmd := exec.Command("install", "-m", "755", "moonbit", "/usr/local/bin/moonbit")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install binary: %v", err)
	}

	// Verify installation
	if _, err := os.Stat("/usr/local/bin/moonbit"); err != nil {
		return fmt.Errorf("failed to verify binary installation")
	}

	return nil
}

// installDesktopEntry puts moonbit in the application launcher.
//
// The entry runs `pkexec /usr/local/bin/moonbit` with Terminal=true: polkit
// shows a graphical password prompt, then the TUI opens in a terminal already
// elevated. The path must be absolute because pkexec sanitises PATH to
// /usr/sbin:/usr/bin:/sbin:/bin, which does not include /usr/local/bin.
func installDesktopEntry(m *model) error {
	for _, f := range []struct{ src, dst string }{
		{"packaging/moonbit.desktop", "/usr/share/applications/moonbit.desktop"},
		{"packaging/moonbit.svg", "/usr/share/icons/hicolor/scalable/apps/moonbit.svg"},
	} {
		if _, err := os.Stat(f.src); err != nil {
			return fmt.Errorf("%s not found (run installer from moonbit project root)", f.src)
		}
		if err := os.MkdirAll(filepath.Dir(f.dst), 0755); err != nil {
			return fmt.Errorf("failed to create %s: %v", filepath.Dir(f.dst), err)
		}
		if err := exec.Command("install", "-m", "644", f.src, f.dst).Run(); err != nil {
			return fmt.Errorf("failed to install %s: %v", f.dst, err)
		}
	}

	// Refresh the caches so the entry appears without a re-login. Both are
	// best-effort; a missing tool is not a failed install.
	_ = exec.Command("update-desktop-database", "/usr/share/applications").Run()
	_ = exec.Command("gtk-update-icon-cache", "-qtf", "/usr/share/icons/hicolor").Run()

	return nil
}

// systemdUnitFiles is every unit the installer places in /etc/systemd/system.
// It must list every unit under systemd/ -- a unit that ships in the repo but is
// missing here cannot be enabled later from the TUI's Schedule screen.
func systemdUnitFiles() []string {
	return []string{
		"systemd/moonbit-scan.service",
		"systemd/moonbit-scan.timer",
		"systemd/moonbit-clean.service",
		"systemd/moonbit-clean.timer",
		"systemd/moonbit-daemon.service",
	}
}

func installSystemd(m *model) error {
	// Install every unit file regardless of the chosen schedule.
	//
	// Timer mode and daemon mode are mutually exclusive at RUNTIME, not at
	// install time. Installing a unit file does not enable it. Skipping the
	// install for daemon/manual mode left the timer units absent from
	// /etc/systemd/system, so the TUI's Schedule screen -- which exists
	// precisely to switch between modes later -- failed with a bare
	// "exit status 1" when the user tried to enable them.
	files := systemdUnitFiles()

	// Verify source files exist
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("systemd file not found: %s (run installer from moonbit project root)", file)
		}
	}

	// Copy files to /etc/systemd/system/
	for _, file := range files {
		target := "/etc/systemd/system/" + strings.TrimPrefix(file, "systemd/")
		cmd := exec.Command("install", "-m", "644", file, target)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install %s: %v", file, err)
		}
	}

	// Reload systemd
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %v", err)
	}

	return nil
}

func configureSchedule(m *model) error {
	switch m.scheduleName {
	case "manual":
		return nil // No configuration needed for manual mode
	case "daemon":
		return nil // Daemon service file already installed, just needs enablement
	default:
		// For daily/weekly modes, timers will be enabled in enableService
		return nil
	}
}

func enableService(m *model) error {
	switch m.scheduleName {
	case "manual":
		return nil
	case "daemon":
		return enableDaemon()
	default:
		return enableTimers(m.scheduleName)
	}
}

func enableTimers(scheduleName string) error {
	var timers []string
	switch scheduleName {
	case "daily":
		timers = []string{"moonbit-scan.timer", "moonbit-clean.timer"}
	case "weekly":
		timers = []string{"moonbit-clean.timer"}
	}

	for _, timer := range timers {
		out, err := exec.Command("systemctl", "enable", "--now", timer).CombinedOutput()
		if err != nil {
			if msg := strings.TrimSpace(string(out)); msg != "" {
				return fmt.Errorf("failed to enable %s: %v: %s", timer, err, msg)
			}
			return fmt.Errorf("failed to enable %s: %v", timer, err)
		}
	}

	return nil
}

func enableDaemon() error {
	// installSystemd runs earlier in the task list and has already placed the
	// unit in /etc/systemd/system.
	if _, err := os.Stat("/etc/systemd/system/moonbit-daemon.service"); err != nil {
		return fmt.Errorf("daemon unit was not installed: %v", err)
	}

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", "moonbit-daemon.service"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to run %v: %s: %w", args, output, err)
		}
	}

	return nil
}

// Uninstall functions

func disableService(m *model) error {
	timers := []string{"moonbit-scan.timer", "moonbit-clean.timer"}

	for _, timer := range timers {
		cmd := exec.Command("systemctl", "disable", "--now", timer)
		_ = cmd.Run() // Ignore errors, timer might not exist
	}

	// Also disable daemon if it exists
	cmd := exec.Command("systemctl", "disable", "--now", "moonbit-daemon.service")
	_ = cmd.Run() // Ignore errors

	return nil
}

func removeBinary(m *model) error {
	// /usr/local/bin is where this installer puts it; /usr/bin is where the
	// distro package puts it. Remove whichever are present.
	for _, path := range []string{"/usr/local/bin/moonbit", "/usr/bin/moonbit"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %v", path, err)
		}
	}
	return nil
}

func removeDesktopEntry(m *model) error {
	for _, f := range []string{
		"/usr/share/applications/moonbit.desktop",
		"/usr/share/icons/hicolor/scalable/apps/moonbit.svg",
	} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %v", f, err)
		}
	}
	_ = exec.Command("update-desktop-database", "/usr/share/applications").Run()
	_ = exec.Command("gtk-update-icon-cache", "-qtf", "/usr/share/icons/hicolor").Run()
	return nil
}

func removeSystemd(m *model) error {
	files := []string{
		"/etc/systemd/system/moonbit-scan.service",
		"/etc/systemd/system/moonbit-scan.timer",
		"/etc/systemd/system/moonbit-clean.service",
		"/etc/systemd/system/moonbit-clean.timer",
		"/etc/systemd/system/moonbit-daemon.service",
	}

	for _, file := range files {
		_ = os.Remove(file) // Ignore errors
	}

	// Reload systemd
	cmd := exec.Command("systemctl", "daemon-reload")
	_ = cmd.Run() // Ignore errors

	return nil
}

func main() {
	// Check for root privileges before starting TUI
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Error: This installer requires root privileges.")
		fmt.Fprintln(os.Stderr, "Please run with sudo:")
		fmt.Fprintln(os.Stderr, "  sudo ./moonbit-installer")
		os.Exit(1)
	}

	p := tea.NewProgram(newModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running installer: %v\n", err)
		os.Exit(1)
	}
}
