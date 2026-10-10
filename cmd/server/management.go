package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

func selectPublicHandler(management bool, admin http.Handler, base string, log *slog.Logger, probe func(context.Context) error) http.Handler {
	if management {
		return managementPublicHandler(admin, base)
	}
	return publicHandler(admin, base, log, nil, probe)
}

func managementPublicHandler(admin http.Handler, base string) http.Handler {
	mux := http.NewServeMux()
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	mux.Handle(u.Path, http.StripPrefix(strings.TrimSuffix(u.Path, "/"), admin))
	mux.Handle("/mdm/v1/", admin)
	if u.Path != "/" {
		mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, u.Path, http.StatusFound) })
	}
	return mux
}
