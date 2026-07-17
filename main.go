package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const appVersion = "0.1.0-m0"

//go:embed web
var webFS embed.FS

func parseDate(d string) (time.Time, error) {
	return time.Parse("2006-01-02", d)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func main() {
	wsFlag := flag.String("workspace", "", "workspace folder (default: folder containing the executable)")
	portFlag := flag.Int("port", 8484, "preferred port")
	lan := flag.Bool("lan", false, "listen on all interfaces so teammates can connect")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser automatically")
	flag.Parse()

	ws, ok := resolveWorkspaceInteractive(*wsFlag)
	if !ok {
		fmt.Println("No workspace folder chosen — nothing to do. Run again and pick a folder.")
		return
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		alertDialog("Cannot create the workspace folder:\n" + err.Error())
		log.Fatalf("cannot create workspace folder: %v", err)
	}
	for _, d := range []string{"Records", "Backups", "Imports"} {
		_ = os.MkdirAll(filepath.Join(ws, d), 0o755)
	}

	db, err := openStore(filepath.Join(ws, "workspace.cbk"))
	if err != nil {
		log.Fatalf("cannot open workspace data: %v", err)
	}
	defer db.Close()

	s := &server{db: db, ws: ws}

	go func() {
		for {
			// one panicking cycle must not kill the engine for the rest of the process
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("engine cycle panic (recovered): %v", r)
					}
				}()
				runSpawner(db)
				runArchiver(db, 7)
				if getMeta(db, "setup_done") == "1" {
					if err := writeRecords(s); err != nil {
						log.Printf("records snapshot: %v", err)
					}
					if err := backupWorkspace(s.ws); err != nil {
						log.Printf("backup: %v", err)
					}
				}
			}()
			time.Sleep(time.Hour)
		}
	}()
	mux := http.NewServeMux()
	s.routes(mux)

	webRoot, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(webRoot))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(webRoot, r.URL.Path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		b, _ := fs.ReadFile(webRoot, "index.html")
		w.Write(b)
	})

	host := "127.0.0.1"
	if *lan {
		host = "0.0.0.0"
	}
	var listener net.Listener
	var port int
	for p := *portFlag; p <= *portFlag+10; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, p))
		if err == nil {
			listener, port = l, p
			break
		}
	}
	if listener == nil {
		log.Fatalf("no free port between %d and %d", *portFlag, *portFlag+10)
	}

	url := fmt.Sprintf("http://localhost:%d", port)
	fmt.Println()
	fmt.Println("  Casebook " + appVersion)
	fmt.Println("  Workspace: " + ws)
	fmt.Println("  Open:      " + url)
	if *lan {
		fmt.Println("  Teammates: http://<this-computer-name>:" + fmt.Sprint(port))
	} else {
		fmt.Println("  (single-user mode; restart with -lan to let teammates connect)")
	}
	fmt.Println()

	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}
	log.Fatal(http.Serve(listener, mux))
}
