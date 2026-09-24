package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Demo is intentionally HTTP/1.1, one non-resumable request, with bounded rate and size.
func Demo(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>QUIC Lab download</title><h1>Загрузка через VPN</h1><p>Выберите этот браузер в VPN. Во время загрузки переключайте Wi-Fi и LTE.</p><p><a href="download">Скачать 128 MiB — около 4 минут</a></p><p>В журнале сервера проверяйте один request_id и completed=true. Возобновление Range отключено.</p>`)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			http.Error(w, "Restart the experiment; ranges are disabled", 416)
			return
		}
		id := identity()
		const size = 128 << 20
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="quic-lab-`+id+`.bin"`)
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Request-ID", id)
		log.Info("download_started", "request_id", id)
		sent := 0
		defer func() { log.Info("download_finished", "request_id", id, "bytes", sent, "completed", sent == size) }()
		b := make([]byte, 32<<10)
		for i := range b {
			b[i] = byte(i % 251)
		}
		ticker := time.NewTicker(62500 * time.Microsecond)
		defer ticker.Stop()
		for sent < size {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
			n, e := w.Write(b)
			sent += n
			if e != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	})
	return mux
}
