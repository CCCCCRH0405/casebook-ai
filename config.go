package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// configPath returns the file that remembers which workspace folder to open.
func configPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "Casebook", "workspace.path")
}

func savedWorkspace() string {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return ""
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return ""
	}
	return p
}

func rememberWorkspace(path string) {
	cp := configPath()
	os.MkdirAll(filepath.Dir(cp), 0o755)
	os.WriteFile(cp, []byte(path), 0o644)
}

// runningAsBundle reports whether the executable lives somewhere a workspace
// should not be written next to it (a macOS .app, /Applications, a temp dir).
func runningAsBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if strings.Contains(exe, ".app/Contents/MacOS/") {
		return true
	}
	if strings.HasPrefix(exe, "/Applications/") || strings.HasPrefix(exe, os.TempDir()) {
		return true
	}
	dir := filepath.Dir(exe)
	probe := filepath.Join(dir, ".cb_write_probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		return true // exe dir is not writable, so we must ask for a folder
	}
	os.Remove(probe)
	return false
}

// chooseFolderDialog shows the OS-native "choose folder" picker and returns the
// selected path, or "" if the user cancelled or no dialog is available.
func chooseFolderDialog() string {
	switch runtime.GOOS {
	case "darwin":
		script := `POSIX path of (choose folder with prompt "Choose a folder for your Casebook workspace. Your data will live here.")`
		out, err := exec.Command("osascript", "-e", script).Output()
		if err != nil {
			return ""
		}
		return strings.TrimRight(strings.TrimSpace(string(out)), "/")
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$f = New-Object System.Windows.Forms.FolderBrowserDialog; ` +
			`$f.Description = 'Choose a folder for your Casebook workspace'; ` +
			`if ($f.ShowDialog() -eq 'OK') { [Console]::Out.Write($f.SelectedPath) }`
		out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	default:
		return ""
	}
}

// alertDialog shows a small native message box (best-effort, used for fatal
// errors when there is no console to read).
func alertDialog(msg string) {
	switch runtime.GOOS {
	case "darwin":
		script := `display dialog "` + strings.ReplaceAll(msg, `"`, `'`) + `" buttons {"OK"} with icon caution with title "Casebook"`
		exec.Command("osascript", "-e", script).Run()
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`[System.Windows.Forms.MessageBox]::Show('` + strings.ReplaceAll(msg, "'", "`'") + `','Casebook')`
		exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
	}
}

// resolveWorkspaceInteractive picks the workspace folder, asking the user with a
// native dialog the first time the app is run from a bundle.
func resolveWorkspaceInteractive(flagVal string) (string, bool) {
	if flagVal != "" {
		return flagVal, true
	}
	if saved := savedWorkspace(); saved != "" {
		return saved, true
	}
	if runningAsBundle() {
		chosen := chooseFolderDialog()
		if chosen == "" {
			return "", false
		}
		rememberWorkspace(chosen)
		return chosen, true
	}
	exe, err := os.Executable()
	if err == nil {
		return filepath.Dir(exe), true
	}
	return "./workspace", true
}
