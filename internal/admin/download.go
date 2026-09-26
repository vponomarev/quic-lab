package admin

import (
	"net/http"
	"os"
	"time"
)

func (w *Web) apkSize() string {
	f, e := os.Stat(w.Config.APKPath)
	if e != nil || !f.Mode().IsRegular() || f.Size() == 0 {
		return ""
	}
	return byteText(uint64(f.Size()))
}

// Only the configured APK is public, never the identity store or a requested path.
func (w *Web) downloadAPK(rw http.ResponseWriter, r *http.Request) {
	f, e := os.Open(w.Config.APKPath)
	if e != nil {
		http.NotFound(rw, r)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		http.NotFound(rw, r)
		return
	}
	// APK downloads may take longer than the admin forms write timeout.
	_ = http.NewResponseController(rw).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	rw.Header().Set("Content-Type", "application/vnd.android.package-archive")
	rw.Header().Set("Content-Disposition", `attachment; filename="quic-lab.apk"`)
	http.ServeContent(rw, r, "quic-lab.apk", info.ModTime(), f)
}
