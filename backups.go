package main

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupWorkspace zips the data file into Backups/ once per day and prunes old copies.
func backupWorkspace(ws string) error {
	dir := filepath.Join(ws, "Backups")
	stamp := time.Now().Format("20060102")
	target := filepath.Join(dir, "backup_"+stamp+".zip")
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if err := writeBackupZip(ws, target); err != nil {
		return err
	}
	return pruneBackups(dir, 14)
}

func writeBackupZip(ws, target string) error {
	src := filepath.Join(ws, "workspace.cbk")
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := target + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	entry, err := zw.Create("workspace.cbk")
	if err == nil {
		_, err = io.Copy(entry, in)
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "backup_") && strings.HasSuffix(e.Name(), ".zip") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return nil
}

func (s *server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	target := filepath.Join(s.ws, "Backups", "backup_"+time.Now().Format("20060102_150405")+".zip")
	if err := writeBackupZip(s.ws, target); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	pruneBackups(filepath.Join(s.ws, "Backups"), 14)
	writeJSON(w, 200, map[string]any{"ok": true, "file": target})
}
