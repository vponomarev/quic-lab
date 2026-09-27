// quic-lab-capture is an on-demand URI handler. It runs no background service.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type settings struct {
	Servers []string `json:"servers"`
}

func canonical(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/") || u.RawPath != "" || strings.Contains(u.Path, "..") {
		return "", errors.New("server must be an HTTPS base URL ending with /, without credentials, query or fragment")
	}
	return u.String(), nil
}
func launch(raw string) (base, id, ticket string, err error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "quic-lab" || u.Host != "capture" || u.User != nil || u.Path != "" {
		err = errors.New("invalid QUIC Lab capture link")
		return
	}
	base, err = canonical(u.Query().Get("server"))
	if err != nil {
		return
	}
	id = u.Query().Get("id")
	ticket = u.Fragment
	valid := regexp.MustCompile(`^[a-f0-9]{64}$`)
	if !valid.MatchString(id) || !valid.MatchString(ticket) {
		err = errors.New("invalid capture ticket")
	}
	return
}
func configPath() (string, error) {
	d, e := os.UserConfigDir()
	return filepath.Join(d, "quic-lab-capture", "trusted.json"), e
}
func trust(raw string) error {
	base, e := canonical(raw)
	if e != nil {
		return e
	}
	p, e := configPath()
	if e != nil {
		return e
	}
	var c settings
	b, e := os.ReadFile(p)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if len(b) > 0 {
		if e = json.Unmarshal(b, &c); e != nil {
			return e
		}
	}
	for _, s := range c.Servers {
		if s == base {
			return nil
		}
	}
	c.Servers = append(c.Servers, base)
	b, _ = json.MarshalIndent(c, "", "  ")
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	return os.WriteFile(p, b, 0600)
}
func trusted(base string) bool {
	p, e := configPath()
	if e != nil {
		return false
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return false
	}
	var c settings
	if json.Unmarshal(b, &c) != nil {
		return false
	}
	for _, s := range c.Servers {
		if s == base {
			return true
		}
	}
	return false
}
func findWireshark(explicit string) (string, error) {
	if explicit != "" {
		resolved, err := exec.LookPath(explicit)
		if err != nil {
			return "", err
		}
		name := strings.ToLower(filepath.Base(resolved))
		if name == "wiresharkportable64.exe" || name == "wiresharkportable.exe" {
			return exec.LookPath(filepath.Join(filepath.Dir(resolved), "App", "Wireshark", "Wireshark.exe"))
		}
		return resolved, nil
	}
	if p, e := exec.LookPath("wireshark"); e == nil {
		return p, nil
	}
	candidates := []string{"/Applications/Wireshark.app/Contents/MacOS/Wireshark", filepath.Join(os.Getenv("ProgramFiles"), "Wireshark", "Wireshark.exe")}
	for _, p := range candidates {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("install Wireshark first, or pass -wireshark with its executable path")
}

var directLaunch bool

func run() error {
	uri := flag.String("open", "", "quic-lab:// link (one-time launch ticket)")
	connect := flag.String("connect", "", "manually supplied one-time link; no installation or saved trust required")
	allow := flag.String("trust-server", "", "explicitly trust an HTTPS lab base URL")
	shark := flag.String("wireshark", "", "Wireshark executable; default: auto-detect")
	dest := flag.String("output-dir", "", "capture directory; default: ~/QUIC Lab Captures")
	saveOnly := flag.Bool("save-only", false, "save capture without launching Wireshark")
	flag.Parse()
	directLaunch = *connect != ""
	if directLaunch {
		if *uri != "" || *allow != "" {
			return errors.New("use -connect alone, without -open or -trust-server")
		}
		*uri = *connect
	}
	if *allow != "" {
		return trust(*allow)
	}
	if *uri == "" {
		flag.Usage()
		return errors.New("use the Open in Wireshark button in the lab admin")
	}
	base, id, ticket, e := launch(*uri)
	if e != nil {
		return e
	}
	if !directLaunch && !trusted(base) {
		return fmt.Errorf("server is not trusted; run the installer with this lab URL: %s", base)
	}
	var executable string
	if !*saveOnly {
		executable, e = findWireshark(*shark)
		if e != nil {
			return e
		}
	}
	if *dest == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		*dest = filepath.Join(home, "QUIC Lab Captures")
	}
	if e = os.MkdirAll(*dest, 0700); e != nil {
		return e
	}
	output := filepath.Join(*dest, time.Now().Format("20060102-150405")+"-"+id[:8]+".pcapng")
	file, e := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer file.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("capture redirects are not allowed") }}
	payload, _ := json.Marshal(map[string]string{"id": id, "ticket": ticket})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"capture/redeem", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	response, e := client.Do(req)
	if e != nil {
		return errors.New("cannot contact trusted lab server")
	}
	var auth struct {
		Token   string `json:"token"`
		Port    int    `json:"port"`
		TCPPort int    `json:"tcp_port"`
	}
	e = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&auth)
	response.Body.Close()
	if response.StatusCode != 200 || e != nil || auth.Port < 1 || auth.Port > 65535 || auth.TCPPort < 1 || auth.TCPPort > 65535 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(auth.Token) {
		return errors.New("launch link expired or was already used; prepare a new link in the admin")
	}
	stream := "wss" + strings.TrimPrefix(base, "https") + "capture/stream?id=" + id
	conn, _, e := websocket.Dial(ctx, stream, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer " + auth.Token}}})
	if e != nil {
		return errors.New("cannot open capture stream; check server/reverse proxy WebSocket settings")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(2 << 20)
	var pipe io.WriteCloser
	var cmd *exec.Cmd
	if !*saveOnly {
		cmd = exec.Command(executable, "-k", "-i", "-", "-d", fmt.Sprintf("udp.port==%d,quic", auth.Port), "-d", fmt.Sprintf("tcp.port==%d,tls", auth.TCPPort))
		pipe, e = cmd.StdinPipe()
		if e != nil {
			return e
		}
		if e = cmd.Start(); e != nil {
			return e
		}
		defer pipe.Close()
		stopPipe := context.AfterFunc(ctx, func() { _ = pipe.Close() })
		defer stopPipe()
		go func() { _ = cmd.Wait(); cancel() }()
	}
	bytes := 0
	first := true
	for {
		kind, b, e := conn.Read(ctx)
		if e != nil {
			if websocket.CloseStatus(e) == websocket.StatusNormalClosure || ctx.Err() != nil {
				break
			}
			return errors.New("capture stream interrupted; partial recording saved")
		}
		if kind != websocket.MessageBinary || len(b) < 12 || len(b)%4 != 0 {
			return errors.New("invalid capture block")
		}
		if first {
			if binary.LittleEndian.Uint32(b) != 0x0a0d0d0a {
				return errors.New("capture section header missing")
			}
			first = false
		}
		// Bound even a compromised trusted server; never interpret data as paths/commands.
		bytes += len(b)
		if bytes > 34<<20 {
			return errors.New("capture exceeded size limit")
		}
		if _, e = file.Write(b); e != nil {
			return e
		}
		if pipe != nil {
			if _, e = pipe.Write(b); e != nil {
				break
			}
		}
	}
	if e = file.Sync(); e != nil {
		return e
	}
	fmt.Println("Saved:", output)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		if runtime.GOOS != "linux" && !directLaunch {
			notify(e.Error())
		}
		os.Exit(1)
	}
}
